// Copyright 2021 xgfone
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"time"

	"github.com/xgfone/apigateway/backend"
	"github.com/xgfone/apigw"
	"github.com/xgfone/apigw/forward/lb"
	"github.com/xgfone/go-service/loadbalancer"
	"github.com/xgfone/go-tools/v7/lifecycle"
	"github.com/xgfone/ship/v3"
)

var hc *loadbalancer.HealthCheck

func init() {
	hc = loadbalancer.NewHealthCheck()
	hc.Interval = time.Second * 10
	lifecycle.Register(hc.Stop)
}

func initAdminRouter(r *ship.Ship, gw *apigw.Gateway) {
	c := adminController{gateway: gw}

	v1admin := r.Group("/v1/admin")
	v1admin.Route("/host").
		GET(c.GetAllDomains).
		DELETE(c.DeleteDomain)
	v1admin.Route("/host/route").
		GET(c.GetAllDomainRoutes).
		POST(c.AddDomainRoute).
		DELETE(c.DelDomainRoute)
	v1admin.Route("/host/route/backend").
		GET(c.GetAllDomainRouteBackends).
		POST(c.AddDomainRouteBackend).
		DELETE(c.DelDomainRouteBackend)
	v1admin.Route("/host/backendgroup").
		GET(c.GetBackendGroup).
		POST(c.CreateBackendGroup).
		DELETE(c.DeleteBackendGroup)

	v1adminUnderlying := v1admin.Group("/underlying")
	v1adminUnderlying.Route("/hosts").GET(c.GetAllUnderlyingHosts)
	v1adminUnderlying.Route("/routes").GET(c.GetAllUnderlyingRoutes)
	v1adminUnderlying.Route("/endpoints").GET(c.GetAllUnderlyingEndpoints)
}

type adminController struct {
	gateway *apigw.Gateway
	timeout time.Duration
}

func (c adminController) GetAllUnderlyingHosts(ctx *ship.Context) (err error) {
	routers := c.gateway.Router().Routers()
	hosts := make([]string, 0, len(routers))
	for host := range routers {
		hosts = append(hosts, host)
	}
	return ctx.JSON(200, map[string][]string{"hosts": c.gateway.GetHosts()})
}

func (c adminController) GetAllUnderlyingRoutes(ctx *ship.Context) (err error) {
	var req struct {
		Host string `query:"host" validate:"zero|hostname_rfc1123"`
	}
	if err = ctx.BindQuery(&req); err != nil {
		return ship.ErrBadRequest.New(err)
	}

	var routes []apigw.Route
	if router := c.gateway.Router().Router(req.Host); router != nil {
		rs := router.Routes()
		routes = make([]apigw.Route, len(rs))
		for i, _len := 0, len(rs); i < _len; i++ {
			routes[i] = rs[i].Handler.(ship.RouteInfo).CtxData.(apigw.Route)
		}
	}

	return ctx.JSON(200, map[string][]apigw.Route{"routes": routes})
}

func (c adminController) GetAllUnderlyingEndpoints(ctx *ship.Context) (err error) {
	endpoints := hc.Endpoints()
	eps := make([]map[string]interface{}, len(endpoints))
	for i, _len := 0, len(endpoints); i < _len; i++ {
		ep := endpoints[i]

		eps[i] = map[string]interface{}{
			"type":            ep.Type(),
			"metadata":        ep.MetaData(),
			"userdata":        ep.UserData(),
			"reference_count": hc.ReferenceCount(ep.String()),
		}
	}
	return ctx.JSON(200, map[string]interface{}{"endpoints": eps})
}

func (c adminController) GetBackendGroup(ctx *ship.Context) (err error) {
	var req struct {
		Host         string `query:"host" validate:"zero|hostname_rfc1123"`
		BackendGroup string `query:"backend_group"`
	}
	if err = ctx.BindQuery(&req); err != nil {
		return ship.ErrBadRequest.New(err)
	}

	m := lb.GetBackendGroupManager(req.Host)
	if m == nil {
		return ship.ErrBadRequest.Newf("no the host '%s'", req.Host)
	}

	if req.BackendGroup == "" {
		bgs := m.BackendGroups()
		gs := make([]string, len(bgs))
		for i, _len := 0, len(bgs); i < _len; i++ {
			gs[i] = bgs[i].Name()
		}
		return ctx.JSON(200, map[string]interface{}{"backend_groups": gs})
	}

	bg := m.BackendGroup(req.BackendGroup)
	if bg == nil {
		return ship.ErrBadRequest.Newf("no backend group named '%s'", req.BackendGroup)
	}

	backends := bg.Backends()
	bs := make(backend.Backends, len(backends))
	for i, _len := 0, len(backends); i < _len; i++ {
		b := backends[i]

		var interval, timeout string
		hc := b.HealthCheck()
		if hc.Interval > 0 {
			interval = hc.Interval.String()
		}
		if hc.Timeout > 0 {
			timeout = hc.Timeout.String()
		}

		bs[i] = backend.Backend{
			Type:     b.Type(),
			Metadata: b.MetaData(),
			RetryNum: hc.RetryNum,
			Interval: interval,
			Timeout:  timeout,
		}
	}

	return ctx.JSON(200, map[string]interface{}{"backends": bs})
}

