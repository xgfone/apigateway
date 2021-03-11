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

package backend

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/xgfone/apigw"
	"github.com/xgfone/apigw/forward/lb"
	"github.com/xgfone/apigw/forward/lb/backend"
	"github.com/xgfone/go-service/loadbalancer"
	"github.com/xgfone/go-tools/v7/lifecycle"
	"github.com/xgfone/go-tools/v7/strings2"
	"github.com/xgfone/ship/v3"
)

// HC is the global health checker.
var HC *loadbalancer.HealthCheck

// DefaultForwarderMaxTimeout is the default maximum timeout of the forwarder.
var DefaultForwarderMaxTimeout time.Duration

func init() {
	HC = loadbalancer.NewHealthCheck()
	HC.Interval = time.Second * 10
	lifecycle.Register(HC.Stop)
}

// NewForwarder returns a new backend forwarder.
//
// If maxTimeout is ZERO, it is equal to DefaultForwarderMaxTimeout by default.
func NewForwarder(name string, maxTimeout time.Duration) lb.Forwarder {
	if maxTimeout == 0 {
		maxTimeout = DefaultForwarderMaxTimeout
	}

	return lb.NewForwarder(name, &lb.ForwarderConfig{
		MaxTimeout:  maxTimeout,
		HealthCheck: HC,
		UpdateLoadBalancer: func(lb *loadbalancer.LoadBalancer) {
			lb.Session = loadbalancer.NewMemorySessionManager()
		},
	})
}

// IsHealthy reports whether the backend is healthy.
func IsHealthy(backend lb.Backend) bool {
	return HC.IsHealthy(backend.String())
}

// Backend is the backend of the route.
type Backend struct {
	Type     string                 `json:"type" validate:"required"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
	Timeout  string                 `json:"timeout,omitempty"`
	Interval string                 `json:"interval,omitempty"`
	RetryNum int                    `json:"retrynum,omitempty"`
}

// Backend converts the information to the lb backend.
func (b Backend) Backend(r apigw.Route) (_ lb.Backend, err error) {
	hc := lb.HealthCheck{RetryNum: b.RetryNum}
	if b.Interval != "" {
		if hc.Interval, err = time.ParseDuration(b.Interval); err != nil {
			return nil, err
		}
	}
	if b.Timeout != "" {
		if hc.Timeout, err = time.ParseDuration(b.Timeout); err != nil {
			return nil, err
		}
	}

	if builder := backend.GetBuilder(b.Type); builder != nil {
		backend, err := builder.New(backend.BuilderContext{
			Route:       r,
			MetaData:    b.Metadata,
			HealthCheck: hc,
		})
		if err != nil {
			return nil, err
		}

		return lb.NewBackendWithHealthCheck(backend, hc), nil
	}

	return nil, fmt.Errorf("no the backend typed '%s'", b.Type)
}

// Backends is a set of Backends.
type Backends []Backend

// Backends converts themself to []lb.Backend.
func (bs Backends) Backends(r apigw.Route) ([]lb.Backend, error) {
	var err error
	backends := make([]lb.Backend, len(bs))
	for i, _len := 0, len(bs); i < _len; i++ {
		if backends[i], err = bs[i].Backend(r); err != nil {
			return backends, err
		}
	}
	return backends, err
}

func init() {
	backend.RegisterBuilder(backend.NewBuilder("group", func(c backend.BuilderContext) (lb.Backend, error) {
		name, ok := c.MetaData["name"].(string)
		if !ok {
			return nil, errors.New("missing the group name")
		} else if name == "" {
			return nil, errors.New("the group name must not be empty")
		}

		if m := lb.DefaultGateway.GetBackendGroupManager(c.Host); m == nil {
			return nil, fmt.Errorf("no host '%s'", c.Host)
		} else if group := m.GetBackendGroup(name); group != nil {
			return group.(lb.Backend), nil
		}

		return nil, fmt.Errorf("no the backend group '%s' below the host '%s'", name, c.Host)
	}))

	backend.RegisterBuilder(backend.NewBuilder("http", func(c backend.BuilderContext) (lb.Backend, error) {
		var req struct {
			QPS      int    `mapstructure:"qps"`
			URL      string `mapstructure:"url"`
			Method   string `mapstructure:"method"`
			CheckURL string `mapstructure:"checkurl"`
		}
		if err := mapstructure.Decode(c.MetaData, &req); err != nil {
			return nil, err
		} else if req.URL == "" {
			return nil, fmt.Errorf("missing the url")
		}

		var hc loadbalancer.HealthChecker
		if req.CheckURL != "" {
			hc = func(c context.Context, url string) error {
				resp, err := http.Get(req.CheckURL)
				if err != nil {
					return err
				}
				defer resp.Body.Close()

				if resp.StatusCode == 200 {
					return nil
				}

				buf := strings2.NewBuilder(int(resp.ContentLength))
				ship.CopyNBuffer(buf, resp.Body, resp.ContentLength, nil)
				return errors.New(buf.String())
			}
		}

		conf := &backend.HTTPBackendConfig{
			UserData:      c.UserData,
			HealthCheck:   c.HealthCheck,
			HealthChecker: hc,
		}
		next, err := backend.NewHTTPBackend(req.Method, req.URL, conf)
		if err != nil {
			return nil, err
		}

		return backend.NewQPSBackend(req.QPS, next), nil
	}))
}
