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
	"net/http"

	"github.com/xgfone/apigw"
	"github.com/xgfone/gconf/v5"
	"github.com/xgfone/go-tools/v7/lifecycle"
	"github.com/xgfone/goapp"
	"github.com/xgfone/goapp/log"
	"github.com/xgfone/goapp/router"
	"github.com/xgfone/ship/v3"
	"github.com/xgfone/ship/v3/middleware"
)

const appName = "apigateway"

var globalOpts = []gconf.Opt{
	gconf.StrOpt("manageraddr", "The address [HOST]:PORT that the api manager listens on."),
	gconf.StrOpt("gatewayaddr", "The address [HOST]:PORT that the api gateway listens on.").D(":80"),
}

var httpOpts = []gconf.Opt{
	gconf.IntOpt("maxidleconnsperhost", "The maximum number of the idle connections per host.").D(100),
	gconf.DurationOpt("idleconntimeout", "The timeout of the idle connection.").D("30s"),
}

func init() {
	gconf.NewGroup("http").RegisterOpts(httpOpts...)
}

func main() {
	// Parse the CLI arguments and initialize the logging.
	goapp.Init(appName, globalOpts)

	// Initialize the http transport.
	tp := http.DefaultTransport.(*http.Transport)
	tp.MaxIdleConnsPerHost = gconf.Group("http").GetInt("maxidleconnsperhost")
	tp.IdleConnTimeout = gconf.Group("http").GetDuration("idleconntimeout")

	// Initialize the api gateway instance.
	gw := apigw.NewGateway()
	gw.Router().Name = "gateway"
	gw.Router().SetLogger(log.GetDefaultLogger())
	gw.Router().RegisterOnShutdown(lifecycle.Stop)
	lifecycle.Register(gw.Router().Stop)

	// Register the route plugins and middlewres, and start the service discoveries.
	registerPlugins(gw)
	registerMiddlewares(gw)
	startServiceDiscoveries(gw)

	// Start the api manager server.
	if maddr := gconf.MustString("manageraddr"); maddr != "" {
		mapp := ship.Default()
		mapp.Name = "manager"
		mapp.Signals = nil
		mapp.Link(gw.Router().Runner)
		mapp.Use(middleware.Logger(), router.Recover)
		mapp.SetLogger(log.GetDefaultLogger())
		router.AddRuntimeRoutes(mapp)
		initAdminRouter(mapp, gw)
		go mapp.Start(maddr)
	}

	// Start the api gateway HTTP server.
	gw.Router().Start(gconf.MustString("gatewayaddr")).Wait()
}