func (c adminController) CreateBackendGroup(ctx *ship.Context) (err error) {
	var req struct {
		Host          string `json:"host" validate:"zero|hostname_rfc1123"`
		BackendGroups []struct {
			Name     string           `json:"name" validate:"required"`
			Backends backend.Backends `json:"backends"`
		} `json:"backend_groups"`
	}
	if err = ctx.Bind(&req); err != nil {
		return ship.ErrBadRequest.New(err)
	}

	m := lb.RegisterBackendGroupManager(lb.NewBackendGroupManager(req.Host))
	for _, bg := range req.BackendGroups {
		backends, err := bg.Backends.Backends(apigw.Route{Host: req.Host})
		if err != nil {
			return ship.ErrBadRequest.New(err)
		}
		m.Add(lb.NewBackendGroup(bg.Name)).AddBackends(backends)
	}

	return
}

func (c adminController) DeleteBackendGroup(ctx *ship.Context) (err error) {
	var req struct {
		Host          string `json:"host" validate:"zero|hostname_rfc1123"`
		BackendGroups []struct {
			Name     string           `json:"name" validate:"required"`
			Backends backend.Backends `json:"backends"`
		} `json:"backend_groups"`
	}
	if err = ctx.Bind(&req); err != nil {
		return ship.ErrBadRequest.New(err)
	}

	if len(req.BackendGroups) == 0 {
		lb.UnregisterBackendGroupManager(req.Host)
		return
	}

	m := lb.GetBackendGroupManager(req.Host)
	if m == nil {
		return
	}

	for _, bg := range req.BackendGroups {
		if len(bg.Backends) == 0 {
			m.Delete(bg.Name)
		} else if backends, err := bg.Backends.Backends(apigw.Route{Host: req.Host}); err != nil {
			return ship.ErrBadRequest.New(err)
		} else if g := m.BackendGroup(bg.Name); g != nil {
			g.DelBackends(backends)
		}
	}

	return
}

func (c adminController) GetAllDomains(ctx *ship.Context) (err error) {
	return ctx.JSON(200, map[string][]string{"hosts": c.gateway.GetHosts()})
}

func (c adminController) DeleteDomain(ctx *ship.Context) (err error) {
	var req struct {
		Host string `query:"host" validate:"zero|hostname_rfc1123"`
	}
	if err = ctx.BindQuery(&req); err != nil {
		return ship.ErrBadRequest.New(err)
	}
	return c.gateway.DelHost(req.Host)
}

func (c adminController) GetAllDomainRoutes(ctx *ship.Context) (err error) {
	var req struct {
		Host string `query:"host" validate:"zero|hostname_rfc1123"`
	}
	if err = ctx.BindQuery(&req); err != nil {
		return ship.ErrBadRequest.New(err)
	}

	routes := c.gateway.GetRoutes(req.Host)
	return ctx.JSON(200, map[string][]apigw.Route{"routes": routes})
}

func (c adminController) AddDomainRoute(ctx *ship.Context) (err error) {
	var r struct {
		apigw.Route
		Backends backend.Backends `json:"backends"`
	}

	if err = ctx.Bind(&r); err != nil {
		return ship.ErrBadRequest.New(err)
	} else if r.Path == "" || r.Method == "" {
		return ship.ErrBadRequest.Newf("missing path or method")
	}

	backends, err := r.Backends.Backends(r.Route)
	if err != nil {
		return ship.ErrBadRequest.New(err)
	}

	forwarder := lb.NewForwarder(r.Name(), c.timeout)
	forwarder.HealthCheck = hc
	r.Forwarder = forwarder
	if r.Route, err = c.gateway.RegisterRoute(r.Route); err != nil {
		return ship.ErrBadRequest.New(err)
	}

	forwarder = r.Route.Forwarder.(*lb.Forwarder)
	if forwarder.Session == nil {
		forwarder.Session = loadbalancer.NewMemorySessionManager()
	}

	forwarder.AddBackends(backends)
	return
}

