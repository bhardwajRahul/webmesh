/*
Copyright 2023 Avi Zimmerman <avi.zimmerman@gmail.com>

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package debug implements a plugin that exposes an HTTP server for debugging
// purposes.
package debug

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"strings"
	"sync"

	"github.com/mitchellh/mapstructure"
	v1 "github.com/webmeshproj/api/v1"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/webmeshproj/webmesh/pkg/context"
	"github.com/webmeshproj/webmesh/pkg/plugins/plugindb"
	"github.com/webmeshproj/webmesh/pkg/storage"
	"github.com/webmeshproj/webmesh/pkg/version"
)

// Plugin is the debug plugin.
type Plugin struct {
	v1.UnimplementedPluginServer
	v1.UnimplementedIPAMPluginServer

	data    storage.MeshStorage
	datamux sync.Mutex
	closec  chan struct{}
	servec  chan struct{}
}

// Options are the options for the debug plugin.
type Options struct {
	// ListenAddress is the address to listen on. Defaults to "localhost:6060".
	ListenAddress string `mapstructure:"listen-address"`
	// PathPrefix is the path prefix to use for the debug server.
	// Defaults to "/debug".
	PathPrefix string `mapstructure:"path-prefix"`
	// DisablePProf disables pprof.
	DisablePProf bool `mapstructure:"disable-pprof"`
	// PProfProfiles is the list of profiles to enable for pprof.
	// An empty list enables all profiles. Each will be available at
	// /<path-prefix>/pprof/<profile>.
	PprofProfiles []string `mapstructure:"pprof-profiles"`
	// EnableDBQuerier enables the database querier.
	EnableDBQuerier bool `mapstructure:"enable-db-querier"`
}

// NewDefaultOptions returns the default options for the debug plugin.
func NewDefaultOptions() Options {
	return Options{
		ListenAddress: "localhost:6060",
		PathPrefix:    "/debug",
		PprofProfiles: []string{},
	}
}

// GetInfo returns the plugin info.
func (p *Plugin) GetInfo(context.Context, *emptypb.Empty) (*v1.PluginInfo, error) {
	return &v1.PluginInfo{
		Name:         "debug",
		Version:      version.Version,
		Description:  "Debug server plugin",
		Capabilities: []v1.PluginCapability{},
	}, nil
}

// Configure configures the plugin.
func (p *Plugin) Configure(ctx context.Context, req *v1.PluginConfiguration) (*emptypb.Empty, error) {
	p.closec = make(chan struct{})
	p.servec = make(chan struct{})
	opts := NewDefaultOptions()
	cfg := req.GetConfig().AsMap()
	if len(cfg) > 0 {
		err := mapstructure.Decode(cfg, &opts)
		if err != nil {
			return nil, fmt.Errorf("failed to decode configuration: %w", err)
		}
	}
	if opts.DisablePProf && !opts.EnableDBQuerier {
		return nil, fmt.Errorf("both pprof and db querier are disabled")
	}
	go p.serve(opts)
	return &emptypb.Empty{}, nil
}

// InjectQuerier injects the querier.
func (p *Plugin) InjectQuerier(srv v1.Plugin_InjectQuerierServer) error {
	p.datamux.Lock()
	p.data = plugindb.Open(srv)
	p.datamux.Unlock()
	select {
	case <-p.closec:
		return nil
	case <-srv.Context().Done():
		return srv.Context().Err()
	}
}

// Close closes the plugin.
func (p *Plugin) Close(ctx context.Context, req *emptypb.Empty) (*emptypb.Empty, error) {
	p.datamux.Lock()
	defer p.datamux.Unlock()
	close(p.closec)
	<-p.servec
	return &emptypb.Empty{}, p.data.Close()
}

func (p *Plugin) serve(opts Options) {
	defer close(p.servec)
	log := slog.Default().With("plugin", "debug")
	mux := http.NewServeMux()
	pathPrefix := strings.TrimSuffix(opts.PathPrefix, "/")
	if !opts.DisablePProf {
		pprofProfiles := opts.PprofProfiles
		if len(pprofProfiles) == 0 {
			pprofProfiles = []string{"goroutine", "heap", "allocs", "threadcreate", "block", "mutex"}
		}
		for _, profile := range pprofProfiles {
			mux.Handle(fmt.Sprintf("%s/pprof/%s", pathPrefix, profile), pprof.Handler(profile))
		}
	}
	if opts.EnableDBQuerier {
		mux.HandleFunc(fmt.Sprintf("%s/db/list", pathPrefix), p.handleDBList)
		mux.HandleFunc(fmt.Sprintf("%s/db/get", pathPrefix), p.handleDBGet)
		mux.HandleFunc(fmt.Sprintf("%s/db/iter-prefix", pathPrefix), p.handleDBIterPrefix)
	}
	server := &http.Server{
		Addr:    opts.ListenAddress,
		Handler: logRequest(mux),
		BaseContext: func(_ net.Listener) context.Context {
			return context.WithLogger(context.Background(), log)
		},
	}
	go func() {
		log.Info("starting debug server", "addr", opts.ListenAddress)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("error running debug server", "err", err.Error())
		}
	}()
	<-p.closec
	log.Info("closing debug server")
	if err := server.Shutdown(context.Background()); err != nil {
		log.Error("error closing debug server", "err", err.Error())
	}
}

func (p *Plugin) handleDBList(w http.ResponseWriter, r *http.Request) {
	p.datamux.Lock()
	defer p.datamux.Unlock()
	defer r.Body.Close()
	if p.data == nil {
		http.Error(w, "plugin not configured", http.StatusInternalServerError)
		return
	}
	log := context.LoggerFrom(r.Context())
	prefix := r.URL.Query().Get("q")
	// We are okay with empty prefix, will return all keys
	log.Info("listing keys for prefix from database", "prefix", prefix)
	resp, err := p.data.List(r.Context(), prefix)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Debug("got keys", "resp", resp)
	fmt.Fprint(w, strings.Join(resp, "\n"))
}

func (p *Plugin) handleDBGet(w http.ResponseWriter, r *http.Request) {
	p.datamux.Lock()
	defer p.datamux.Unlock()
	defer r.Body.Close()
	if p.data == nil {
		http.Error(w, "plugin not configured", http.StatusInternalServerError)
		return
	}
	log := context.LoggerFrom(r.Context())
	key := r.URL.Query().Get("q")
	if key == "" {
		log.Error("missing key parameter in request")
		http.Error(w, "missing key", http.StatusBadRequest)
		return
	}
	log.Info("getting key from database", "key", key)
	resp, err := p.data.GetValue(r.Context(), key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp = strings.TrimSpace(resp)
	log.Debug("got key", "key", key, "resp", resp)
	fmt.Fprint(w, resp)
}

func (p *Plugin) handleDBIterPrefix(w http.ResponseWriter, r *http.Request) {
	p.datamux.Lock()
	defer p.datamux.Unlock()
	defer r.Body.Close()
	if p.data == nil {
		http.Error(w, "plugin not configured", http.StatusInternalServerError)
		return
	}
	// TODO: may be pointless to implement this
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

func logRequest(next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log := context.LoggerFrom(r.Context())
		log.Info("request", "method", r.Method, "url", r.URL.String())
		next.ServeHTTP(w, r)
	}
}
