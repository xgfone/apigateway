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

	"github.com/xgfone/apigw"
	"github.com/xgfone/apigw/forward/lb"
	"github.com/xgfone/apigw/forward/lb/backend"
	"github.com/xgfone/ship/v3"
)

func initAdminRouter(r *ship.Ship, gw *apigw.Gateway) {
	c := adminController{gateway: gw}

	v1admin := r.Group("/v1/admin")
	v1admin.Route("/host").GET(c.GetAllDomains).DELETE(c.DeleteDomain)
	v1admin.Route("/host/route").
		GET(c.GetAllDomainRoutes).
		POST(c.AddDomainRoute).
		DELETE(c.DelDomainRoute)
	v1admin.Route("/host/route/backend").
		GET(c.GetAllDomainRouteBackends).
		POST(c.AddDomainRouteBackend).
		DELETE(c.DelDomainRouteBackend)
}

type adminController struct {
	gateway *apigw.Gateway
	timeout time.Duration
}

func (c adminController) GetAllDomains(ctx *ship.Context) (err error) {
	routers := c.gateway.Router().Routers()
	domains := make([]string, 0, len(routers))
	for domain := range routers {
		if domain != "" {
			domains = append(domains, domain)
		}
	}

	return ctx.JSON(200, map[string][]string{"hosts": domains})
}

func (c adminController) DeleteDomain(ctx *ship.Context) (err error) {
	c.gateway.Router().DelHost(ctx.QueryParam("host"))
	return
}

func (c adminController) GetAllDomainRoutes(ctx *ship.Context) (err error) {
	router := c.gateway.Router().Router(ctx.QueryParam("host"))
	if router == nil {
		return ctx.JSON(200, map[string][]interface{}{"routes": {}})
	}

	routes := router.Routes()
	results := make([]apigw.Route, len(routes))
	for i, _len := 0, len(routes); i < _len; i++ {
		results[i] = routes[i].Handler.(ship.RouteInfo).CtxData.(apigw.Route)
	}
	return ctx.JSON(200, map[string][]apigw.Route{"routes": results})
}

func (c adminController) AddDomainRoute(ctx *ship.Context) (err error) {
	var r struct {
		apigw.Route
		Backends []Backend `json:"backends"`
	}

	if err = ctx.Bind(&r); err != nil {
		return ship.ErrBadRequest.New(err)
	} else if r.Path == "" || r.Method == "" {
		return ship.ErrBadRequest.Newf("missing path or method")
	}

	backends := make([]lb.Backend, len(r.Backends))
	for i, _len := 0, len(r.Backends); i < _len; i++ {
		if backends[i], err = r.Backends[i].Backend(); err != nil {
			return ship.ErrBadRequest.New(err)
		}
	}

	r.Forwarder = lb.NewForwarder(c.timeout)
	if err = c.gateway.RegisterRoute(r.Route); err != nil {
		return ship.ErrBadRequest.New(err)
	}

	for _, b := range backends {
		r.Forwarder.(*lb.Forwarder).EndpointManager().AddEndpoint(b)
	}

	return
}

func (c adminController) DelDomainRoute(ctx *ship.Context) (err error) {
	var r apigw.Route
	if err = ctx.Bind(&r); err != nil {
		return ship.ErrBadRequest.New(err)
	} else if r.Path == "" || r.Method == "" {
		return ship.ErrBadRequest.Newf("missing path or method")
	}

	if err = c.gateway.UnregisterRoute(r); err != nil {
		return ship.ErrBadRequest.New(err)
	}

	return
}

