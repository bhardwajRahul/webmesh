/*
Copyright 2023.

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

// Package connect contains an implementation of the connect subcommand.
// It is used to connect to the mesh as an ephemeral node. It makes certain
// assumptions about the local environment. For example, it assumes the
// local hostname or a random UUID for the local node ID, an in-memory
// raft store, and to join the cluster as an observer.
package connect

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gitlab.com/webmesh/node/pkg/store"
	"gitlab.com/webmesh/node/pkg/store/streamlayer"
	"gitlab.com/webmesh/node/pkg/wireguard"
)

// Options are options for configuring the connect command.
type Options struct {
	// InterfaceName is the name of the wireguard interface to use.
	InterfaceName string
	// ForceTUN is whether to force the use of a TUN interface.
	ForceTUN bool
	// NoModprobe is whether to not attempt to load the wireguard kernel module.
	NoModprobe bool
	// JoinServer is the address of the join server to use.
	JoinServer string
	// RaftPort is the port to use for the Raft transport.
	RaftPort uint16
	// TLSCertFile is the path to a TLS certificate file to use
	// for mTLS.
	TLSCertFile string
	// TLSKeyFile is the path to a TLS key file to use for mTLS.
	TLSKeyFile string
	// TLSCAFile is the path to a CA file for verifying the join
	// server's certificate
	TLSCAFile string
	// Insecure is whether to not use TLS when joining the cluster.
	// This assumes an insecure raft transport as well.
	Insecure bool
	// NoIPv4 is whether to not use IPv4 when joining the cluster.
	NoIPv4 bool
	// NoIPv6 is whether to not use IPv6 when joining the cluster.
	NoIPv6 bool
	// AllowedIPs is a map of peers to allowed IPs.
	AllowedIPs map[string][]string
}

// Connect connects to the mesh as an ephemeral node. The context
// is used to cancel the initial join to the cluster. The stopChan
// is used to stop the node.
func Connect(ctx context.Context, opts Options, stopChan chan struct{}) error {
	// Configure the stream layer
	streamlayerOpts := streamlayer.NewOptions()
	streamlayerOpts.ListenAddress = fmt.Sprintf(":%d", opts.RaftPort)
	streamlayerOpts.TLSCertFile = opts.TLSCertFile
	streamlayerOpts.TLSKeyFile = opts.TLSKeyFile
	streamlayerOpts.TLSCAFile = opts.TLSCAFile
	streamlayerOpts.Insecure = opts.Insecure

	// Configure the raft store
	storeOpts := store.NewOptions()
	storeOpts.InMemory = true
	storeOpts.RaftLogFormat = string(store.RaftLogFormatProtobufSnappy)
	storeOpts.Join = opts.JoinServer
	storeOpts.NoIPv4 = opts.NoIPv4
	storeOpts.NoIPv6 = opts.NoIPv6

	// Configure the wireguard interface
	wireguardOpts := wireguard.NewOptions()
	wireguardOpts.Name = opts.InterfaceName
	wireguardOpts.ForceName = true
	wireguardOpts.ForceTUN = opts.ForceTUN
	wireguardOpts.NoModprobe = opts.NoModprobe
	wireguardOpts.PersistentKeepAlive = time.Second * 5
	var allowedIPs strings.Builder
	for peer, ips := range opts.AllowedIPs {
		allowedIPs.WriteString(fmt.Sprintf("%s=%s", peer, strings.Join(ips, ",")))
	}
	wireguardOpts.AllowedIPs = allowedIPs.String()

	// Create the stream layer
	sl, err := streamlayer.New(streamlayerOpts)
	if err != nil {
		return fmt.Errorf("create stream layer: %w", err)
	}

	// Create the store
	st := store.New(sl, storeOpts, wireguardOpts)
	if err := st.Open(); err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	select {
	case <-stopChan:
		return st.Close()
	case <-ctx.Done():
		return ctx.Err()
	case <-st.ReadyNotify(ctx):
		if ctx.Err() != nil {
			err = fmt.Errorf("wait for store ready: %w", ctx.Err())
			closeErr := st.Close()
			if closeErr != nil {
				err = fmt.Errorf("%w: %w", err, closeErr)
			}
			return err
		}
	}
	<-stopChan
	return st.Close()
}
