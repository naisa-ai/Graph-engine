package grpc_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	gepb "github.com/naisa-ai/graph-engine/gen/graphengine/v1"
	grpcserver "github.com/naisa-ai/graph-engine/internal/api/grpc"
	"github.com/naisa-ai/graph-engine/internal/service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestComponentsResultBoundedStream(t *testing.T) {
	for _, tc := range []struct {
		name           string
		count          int
		chunks         bool
		membershipOnly bool
	}{{"large_negotiated", 500000, true, false}, {"legacy_single", 7, false, false}, {"empty_negotiated", 0, true, false}, {"missing_node_ids_negotiated", 500000, true, true}, {"missing_node_ids_unnegotiated", 7, false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			store := service.NewResultStore(4, 128<<20, time.Minute)
			defer store.Close()
			result := service.NewAlgoResult("active", service.AlgoKindCommunities, "test")
			result.Meta["num_components"] = "7"
			result.NodeIDsU64 = make([]uint64, tc.count)
			result.MembershipU32 = make([]uint32, tc.count)
			for n := range result.NodeIDsU64 {
				result.NodeIDsU64[n] = uint64(n) + (1 << 56)
				result.MembershipU32[n] = uint32(n % 7)
			}
			if tc.membershipOnly {
				result.NodeIDsU64 = nil // malformed result: assignments have no node mapping
			}
			if err := store.Store(result); err != nil {
				t.Fatal(err)
			}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			handler := grpcserver.NewGraphEngineHandler(logger, nil, nil, nil, store, nil, nil, nil)
			server := grpc.NewServer()
			gepb.RegisterGraphEngineServer(server, handler)
			lis, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			serveErr := make(chan error, 1)
			go func() { serveErr <- server.Serve(lis) }()
			defer func() {
				server.Stop()
				if err := <-serveErr; err != nil && !errors.Is(err, grpc.ErrServerStopped) {
					t.Errorf("serve test gRPC server: %v", err)
				}
			}()
			conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := conn.Close(); err != nil {
					t.Errorf("close test gRPC connection: %v", err)
				}
			}()
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			if tc.chunks {
				ctx = metadata.AppendToOutgoingContext(ctx, "x-ge-components-chunks", "1")
			}
			stream, err := gepb.NewGraphEngineClient(conn).GetResult(ctx, &gepb.GetResultRequest{Result: &gepb.ResultRef{ResultId: result.ID}})
			if err != nil {
				t.Fatal(err)
			}
			rows, chunks := 0, 0
			done := false
			for {
				chunk, err := stream.Recv()
				if err == io.EOF {
					break
				}
				if err != nil {
					if tc.membershipOnly {
						if status.Code(err) != codes.Internal || status.Convert(err).Message() != "invalid components result: parallel arrays differ" {
							t.Fatalf("missing node IDs: got %v, want explicit parallel-array error", err)
						}
						if rows != 0 || chunks != 0 || done {
							t.Fatal("malformed result emitted data or completion before failing")
						}
						return
					}
					t.Fatalf("bounded default gRPC receive failed: %v", err)
				}
				if tc.chunks && proto.Size(chunk) > 1<<20 {
					t.Fatalf("result frame is not bounded: %d bytes", proto.Size(chunk))
				}
				if data := chunk.GetComponents(); data != nil {
					chunks++
					if len(data.NodeIdsU64) != len(data.Membership) {
						t.Fatal("parallel arrays differ")
					}
					for n, id := range data.NodeIdsU64 {
						if id != uint64(rows+n)+(1<<56) || data.Membership[n] != uint32((rows+n)%7) {
							t.Fatal("stream lost or corrupted membership")
						}
					}
					rows += len(data.NodeIdsU64)
				}
				if chunk.GetU32() != nil {
					t.Fatal("components result unexpectedly emitted membership-only data")
				}
				done = done || chunk.GetDone()
			}
			if tc.membershipOnly {
				t.Fatal("missing node IDs must fail instead of completing successfully")
			}
			if rows != tc.count || !done {
				t.Fatalf("incomplete stream: rows=%d expected=%d done=%v", rows, tc.count, done)
			}
			if tc.count > 65536 && chunks < 2 {
				t.Fatal("large result was not chunked")
			}
			if !tc.chunks && chunks != 1 {
				t.Fatal("legacy single-message contract changed")
			}
		})
	}
}
