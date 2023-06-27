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

package admin

import (
	"context"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/webmeshproj/node/pkg/store"
)

func newTestServer(t *testing.T) (*Server, func()) {
	t.Helper()
	store, err := store.NewTestStore(context.Background())
	if err != nil {
		t.Fatal(fmt.Errorf("error creating test store: %w", err))
	}
	return New(store, true), func() { store.Close() }
}

type testCase[REQ any] struct {
	name string
	req  *REQ
	code codes.Code
	tval func(*testing.T)
}

type testFunc[REQ, RESP any] func(context.Context, *REQ) (RESP, error)

func runTestCase[REQ, RESP any](t *testing.T, tc testCase[REQ], tf testFunc[REQ, RESP]) {
	t.Run(tc.name, func(t *testing.T) {
		_, err := tf(context.Background(), tc.req)
		if tc.code == codes.OK {
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		} else {
			if err == nil {
				t.Errorf("expected error: %v", tc.code)
			} else {
				status, ok := status.FromError(err)
				if !ok {
					t.Fatalf("expected error to be a status error")
				}
				if status.Code() != tc.code {
					t.Errorf("expected error: %v, got: %v", tc.code, status.Code())
				}
			}
		}
		if tc.tval != nil {
			t.Run("validate", tc.tval)
		}
	})
}
