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

package node

import (
	"context"

	v1 "gitlab.com/webmesh/api/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"gitlab.com/webmesh/node/pkg/services/node/peers"
)

func (s *Server) GetNode(ctx context.Context, req *v1.GetNodeRequest) (*v1.MeshNode, error) {
	node, err := s.peers.Get(ctx, req.GetId())
	if err != nil {
		if err == peers.ErrNodeNotFound {
			return nil, status.Errorf(codes.NotFound, "node %s not found", req.GetId())
		}
		return nil, status.Errorf(codes.Internal, "failed to get node: %v", err)
	}
	return dbNodeToAPINode(node), nil
}