func (c adminController) DelDomainRoute(ctx *ship.Context) (err error) {
	var r apigw.Route
	if err = ctx.Bind(&r); err != nil {
		return ship.ErrBadRequest.New(err)
	} else if r.Path == "" || r.Method == "" {
		return ship.ErrBadRequest.Newf("missing path or method")
	}

	if _, err = c.gateway.UnregisterRoute(r); err != nil {
		return ship.ErrBadRequest.New(err)
	}

	return
}

func (c adminController) GetAllDomainRouteBackends(ctx *ship.Context) (err error) {
	var req struct {
		Host   string `query:"host" validate:"zero|hostname_rfc1123"`
		Path   string `query:"path" validate:"required"`
		Method string `query:"method" validate:"required"`
	}
	if err = ctx.BindQuery(&req); err != nil {
		return ship.ErrBadRequest.New(err)
	}

	r, ok := c.gateway.GetRoute(req.Host, req.Path, req.Method)
	if !ok {
		return ctx.JSON(200, map[string][]interface{}{"backends": {}})
	}

	forwarder := r.Forwarder.(*lb.Forwarder)
	backends := forwarder.Backends()
	results := make([]backend.Backend, len(backends))
	for i, _len := 0, len(backends); i < _len; i++ {
		b := backends[i].(lb.Backend)

		var interval, timeout string
		hc := b.HealthCheck()
		if hc.Interval > 0 {
			interval = hc.Interval.String()
		}
		if hc.Timeout > 0 {
			timeout = hc.Timeout.String()
		}

		results[i] = backend.Backend{
			Type:     b.Type(),
			Metadata: b.MetaData(),
			RetryNum: hc.RetryNum,
			Interval: interval,
			Timeout:  timeout,
		}
	}

	if m := lb.GetBackendGroupManager(req.Host); m != nil {
		bgs := m.BackendGroupsByUpdaterName(forwarder.Name())
		if _len := len(bgs); _len != 0 {
			for i := 0; i < _len; i++ {
				results = append(results, backend.Backend{
					Type:     "group",
					Metadata: map[string]interface{}{"name": bgs[i].Name()},
				})
			}
		}
	}

	return ctx.JSON(200, map[string]interface{}{"backends": results})
}

func (c adminController) AddDomainRouteBackend(ctx *ship.Context) (err error) {
	var req struct {
		Host     string           `json:"host" validate:"zero|hostname_rfc1123"`
		Path     string           `json:"path" validate:"required"`
		Method   string           `json:"method" validate:"required"`
		Backends backend.Backends `json:"backends"`
	}
	if err = ctx.Bind(&req); err != nil {
		return ship.ErrBadRequest.New(err)
	} else if len(req.Backends) == 0 {
		return
	}

	route := apigw.NewRoute(req.Host, req.Path, req.Method)
	backends, err := req.Backends.Backends(route)
	if err != nil {
		return ship.ErrBadRequest.New(err)
	}

	if !c.gateway.HasHost(req.Host) {
		return ship.ErrBadRequest.Newf("no host '%s'", req.Host)
	}

	r, ok := c.gateway.GetRoute(req.Host, req.Path, req.Method)
	if !ok {
		return ship.ErrBadRequest.Newf("no host route: host=%s, path=%s, method=%s",
			req.Host, req.Path, req.Method)
	}

	r.Forwarder.(*lb.Forwarder).AddBackends(backends)
	return
}

func (c adminController) DelDomainRouteBackend(ctx *ship.Context) (err error) {
	var req struct {
		Host     string           `json:"host" validate:"zero|hostname_rfc1123"`
		Path     string           `json:"path" validate:"required"`
		Method   string           `json:"method" validate:"required"`
		Backends backend.Backends `json:"backends"`
	}
	if err = ctx.Bind(&req); err != nil {
		return ship.ErrBadRequest.New(err)
	} else if len(req.Backends) == 0 {
		return
	}

	route := apigw.NewRoute(req.Host, req.Path, req.Method)
	backends, err := req.Backends.Backends(route)
	if err != nil {
		return ship.ErrBadRequest.New(err)
	}

	if !c.gateway.HasHost(req.Host) {
		return ship.ErrBadRequest.Newf("no host '%s'", req.Host)
	}

	r, ok := c.gateway.GetRoute(req.Host, req.Path, req.Method)
	if !ok {
		return ship.ErrBadRequest.Newf("no host route: host=%s, path=%s, method=%s",
			req.Host, req.Path, req.Method)
	}

	r.Forwarder.(*lb.Forwarder).DelBackends(backends)
	return
}
