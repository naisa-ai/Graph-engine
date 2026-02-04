import datetime

from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf import duration_pb2 as _duration_pb2
from graphengine.v1 import graph_engine_pb2 as _graph_engine_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class HealthRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class HealthResponse(_message.Message):
    __slots__ = ("status", "meta")
    class MetaEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    STATUS_FIELD_NUMBER: _ClassVar[int]
    META_FIELD_NUMBER: _ClassVar[int]
    status: str
    meta: _containers.ScalarMap[str, str]
    def __init__(self, status: _Optional[str] = ..., meta: _Optional[_Mapping[str, str]] = ...) -> None: ...

class ListGraphsRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ListGraphsResponse(_message.Message):
    __slots__ = ("graphs",)
    GRAPHS_FIELD_NUMBER: _ClassVar[int]
    graphs: _containers.RepeatedCompositeFieldContainer[GraphSummary]
    def __init__(self, graphs: _Optional[_Iterable[_Union[GraphSummary, _Mapping]]] = ...) -> None: ...

class GraphSummary(_message.Message):
    __slots__ = ("graph_name", "current_version_id", "vcount", "ecount", "published_at")
    GRAPH_NAME_FIELD_NUMBER: _ClassVar[int]
    CURRENT_VERSION_ID_FIELD_NUMBER: _ClassVar[int]
    VCOUNT_FIELD_NUMBER: _ClassVar[int]
    ECOUNT_FIELD_NUMBER: _ClassVar[int]
    PUBLISHED_AT_FIELD_NUMBER: _ClassVar[int]
    graph_name: str
    current_version_id: str
    vcount: int
    ecount: int
    published_at: _timestamp_pb2.Timestamp
    def __init__(self, graph_name: _Optional[str] = ..., current_version_id: _Optional[str] = ..., vcount: _Optional[int] = ..., ecount: _Optional[int] = ..., published_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class DescribeGraphRequest(_message.Message):
    __slots__ = ("graph",)
    GRAPH_FIELD_NUMBER: _ClassVar[int]
    graph: _graph_engine_pb2.GraphRef
    def __init__(self, graph: _Optional[_Union[_graph_engine_pb2.GraphRef, _Mapping]] = ...) -> None: ...

class DescribeGraphResponse(_message.Message):
    __slots__ = ("summary", "schema", "labels")
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    SUMMARY_FIELD_NUMBER: _ClassVar[int]
    SCHEMA_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    summary: GraphSummary
    schema: _graph_engine_pb2.Schema
    labels: _containers.ScalarMap[str, str]
    def __init__(self, summary: _Optional[_Union[GraphSummary, _Mapping]] = ..., schema: _Optional[_Union[_graph_engine_pb2.Schema, _Mapping]] = ..., labels: _Optional[_Mapping[str, str]] = ...) -> None: ...

class CacheStatsRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class CacheStatsResponse(_message.Message):
    __slots__ = ("results_items", "results_bytes", "views_items", "views_bytes")
    RESULTS_ITEMS_FIELD_NUMBER: _ClassVar[int]
    RESULTS_BYTES_FIELD_NUMBER: _ClassVar[int]
    VIEWS_ITEMS_FIELD_NUMBER: _ClassVar[int]
    VIEWS_BYTES_FIELD_NUMBER: _ClassVar[int]
    results_items: int
    results_bytes: int
    views_items: int
    views_bytes: int
    def __init__(self, results_items: _Optional[int] = ..., results_bytes: _Optional[int] = ..., views_items: _Optional[int] = ..., views_bytes: _Optional[int] = ...) -> None: ...

class ValidateGraphRequest(_message.Message):
    __slots__ = ("graph", "deep")
    GRAPH_FIELD_NUMBER: _ClassVar[int]
    DEEP_FIELD_NUMBER: _ClassVar[int]
    graph: _graph_engine_pb2.GraphRef
    deep: bool
    def __init__(self, graph: _Optional[_Union[_graph_engine_pb2.GraphRef, _Mapping]] = ..., deep: bool = ...) -> None: ...

class ValidateGraphResponse(_message.Message):
    __slots__ = ("status", "warnings", "metrics")
    class MetricsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    STATUS_FIELD_NUMBER: _ClassVar[int]
    WARNINGS_FIELD_NUMBER: _ClassVar[int]
    METRICS_FIELD_NUMBER: _ClassVar[int]
    status: _graph_engine_pb2.Status
    warnings: _containers.RepeatedScalarFieldContainer[str]
    metrics: _containers.ScalarMap[str, str]
    def __init__(self, status: _Optional[_Union[_graph_engine_pb2.Status, _Mapping]] = ..., warnings: _Optional[_Iterable[str]] = ..., metrics: _Optional[_Mapping[str, str]] = ...) -> None: ...

class TraceJobRequest(_message.Message):
    __slots__ = ("job",)
    JOB_FIELD_NUMBER: _ClassVar[int]
    job: _graph_engine_pb2.JobRef
    def __init__(self, job: _Optional[_Union[_graph_engine_pb2.JobRef, _Mapping]] = ...) -> None: ...

class TraceJobResponse(_message.Message):
    __slots__ = ("spans",)
    SPANS_FIELD_NUMBER: _ClassVar[int]
    spans: _containers.RepeatedCompositeFieldContainer[TraceSpan]
    def __init__(self, spans: _Optional[_Iterable[_Union[TraceSpan, _Mapping]]] = ...) -> None: ...

class TraceSpan(_message.Message):
    __slots__ = ("name", "duration", "tags")
    class TagsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    NAME_FIELD_NUMBER: _ClassVar[int]
    DURATION_FIELD_NUMBER: _ClassVar[int]
    TAGS_FIELD_NUMBER: _ClassVar[int]
    name: str
    duration: _duration_pb2.Duration
    tags: _containers.ScalarMap[str, str]
    def __init__(self, name: _Optional[str] = ..., duration: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ..., tags: _Optional[_Mapping[str, str]] = ...) -> None: ...

class ExportSubgraphRequest(_message.Message):
    __slots__ = ("graph", "view", "format", "max_edges")
    class Format(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
        __slots__ = ()
        EDGE_LIST: _ClassVar[ExportSubgraphRequest.Format]
        CSV: _ClassVar[ExportSubgraphRequest.Format]
    EDGE_LIST: ExportSubgraphRequest.Format
    CSV: ExportSubgraphRequest.Format
    GRAPH_FIELD_NUMBER: _ClassVar[int]
    VIEW_FIELD_NUMBER: _ClassVar[int]
    FORMAT_FIELD_NUMBER: _ClassVar[int]
    MAX_EDGES_FIELD_NUMBER: _ClassVar[int]
    graph: _graph_engine_pb2.GraphRef
    view: _graph_engine_pb2.ViewRef
    format: ExportSubgraphRequest.Format
    max_edges: int
    def __init__(self, graph: _Optional[_Union[_graph_engine_pb2.GraphRef, _Mapping]] = ..., view: _Optional[_Union[_graph_engine_pb2.ViewRef, _Mapping]] = ..., format: _Optional[_Union[ExportSubgraphRequest.Format, str]] = ..., max_edges: _Optional[int] = ...) -> None: ...

class ExportChunk(_message.Message):
    __slots__ = ("data",)
    DATA_FIELD_NUMBER: _ClassVar[int]
    data: bytes
    def __init__(self, data: _Optional[bytes] = ...) -> None: ...
