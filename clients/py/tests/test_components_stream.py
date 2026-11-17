# SPDX-License-Identifier: MIT
# Copyright (c) 2026 Naisa AI, Inc.

"""Public SDK membership collection over real gRPC, without application dependencies."""
from concurrent.futures import ThreadPoolExecutor
import threading
import re
import time

import grpc
import pytest

from graphengine import GraphEngineClient
from graphengine.types import GraphRef
from graphengine.exceptions import GraphEngineError
from graphengine.gen import graph_engine_pb2 as pb
from graphengine.gen import graph_engine_pb2_grpc as rpc


def members(ids=(), groups=()):
    return pb.ResultChunk(components=pb.ComponentsResult(node_ids_u64=ids, membership=groups))


class MembershipServer(rpc.GraphEngineServicer):
    def __init__(self, frames=None, large=False, wait=False):
        self.frames = frames or []
        self.large = large
        self.wait = wait
        self.metadata = None
        self.cancelled = threading.Event()

    def Run(self, request, context):
        return pb.RunResponse(job=pb.JobRef(job_id="members"))

    def GetJob(self, request, context):
        return pb.GetJobResponse(state=pb.GetJobResponse.SUCCEEDED, result=pb.ResultRef(result_id="members"))

    def GetResult(self, request, context):
        self.metadata = list(context.invocation_metadata())
        context.add_callback(self.cancelled.set)
        if self.large:
            width = 65536 if self.metadata.count(("x-ge-components-chunks", "1")) == 1 else 500000
            for start in range(0, 500000, width):
                end = min(start + width, 500000)
                yield members(range((1 << 56) + start, (1 << 56) + end), (i % 7 for i in range(start, end)))
            yield pb.ResultChunk(done=True)
        else:
            yield from self.frames
        if self.wait:
            while context.is_active():
                time.sleep(.01)


@pytest.fixture
def connect():
    resources = []
    def create(service):
        server = grpc.server(ThreadPoolExecutor(max_workers=2))
        rpc.add_GraphEngineServicer_to_server(service, server)
        port = server.add_insecure_port("127.0.0.1:0")
        server.start()
        client = GraphEngineClient(f"127.0.0.1:{port}", timeout=2, metadata={"authorization": "test-token", "x-ge-components-chunks": "old"})
        client.__enter__()
        resources.append((client, server))
        return client
    yield create
    for client, server in resources:
        client.close()
        server.stop(0).wait()


@pytest.mark.parametrize("method", ["components", "communities"])
def test_large_public_algorithms(connect, method):
    assert members(range(1 << 56, (1 << 56) + 500000), [0] * 500000).ByteSize() > 4 << 20
    service = MembershipServer(large=True)
    result = getattr(connect(service), method)(GraphRef("public"))
    assert len(result.node_ids) == len(result.membership) == 500000
    assert result.num_components == 7
    assert all(node == (1 << 56) + i and result.membership[i] == i % 7 for i, node in enumerate(result.node_ids))
    assert service.metadata.count(("x-ge-components-chunks", "1")) == 1
    assert ("x-ge-components-chunks", "old") not in service.metadata
    assert ("authorization", "test-token") in service.metadata


@pytest.mark.parametrize("frames,count,error", [
    ([members([1, 2], [7, 8]), pb.ResultChunk(done=True)], 2, None),
    ([members(), pb.ResultChunk(done=True)], 0, None),
    ([members([1], [7]), members([2], [8]), pb.ResultChunk(done=True)], 2, None),
    ([pb.ResultChunk(u32=pb.U32Buffer(name="membership", values=[7])), pb.ResultChunk(u32=pb.U32Buffer(name="membership", values=[8])), pb.ResultChunk(done=True)], 2, None),
    ([members([1], [7])], 0, "Incomplete membership result stream"),
    ([pb.ResultChunk(done=True)], 0, "Incomplete membership result stream"),
    ([members([1, 2], [7]), pb.ResultChunk(done=True)], 0, "Invalid membership result: mixed format or mismatched arrays"),
    ([members([1], [7]), pb.ResultChunk(u32=pb.U32Buffer(name="membership", values=[8])), pb.ResultChunk(done=True)], 0, "Invalid membership result: mixed format"),
    ([pb.ResultChunk(u32=pb.U32Buffer(name="membership", values=[8])), members([1], [7]), pb.ResultChunk(done=True)], 0, "Invalid membership result: mixed format or mismatched arrays"),
    ([members(), pb.ResultChunk(done=True), members()], 0, "Invalid membership result: frame after Done"),
    ([members([1], [7]), members([1], [8]), pb.ResultChunk(done=True)], 2, None),
])
def test_validation_and_compatibility(connect, frames, count, error):
    client = connect(MembershipServer(frames))
    if error:
        with pytest.raises(GraphEngineError, match="^" + re.escape(error) + "$"):
            client.communities(GraphRef("public"))
    else:
        result = client.communities(GraphRef("public"))
        assert len(result.membership) == count


def test_rejection_cancels_rpc(connect):
    service = MembershipServer([members([1, 2], [7])], wait=True)
    start = time.monotonic()
    with pytest.raises(GraphEngineError, match="mismatched arrays"):
        connect(service).communities(GraphRef("public"))
    assert time.monotonic() - start < 1
    assert service.cancelled.wait(1)
