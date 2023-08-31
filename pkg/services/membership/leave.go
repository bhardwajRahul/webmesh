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

package membership

import (
	"log/slog"
	"time"

	v1 "github.com/webmeshproj/api/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/webmeshproj/webmesh/pkg/context"
	"github.com/webmeshproj/webmesh/pkg/meshdb/peers"
	"github.com/webmeshproj/webmesh/pkg/services/leaderproxy"
)

func (s *Server) Leave(ctx context.Context, req *v1.LeaveRequest) (*v1.LeaveResponse, error) {
	if !context.IsInNetwork(ctx, s.wg) {
		addr, _ := context.PeerAddrFrom(ctx)
		s.log.Warn("Received Leave request from out of network", slog.String("peer", addr.String()))
		return nil, status.Errorf(codes.PermissionDenied, "request is not in-network")
	}
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	if !s.raft.IsLeader() {
		return nil, status.Errorf(codes.FailedPrecondition, "not leader")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log.Info("Leave request received", slog.Any("request", req))
	// Check that the node is indeed who they say they are
	if s.rbac.IsSecure() {
		if proxiedFor, ok := leaderproxy.ProxiedFor(ctx); ok {
			if proxiedFor != req.GetId() {
				return nil, status.Errorf(codes.PermissionDenied, "proxied for %s, not %s", proxiedFor, req.GetId())
			}
		} else {
			if peer, ok := context.AuthenticatedCallerFrom(ctx); ok {
				if peer != req.GetId() {
					return nil, status.Errorf(codes.PermissionDenied, "peer id %s, not %s", peer, req.GetId())
				}
			} else {
				return nil, status.Error(codes.PermissionDenied, "no peer authentication info in context")
			}
		}
	}

	// Lookup the peer first to make sure they exist
	leaving, err := peers.New(s.raft.Storage()).Get(ctx, req.GetId())
	if err != nil {
		if err == peers.ErrNodeNotFound {
			// We're done here if they don't exist
			return &v1.LeaveResponse{}, nil
		}
		return nil, status.Errorf(codes.Internal, "failed to get peer: %v", err)
	}

	if leaving.PortFor(v1.Feature_RAFT) != 0 {
		defer func() {
			_, _ = s.raft.Barrier(ctx, time.Second*15)
		}()
		s.log.Info("Removing mesh node from raft", "id", req.GetId())
		err := s.raft.RemoveServer(ctx, req.GetId(), false)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to remove raft member: %v", err)
		}
	}

	s.log.Info("Removing mesh node from peers DB", "id", req.GetId())
	err = peers.New(s.raft.Storage()).Delete(ctx, req.GetId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete peer: %v", err)
	}

	go func() {
		// Notify any watching plugins
		if s.plugins != nil && s.plugins.HasWatchers() {
			err := s.plugins.Emit(context.Background(), &v1.Event{
				Type: v1.Event_NODE_JOIN,
				Event: &v1.Event_Node{
					Node: &v1.MeshNode{
						Id:                 leaving.Id,
						PrimaryEndpoint:    leaving.PrimaryEndpoint,
						WireguardEndpoints: leaving.WireguardEndpoints,
						ZoneAwarenessId:    leaving.ZoneAwarenessId,
						PublicKey:          leaving.PublicKey,
						PrivateIpv4:        leaving.PrivateAddrV4().String(),
						PrivateIpv6:        leaving.PrivateAddrV6().String(),
						Features:           leaving.Features,
						JoinedAt:           leaving.JoinedAt,
					},
				},
			})
			if err != nil {
				s.log.Warn("Failed to emit event", "error", err.Error())
			}
		}
	}()

	return &v1.LeaveResponse{}, nil
}
