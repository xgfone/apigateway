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
	"github.com/xgfone/apigateway/backend"
	"github.com/xgfone/apigw"
	"github.com/xgfone/apigw/forward/lb"
	"github.com/xgfone/gconf/v5"
	"github.com/xgfone/ship/v3"
)

var routeOpts = []gconf.Opt{
	gconf.DurationOpt("maxtimeout", "The maximum timeout to forward the request to the backend").D("1m"),
}

func init() { gconf.RegisterOpts(routeOpts...) }

func initAdminRouter(r *ship.Ship) {
	backend.DefaultForwarderMaxTimeout = gconf.MustDuration("maxtimeout")
	c := adminController{}

	v1admin := r.Group("/v1/admin")
	v1admin.Route("/host").
		GET(c.GetAllDomains).
		POST(c.CreateDomain).
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

type adminController struct{}

func (c adminController) sendError(host, path, method string, err error) error {
	switch err {
	case nil:
		return nil
	case apigw.ErrNoHost:
		return ship.ErrBadRequest.Newf("no host '%s'", host)
	case apigw.ErrNoRoute:
		return ship.ErrBadRequest.Newf("no route: host=%s, path=%s, method=%s", host, path, method)
	case apigw.ErrEmptyPath:
		return ship.ErrBadRequest.Newf("the path must not be empty")
	default:
		return ship.ErrInternalServerError.New(err)
	}
}

func (c adminController) GetAllUnderlyingHosts(ctx *ship.Context) (err error) {
	routers := lb.DefaultGateway.Router().Routers()
	hosts := make([]string, 0, len(routers))
	for host := range routers {
		hosts = append(hosts, host)
	}
	return ctx.JSON(200, map[string][]string{"hosts": hosts})
}

func (c adminController) GetAllUnderlyingRoutes(ctx *ship.Context) (err error) {
	var req struct {
		Host string `query:"host" validate:"zero|hostname_rfc1123"`
	}
	if err = ctx.BindQuery(&req); err != nil {
		return ship.ErrBadRequest.New(err)
	}

	var routes []apigw.Route
	if router := lb.DefaultGateway.Router().Router(req.Host); router != nil {
		rs := router.Routes()
		routes = make([]apigw.Route, len(rs))
		for i, _len := 0, len(rs); i < _len; i++ {
			routes[i] = rs[i].Handler.(ship.RouteInfo).CtxData.(apigw.Route)
		}
	}

	return ctx.JSON(200, map[string][]apigw.Route{"routes": routes})
}

func (c adminController) GetAllUnderlyingEndpoints(ctx *ship.Context) (err error) {
	endpoints := backend.HC.Endpoints()
	eps := make([]map[string]interface{}, len(endpoints))
	for i, _len := 0, len(endpoints); i < _len; i++ {
		ep := endpoints[i]
		metadata := ep.MetaData()
		metadata["online"] = backend.HC.IsHealthy(ep.String())
		eps[i] = map[string]interface{}{
			"type":            ep.Type(),
			"userdata":        ep.UserData(),
			"metadata":        metadata,
			"reference_count": backend.HC.ReferenceCount(ep.String()),
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

	m := lb.DefaultGateway.GetBackendGroupManager(req.Host)
	if m == nil {
		return ship.ErrBadRequest.Newf("no host '%s'", req.Host)
	}

	if req.BackendGroup == "" {
		bgs := m.GetBackendGroups()
		gs := make([]string, len(bgs))
		for i, _len := 0, len(bgs); i < _len; i++ {
			gs[i] = bgs[i].Name()
		}
		return ctx.JSON(200, map[string]interface{}{"backend_groups": gs})
	}

	bg := m.GetBackendGroup(req.BackendGroup)
	if bg == nil {
		return ship.ErrBadRequest.Newf("no backend group named '%s'", req.BackendGroup)
	}

	backends := bg.GetBackends()
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

	updaters := bg.GetUpdaters()
	forwarders := make([]string, len(updaters))
	for i, _len := 0, len(updaters); i < _len; i++ {
		forwarders[i] = updaters[i].Name()
	}

	return ctx.JSON(200, map[string]interface{}{"backends": bs, "forwarders": forwarders})
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

	m := lb.DefaultGateway.GetBackendGroupManager(req.Host)
	if m == nil {
		return ship.ErrBadRequest.Newf("no host '%s'", req.Host)
	}

	conf := &lb.GroupBackendConfig{IsHealthy: backend.IsHealthy}
	for _, bg := range req.BackendGroups {
		backends, err := bg.Backends.Backends(apigw.Route{Host: req.Host})
		if err != nil {
			return ship.ErrBadRequest.New(err)
		}

		group := m.AddOrNewBackendGroup(bg.Name, conf)
		for _, backend := range backends {
			group.AddBackend(backend)
		}
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

	m := lb.DefaultGateway.GetBackendGroupManager(req.Host)
	if m == nil {
		return ship.ErrBadRequest.Newf("no host '%s'", req.Host)
	}

	for _, bg := range req.BackendGroups {
		if len(bg.Backends) == 0 {
			m.DelBackendGroupByName(bg.Name)
		} else if backends, err := bg.Backends.Backends(apigw.Route{Host: req.Host}); err != nil {
			return ship.ErrBadRequest.New(err)
		} else if g := m.GetBackendGroup(bg.Name); g != nil {
			for _, backend := range backends {
				g.DelBackend(backend)
			}
		}
	}

	return
}

func (c adminController) GetAllDomains(ctx *ship.Context) (err error) {
	return ctx.JSON(200, map[string][]string{"hosts": lb.DefaultGateway.GetHosts()})
}

func (c adminController) CreateDomain(ctx *ship.Context) (err error) {
	var req struct {
		Host string `json:"host" validate:"zero|hostname_rfc1123"`
	}
	if err = ctx.Bind(&req); err != nil {
		return ship.ErrBadRequest.New(err)
	}
	return lb.DefaultGateway.AddHost(req.Host)
}

func (c adminController) DeleteDomain(ctx *ship.Context) (err error) {
	var req struct {
		Host string `query:"host" validate:"zero|hostname_rfc1123"`
	}
	if err = ctx.BindQuery(&req); err != nil {
		return ship.ErrBadRequest.New(err)
	}
	return lb.DefaultGateway.DelHost(req.Host)
}

func (c adminController) GetAllDomainRoutes(ctx *ship.Context) (err error) {
	var req struct {
		Host string `query:"host" validate:"zero|hostname_rfc1123"`
	}
	if err = ctx.BindQuery(&req); err != nil {
		return ship.ErrBadRequest.New(err)
	}

	routes, err := lb.DefaultGateway.GetRoutes(req.Host)
	if err != nil {
		return c.sendError(req.Host, "", "", err)
	}
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

	r.Forwarder = backend.NewForwarder(r.Name(), 0)
	if r.Route, err = lb.DefaultGateway.RegisterRoute(r.Route); err != nil {
		return ship.ErrBadRequest.New(err)
	}

	r.Route.Forwarder.(lb.Forwarder).AddBackends(backends...)
	return
}

func (c adminController) DelDomainRoute(ctx *ship.Context) (err error) {
	var r apigw.Route
	if err = ctx.Bind(&r); err != nil {
		return ship.ErrBadRequest.New(err)
	} else if r.Path == "" || r.Method == "" {
		return ship.ErrBadRequest.Newf("missing path or method")
	}

	if _, err = lb.DefaultGateway.UnregisterRoute(r); err != nil {
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

	bs, err := lb.DefaultGateway.GetRouteBackends(req.Host, req.Path, req.Method)
	if err != nil {
		return c.sendError(req.Host, req.Path, req.Method, err)
	}

	backends := make([]backend.Backend, len(bs))
	for i, _len := 0, len(bs); i < _len; i++ {
		b := bs[i].(lb.Backend)

		var interval, timeout string
		hc := b.HealthCheck()
		if hc.Interval > 0 {
			interval = hc.Interval.String()
		}
		if hc.Timeout > 0 {
			timeout = hc.Timeout.String()
		}

		backends[i] = backend.Backend{
			Type:     b.Type(),
			Metadata: b.MetaData(),
			RetryNum: hc.RetryNum,
			Interval: interval,
			Timeout:  timeout,
		}
	}

	return ctx.JSON(200, map[string]interface{}{"backends": backends})
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

	err = lb.DefaultGateway.AddRouteBackends(req.Host, req.Path, req.Method, backends...)
	return c.sendError(req.Host, req.Path, req.Method, err)
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

	err = lb.DefaultGateway.DelRouteBackends(req.Host, req.Path, req.Method, backends...)
	return c.sendError(req.Host, req.Path, req.Method, err)
}
