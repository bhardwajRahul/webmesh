//go:build !wasm

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

package libp2p

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multiformats/go-multiaddr"

	"github.com/webmeshproj/webmesh/pkg/context"
	"github.com/webmeshproj/webmesh/pkg/util/crypto"
)

// TestBootstrapTransport tests the libp2p bootstrap transport.
func TestBootstrapTransport(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("SingleNode", func(t *testing.T) {
		rendezvous := fmt.Sprintf("%x", sha256.Sum256([]byte(uuid.New().String())))
		announcer := &mockAnnouncer{}
		transport, err := NewBootstrapTransport(ctx, announcer, BootstrapOptions{
			Rendezvous:      rendezvous,
			PSK:             crypto.MustGeneratePSK(),
			ElectionTimeout: time.Second,
			Host: HostOptions{
				ConnectTimeout: time.Second,
				LocalAddrs: []multiaddr.Multiaddr{
					multiaddr.StringCast("/ip6/::1/tcp/0"),
				},
			},
		})
		if err != nil {
			t.Fatalf("failed to create bootstrap transport: %v", err)
		}
		// Do the leader election
		leader, rt, err := transport.LeaderElect(ctx)
		if err != nil {
			t.Fatalf("failed to leader elect: %v", err)
		}
		// Check that we became the leader.
		if !leader {
			t.Fatalf("expected to become leader")
		}
		// We should not get a round tripper back
		if rt != nil {
			t.Fatalf("expected no round tripper")
		}
		if !announcer.announced {
			t.Fatalf("expected to announce")
		}
	})
}

type mockAnnouncer struct {
	opts      AnnounceOptions
	announced bool
	left      bool
}

// AnnounceToDHT should announce the join protocol to the DHT,
// such that it can be used by a libp2p transport.JoinRoundTripper.
func (m *mockAnnouncer) AnnounceToDHT(ctx context.Context, opts AnnounceOptions) error {
	m.opts = opts
	m.announced = true
	return nil
}

// LeaveDHT should remove the join protocol from the DHT for the
// given rendezvous string.
func (m *mockAnnouncer) LeaveDHT(ctx context.Context, rendezvous string) error {
	m.left = true
	return nil
}
