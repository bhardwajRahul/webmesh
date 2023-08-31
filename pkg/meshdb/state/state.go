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

// Package state provides an interface for querying mesh state.
package state

import (
	"context"
	"database/sql"
	"net/netip"

	"github.com/webmeshproj/webmesh/pkg/meshdb/peers"
	"github.com/webmeshproj/webmesh/pkg/storage"
)

// State is the interface for querying mesh state.
type State interface {
	// GetIPv6Prefix returns the IPv6 prefix.
	GetIPv6Prefix(ctx context.Context) (netip.Prefix, error)
	// GetIPv4Prefix returns the IPv4 prefix.
	GetIPv4Prefix(ctx context.Context) (netip.Prefix, error)
	// GetMeshDomain returns the mesh domain.
	GetMeshDomain(ctx context.Context) (string, error)
	// ListPublicRPCAddresses returns all public gRPC addresses in the mesh.
	// The map key is the node ID.
	ListPublicRPCAddresses(ctx context.Context) (map[string]netip.AddrPort, error)
	// ListPeerPublicRPCAddresses returns all public gRPC addresses in the mesh excluding a node.
	// The map key is the node ID.
	ListPeerPublicRPCAddresses(ctx context.Context, nodeID string) (map[string]netip.AddrPort, error)
	// ListPeerPrivateRPCAddresses returns all private gRPC addresses in the mesh excluding a node.
	// The map key is the node ID.
	ListPeerPrivateRPCAddresses(ctx context.Context, nodeID string) (map[string]netip.AddrPort, error)
}

// ErrNodeNotFound is returned when a node is not found.
var ErrNodeNotFound = sql.ErrNoRows

const (
	// MeshStatePrefix is the prefix for mesh state keys.
	MeshStatePrefix = "/registry/meshstate"
	// IPv6PrefixKey is the key for the IPv6 prefix.
	IPv6PrefixKey = MeshStatePrefix + "/ipv6prefix"
	// IPv4PrefixKey is the key for the IPv4 prefix.
	IPv4PrefixKey = MeshStatePrefix + "/ipv4prefix"
	// MeshDomainKey is the key for the mesh domain.
	MeshDomainKey = MeshStatePrefix + "/meshdomain"
)

type state struct {
	storage.MeshStorage
}

// New returns a new State.
func New(db storage.MeshStorage) State {
	return &state{db}
}

func (s *state) GetIPv6Prefix(ctx context.Context) (netip.Prefix, error) {
	prefix, err := s.GetValue(ctx, IPv6PrefixKey)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.ParsePrefix(prefix)
}

func (s *state) GetIPv4Prefix(ctx context.Context) (netip.Prefix, error) {
	prefix, err := s.GetValue(ctx, IPv4PrefixKey)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.ParsePrefix(prefix)
}

func (s *state) GetMeshDomain(ctx context.Context) (string, error) {
	return s.GetValue(ctx, MeshDomainKey)
}

func (s *state) ListPublicRPCAddresses(ctx context.Context) (map[string]netip.AddrPort, error) {
	nodes, err := peers.New(s).ListPublicNodes(ctx)
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, nil
	}
	out := make(map[string]netip.AddrPort)
	for _, node := range nodes {
		if addr := node.PublicRPCAddr(); addr.IsValid() {
			out[node.GetId()] = addr
		}
	}
	return out, nil
}

func (s *state) ListPeerPublicRPCAddresses(ctx context.Context, nodeID string) (map[string]netip.AddrPort, error) {
	nodes, err := s.ListPublicRPCAddresses(ctx)
	if err != nil {
		return nil, err
	}
	for node := range nodes {
		if node == nodeID {
			delete(nodes, node)
			break
		}
	}
	return nodes, nil
}

func (s *state) ListPeerPrivateRPCAddresses(ctx context.Context, nodeID string) (map[string]netip.AddrPort, error) {
	nodes, err := peers.New(s).List(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]netip.AddrPort)
	for _, node := range nodes {
		if node.GetId() == nodeID {
			continue
		}
		var addr netip.AddrPort
		if node.PrivateRPCAddrV4().IsValid() {
			addr = node.PrivateRPCAddrV4()
		} else {
			addr = node.PrivateRPCAddrV6()
		}
		out[node.GetId()] = addr
	}
	return out, nil
}
