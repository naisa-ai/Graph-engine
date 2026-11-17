// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Naisa AI, Inc.

package graphengine

import (
	"context"
	"net"
	"reflect"
	"testing"
	"time"

	gepb "github.com/naisa-ai/graph-engine/clients/go/gen/graphengine/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

type membershipServer struct {
	gepb.UnimplementedGraphEngineServer
	frames    []*gepb.ResultChunk
	large     bool
	legacy    bool
	seen      chan metadata.MD
	cancelled chan struct{}
}

func (s *membershipServer) Run(context.Context, *gepb.RunRequest) (*gepb.RunResponse, error) {
	return &gepb.RunResponse{Completed: &gepb.GetJobResponse{State: gepb.GetJobResponse_SUCCEEDED, Result: &gepb.ResultRef{ResultId: "members"}}}, nil
}
func members(ids []uint64, groups []uint32) *gepb.ResultChunk {
	return &gepb.ResultChunk{Payload: &gepb.ResultChunk_Components{Components: &gepb.ComponentsResult{NodeIdsU64: ids, Membership: groups}}}
}
func endMembers() *gepb.ResultChunk {
	return &gepb.ResultChunk{Payload: &gepb.ResultChunk_Done{Done: true}}
}
func (s *membershipServer) GetResult(_ *gepb.GetResultRequest, stream gepb.GraphEngine_GetResultServer) error {
	md, _ := metadata.FromIncomingContext(stream.Context())
	if s.seen != nil {
		s.seen <- md.Copy()
	}
	frames := s.frames
	if s.large {
		ids := make([]uint64, 500000)
		groups := make([]uint32, len(ids))
		for i := range ids {
			ids[i] = 1<<56 + uint64(i)
			groups[i] = uint32(i % 7)
		}
		width := len(ids)
		if !s.legacy && reflect.DeepEqual(md.Get("x-ge-components-chunks"), []string{"1"}) {
			width = 65536
		}
		for start := 0; start < len(ids); start += width {
			stop := start + width
			if stop > len(ids) {
				stop = len(ids)
			}
			frames = append(frames, members(ids[start:stop], groups[start:stop]))
		}
		frames = append(frames, endMembers())
	}
	for _, f := range frames {
		if err := stream.Send(f); err != nil {
			return err
		}
	}
	if s.cancelled != nil {
		<-stream.Context().Done()
		close(s.cancelled)
		return stream.Context().Err()
	}
	return nil
}
func membershipClient(t *testing.T, s *membershipServer) *Client {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	gepb.RegisterGraphEngineServer(server, s)
	go server.Serve(lis)
	t.Cleanup(func() { server.Stop(); lis.Close() })
	c, err := NewClient(lis.Addr().String(), WithInsecure(), WithTimeout(5*time.Second), WithMetadata(map[string]string{"authorization": "test-token"}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}
func TestMembershipPublicAlgorithmsLarge(t *testing.T) {
	ids := make([]uint64, 500000)
	groups := make([]uint32, len(ids))
	for i := range ids {
		ids[i] = 1<<56 + uint64(i)
	}
	if proto.Size(members(ids, groups)) <= 4<<20 {
		t.Fatal("fixture must exceed default gRPC cap")
	}
	for _, method := range []string{"components", "communities", "view"} {
		t.Run(method, func(t *testing.T) {
			seen := make(chan metadata.MD, 1)
			c := membershipClient(t, &membershipServer{large: true, seen: seen})
			ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("x-ge-components-chunks", "old", "x-request-id", "caller"))
			var result *ComponentsResult
			var err error
			switch method {
			case "components":
				result, err = c.Components(ctx, &gepb.GraphRef{GraphName: "public"})
			case "communities":
				result, err = c.Communities(ctx, &gepb.GraphRef{GraphName: "public"})
			default:
				result, err = c.CommunitiesOnView(ctx, &gepb.ViewRef{ViewId: "public-view"})
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(result.NodeIDs) != 500000 || len(result.Membership) != 500000 || result.NumComponents != 7 {
				t.Fatalf("incomplete result: nodes=%d memberships=%d groups=%d", len(result.NodeIDs), len(result.Membership), result.NumComponents)
			}
			for i, id := range result.NodeIDs {
				if id != 1<<56+uint64(i) || result.Membership[i] != uint32(i%7) {
					t.Fatalf("corrupt row %d", i)
				}
			}
			md := <-seen
			for k, want := range map[string][]string{"x-ge-components-chunks": {"1"}, "authorization": {"test-token"}, "x-request-id": {"caller"}} {
				if !reflect.DeepEqual(md.Get(k), want) {
					t.Errorf("metadata %s=%v", k, md.Get(k))
				}
			}
		})
	}
}
func TestMembershipValidationAndCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name      string
		frames    []*gepb.ResultChunk
		wantError bool
		count     int
	}{
		{"legacy_single", []*gepb.ResultChunk{members([]uint64{1, 2}, []uint32{7, 8}), endMembers()}, false, 2},
		{"empty", []*gepb.ResultChunk{members(nil, nil), endMembers()}, false, 0},
		{"multiple", []*gepb.ResultChunk{members([]uint64{1}, []uint32{7}), members([]uint64{2}, []uint32{8}), endMembers()}, false, 2},
		{"legacy_buffers", []*gepb.ResultChunk{{Payload: &gepb.ResultChunk_U32{U32: &gepb.U32Buffer{Name: "membership", Values: []uint32{7}}}}, {Payload: &gepb.ResultChunk_U32{U32: &gepb.U32Buffer{Name: "membership", Values: []uint32{8}}}}, endMembers()}, false, 2},
		{"truncated", []*gepb.ResultChunk{members([]uint64{1}, []uint32{7})}, true, 0},
		{"no_data", []*gepb.ResultChunk{endMembers()}, true, 0},
		{"mismatch", []*gepb.ResultChunk{members([]uint64{1, 2}, []uint32{7}), endMembers()}, true, 0},
		{"trailing", []*gepb.ResultChunk{members(nil, nil), endMembers(), members(nil, nil)}, true, 0},
		{"conflict", []*gepb.ResultChunk{members([]uint64{1}, []uint32{7}), members([]uint64{1}, []uint32{8}), endMembers()}, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := membershipClient(t, &membershipServer{frames: tc.frames})
			r, err := c.Communities(context.Background(), &gepb.GraphRef{GraphName: "public"})
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v wantError=%v", err, tc.wantError)
			}
			if err == nil && len(r.Membership) != tc.count {
				t.Fatalf("lost rows: %v", r)
			}
		})
	}
}
func TestMembershipRejectedStreamCancels(t *testing.T) {
	cancelled := make(chan struct{})
	c := membershipClient(t, &membershipServer{frames: []*gepb.ResultChunk{members([]uint64{1, 2}, []uint32{7})}, cancelled: cancelled})
	start := time.Now()
	_, err := c.Communities(context.Background(), &gepb.GraphRef{GraphName: "public"})
	if time.Since(start) > time.Second {
		t.Fatal("validation waited for RPC timeout")
	}
	if err == nil {
		t.Fatal("accepted malformed data")
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("result RPC not cancelled")
	}
}

func TestMembershipCallerCancellation(t *testing.T) {
	cancelled := make(chan struct{})
	c := membershipClient(t, &membershipServer{cancelled: cancelled})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := c.GetComponentsResult(ctx, &gepb.ResultRef{ResultId: "members"})
	if err == nil {
		t.Fatal("accepted cancelled stream")
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("server did not observe cancellation")
	}
}