func (c adminController) GetAllDomainRouteBackends(ctx *ship.Context) (err error) {
	var req struct {
		Host   string `query:"host"`
		Path   string `query:"path" validate:"required"`
		Method string `query:"method" validate:"required"`
	}
	if err = ctx.BindQuery(&req); err != nil {
		return ship.ErrBadRequest.New(err)
	}

	router := c.gateway.Router().Router(req.Host)
	if router == nil {
		return ctx.JSON(200, map[string][]interface{}{"backends": {}})
	}

	var backends []map[string]interface{}
	for _, route := range router.Routes() {
		if route.Path == req.Path && route.Method == req.Method {
			r := route.Handler.(ship.RouteInfo).CtxData.(apigw.Route)
			f := r.Forwarder.(*lb.Forwarder)
			endpoints := f.EndpointManager().Endpoints()
			backends = make([]map[string]interface{}, len(endpoints))
			for j, _len := 0, len(endpoints); j < _len; j++ {
				backends[j] = endpoints[j].(lb.Backend).Metadata()
			}

			break
		}
	}

	return ctx.JSON(200, map[string]interface{}{"backends": backends})
}

// Backend is the backend of the route.
type Backend struct {
	Method string `json:"method"`
	URL    string `json:"url"`
}

// Backend converts itself to the LB backend.
func (b Backend) Backend() (lb.Backend, error) {
	if b.Method == "noop" {
		return backend.NewNoopBackend(b.URL), nil
	}
	return backend.NewHTTPBackend(b.Method, b.URL, nil)
}

func (c adminController) AddDomainRouteBackend(ctx *ship.Context) (err error) {
	var req struct {
		Host     string    `json:"host"`
		Path     string    `json:"path" validate:"required"`
		Method   string    `json:"method" validate:"required"`
		Backends []Backend `json:"backends"`
	}
	if err = ctx.Bind(&req); err != nil {
		return ship.ErrBadRequest.New(err)
	} else if len(req.Backends) == 0 {
		return
	}

	backends := make([]lb.Backend, len(req.Backends))
	for i, _len := 0, len(req.Backends); i < _len; i++ {
		if backends[i], err = req.Backends[i].Backend(); err != nil {
			return ship.ErrBadRequest.New(err)
		}
	}

	router := c.gateway.Router().Router(req.Host)
	if router == nil {
		return ship.ErrBadRequest.Newf("no host '%s'", req.Host)
	}

	for _, route := range router.Routes() {
		if route.Path == req.Path && route.Method == req.Method {
			r := route.Handler.(ship.RouteInfo).CtxData.(apigw.Route)
			m := r.Forwarder.(*lb.Forwarder).EndpointManager()
			for _, backend := range backends {
				m.AddEndpoint(backend)
			}
			return
		}
	}

	return ship.ErrBadRequest.Newf("no host route: host=%s, path=%s, method=%s",
		req.Host, req.Path, req.Method)
}

func (c adminController) DelDomainRouteBackend(ctx *ship.Context) (err error) {
	var req struct {
		Host     string    `json:"host"`
		Path     string    `json:"path" validate:"required"`
		Method   string    `json:"method" validate:"required"`
		Backends []Backend `json:"backends"`
	}
	if err = ctx.Bind(&req); err != nil {
		return ship.ErrBadRequest.New(err)
	} else if len(req.Backends) == 0 {
		return
	}

	backends := make([]lb.Backend, len(req.Backends))
	for i, _len := 0, len(req.Backends); i < _len; i++ {
		if backends[i], err = req.Backends[i].Backend(); err != nil {
			return ship.ErrBadRequest.New(err)
		}
	}

	router := c.gateway.Router().Router(req.Host)
	if router == nil {
		return ship.ErrBadRequest.Newf("no host '%s'", req.Host)
	}

	for _, route := range router.Routes() {
		if route.Path == req.Path && route.Method == req.Method {
			r := route.Handler.(ship.RouteInfo).CtxData.(apigw.Route)
			m := r.Forwarder.(*lb.Forwarder).EndpointManager()
			for _, backend := range backends {
				m.DelEndpoint(backend)
			}
			return
		}
	}

	return ship.ErrBadRequest.Newf("no host route: host=%s, path=%s, method=%s",
		req.Host, req.Path, req.Method)
}
