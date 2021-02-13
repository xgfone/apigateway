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
	"github.com/xgfone/apigw"
	"github.com/xgfone/apigw/loader"
	"github.com/xgfone/gconf/v5"
	"github.com/xgfone/go-tools/v7/lifecycle"
	"github.com/xgfone/goapp/log"
)

func init() {
	gconf.RegisterOpts(
		gconf.StrSliceOpt("plugins", "The list of the names of the plugins to be enabled."),
		gconf.StrSliceOpt("middlewares", "The list of the names of the middlewares to be enabled."),
		gconf.StrSliceOpt("sds", "The list of the names of the service discoveries to be enabled."),
	)
}

func registerPlugins(gw *apigw.Gateway) {
	for _, name := range gconf.MustStringSlice("plugins") {
		loader := loader.GetPluginLoader(name)
		if loader == nil {
			log.Fatalf("no the plugin named '%s'", name)
		}

		plugin, err := loader.Plugin()
		if err != nil {
			log.Fatal("fail to load the plugin", log.F("plugin", name), log.E(err))
		}

		gw.RegisterPlugin(plugin)
	}
}

func registerMiddlewares(gw *apigw.Gateway) {
	for _, name := range gconf.MustStringSlice("middlewares") {
		loader := loader.GetMiddlewareLoader(name)
		if loader == nil {
			log.Fatalf("no the middleware named '%s'", name)
		}

		mw, err := loader.Middleware()
		if err != nil {
			log.Fatal("fail to load the middleware", log.F("middleware", name), log.E(err))
		}

		gw.RegisterMiddlewares(mw)
	}
}

func startServiceDiscoveries(gw *apigw.Gateway) {
	for _, name := range gconf.MustStringSlice("sds") {
		loader := loader.GetServiceDiscoveryLoader(name)
		if loader == nil {
			log.Fatalf("no the service discovery named '%s'", name)
		}

		sd, err := loader.ServiceDiscovery()
		if err != nil {
			log.Fatal("fail to load the service discovery", log.F("sd", name), log.E(err))
		}

		go sd(lifecycle.Context(), gw)
	}
}
