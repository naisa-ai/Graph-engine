import datetime

from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf import duration_pb2 as _duration_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class ColumnType(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    COL_BOOL: _ClassVar[ColumnType]
    COL_U32: _ClassVar[ColumnType]
    COL_U64: _ClassVar[ColumnType]
    COL_F32: _ClassVar[ColumnType]
    COL_F64: _ClassVar[ColumnType]
    COL_STRING: _ClassVar[ColumnType]
COL_BOOL: ColumnType
COL_U32: ColumnType
COL_U64: ColumnType
COL_F32: ColumnType
COL_F64: ColumnType
COL_STRING: ColumnType

class GraphRef(_message.Message):
    __slots__ = ("graph_name", "version_id")
    GRAPH_NAME_FIELD_NUMBER: _ClassVar[int]
    VERSION_ID_FIELD_NUMBER: _ClassVar[int]
    graph_name: str
    version_id: str
    def __init__(self, graph_name: _Optional[str] = ..., version_id: _Optional[str] = ...) -> None: ...

class ViewRef(_message.Message):
    __slots__ = ("view_id",)
    VIEW_ID_FIELD_NUMBER: _ClassVar[int]
    view_id: str
    def __init__(self, view_id: _Optional[str] = ...) -> None: ...

class ResultRef(_message.Message):
    __slots__ = ("result_id",)
    RESULT_ID_FIELD_NUMBER: _ClassVar[int]
    result_id: str
    def __init__(self, result_id: _Optional[str] = ...) -> None: ...

class JobRef(_message.Message):
    __slots__ = ("job_id",)
    JOB_ID_FIELD_NUMBER: _ClassVar[int]
    job_id: str
    def __init__(self, job_id: _Optional[str] = ...) -> None: ...

class BeginBuildRequest(_message.Message):
    __slots__ = ("graph_name", "directed", "node_id_format", "schema", "labels")
    class NodeIdFormat(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
        __slots__ = ()
        NODE_ID_UINT64: _ClassVar[BeginBuildRequest.NodeIdFormat]
        NODE_ID_STRING: _ClassVar[BeginBuildRequest.NodeIdFormat]
    NODE_ID_UINT64: BeginBuildRequest.NodeIdFormat
    NODE_ID_STRING: BeginBuildRequest.NodeIdFormat
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    GRAPH_NAME_FIELD_NUMBER: _ClassVar[int]
    DIRECTED_FIELD_NUMBER: _ClassVar[int]
    NODE_ID_FORMAT_FIELD_NUMBER: _ClassVar[int]
    SCHEMA_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    graph_name: str
    directed: bool
    node_id_format: BeginBuildRequest.NodeIdFormat
    schema: Schema
    labels: _containers.ScalarMap[str, str]
    def __init__(self, graph_name: _Optional[str] = ..., directed: bool = ..., node_id_format: _Optional[_Union[BeginBuildRequest.NodeIdFormat, str]] = ..., schema: _Optional[_Union[Schema, _Mapping]] = ..., labels: _Optional[_Mapping[str, str]] = ...) -> None: ...

class BeginBuildResponse(_message.Message):
    __slots__ = ("build_id",)
    BUILD_ID_FIELD_NUMBER: _ClassVar[int]
    build_id: str
    def __init__(self, build_id: _Optional[str] = ...) -> None: ...

class UploadRequest(_message.Message):
    __slots__ = ("build_id", "vertices", "edges", "vertex_columns", "edge_columns", "finalize")
    BUILD_ID_FIELD_NUMBER: _ClassVar[int]
    VERTICES_FIELD_NUMBER: _ClassVar[int]
    EDGES_FIELD_NUMBER: _ClassVar[int]
    VERTEX_COLUMNS_FIELD_NUMBER: _ClassVar[int]
    EDGE_COLUMNS_FIELD_NUMBER: _ClassVar[int]
    FINALIZE_FIELD_NUMBER: _ClassVar[int]
    build_id: str
    vertices: VertexChunk
    edges: EdgeChunk
    vertex_columns: ColumnChunk
    edge_columns: ColumnChunk
    finalize: bool
    def __init__(self, build_id: _Optional[str] = ..., vertices: _Optional[_Union[VertexChunk, _Mapping]] = ..., edges: _Optional[_Union[EdgeChunk, _Mapping]] = ..., vertex_columns: _Optional[_Union[ColumnChunk, _Mapping]] = ..., edge_columns: _Optional[_Union[ColumnChunk, _Mapping]] = ..., finalize: bool = ...) -> None: ...

class UploadResponse(_message.Message):
    __slots__ = ("build_id", "received_vertices", "received_edges")
    BUILD_ID_FIELD_NUMBER: _ClassVar[int]
    RECEIVED_VERTICES_FIELD_NUMBER: _ClassVar[int]
    RECEIVED_EDGES_FIELD_NUMBER: _ClassVar[int]
    build_id: str
    received_vertices: int
    received_edges: int
    def __init__(self, build_id: _Optional[str] = ..., received_vertices: _Optional[int] = ..., received_edges: _Optional[int] = ...) -> None: ...

class PublishBuildRequest(_message.Message):
    __slots__ = ("build_id", "artifacts", "allow_warnings")
    BUILD_ID_FIELD_NUMBER: _ClassVar[int]
    ARTIFACTS_FIELD_NUMBER: _ClassVar[int]
    ALLOW_WARNINGS_FIELD_NUMBER: _ClassVar[int]
    build_id: str
    artifacts: BatchArtifacts
    allow_warnings: bool
    def __init__(self, build_id: _Optional[str] = ..., artifacts: _Optional[_Union[BatchArtifacts, _Mapping]] = ..., allow_warnings: bool = ...) -> None: ...

class PublishBuildResponse(_message.Message):
    __slots__ = ("graph", "status")
    GRAPH_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    graph: GraphRef
    status: Status
    def __init__(self, graph: _Optional[_Union[GraphRef, _Mapping]] = ..., status: _Optional[_Union[Status, _Mapping]] = ...) -> None: ...

class Status(_message.Message):
    __slots__ = ("code", "message")
    CODE_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    code: int
    message: str
    def __init__(self, code: _Optional[int] = ..., message: _Optional[str] = ...) -> None: ...

class BatchArtifacts(_message.Message):
    __slots__ = ("compute_components", "compute_communities", "compute_kcore", "compute_betweenness_sampled")
    COMPUTE_COMPONENTS_FIELD_NUMBER: _ClassVar[int]
    COMPUTE_COMMUNITIES_FIELD_NUMBER: _ClassVar[int]
    COMPUTE_KCORE_FIELD_NUMBER: _ClassVar[int]
    COMPUTE_BETWEENNESS_SAMPLED_FIELD_NUMBER: _ClassVar[int]
    compute_components: bool
    compute_communities: bool
    compute_kcore: bool
    compute_betweenness_sampled: bool
    def __init__(self, compute_components: bool = ..., compute_communities: bool = ..., compute_kcore: bool = ..., compute_betweenness_sampled: bool = ...) -> None: ...

class Schema(_message.Message):
    __slots__ = ("vertex_columns", "edge_columns")
    VERTEX_COLUMNS_FIELD_NUMBER: _ClassVar[int]
    EDGE_COLUMNS_FIELD_NUMBER: _ClassVar[int]
    vertex_columns: _containers.RepeatedCompositeFieldContainer[ColumnDef]
    edge_columns: _containers.RepeatedCompositeFieldContainer[ColumnDef]
    def __init__(self, vertex_columns: _Optional[_Iterable[_Union[ColumnDef, _Mapping]]] = ..., edge_columns: _Optional[_Iterable[_Union[ColumnDef, _Mapping]]] = ...) -> None: ...

class ColumnDef(_message.Message):
    __slots__ = ("name", "type")
    NAME_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    name: str
    type: ColumnType
    def __init__(self, name: _Optional[str] = ..., type: _Optional[_Union[ColumnType, str]] = ...) -> None: ...

class VertexChunk(_message.Message):
    __slots__ = ("node_id_u64", "node_id_str")
    NODE_ID_U64_FIELD_NUMBER: _ClassVar[int]
    NODE_ID_STR_FIELD_NUMBER: _ClassVar[int]
    node_id_u64: _containers.RepeatedScalarFieldContainer[int]
    node_id_str: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, node_id_u64: _Optional[_Iterable[int]] = ..., node_id_str: _Optional[_Iterable[str]] = ...) -> None: ...

class EdgeChunk(_message.Message):
    __slots__ = ("src_u64", "dst_u64", "src_str", "dst_str", "edge_id_u64", "kind", "weight")
    SRC_U64_FIELD_NUMBER: _ClassVar[int]
    DST_U64_FIELD_NUMBER: _ClassVar[int]
    SRC_STR_FIELD_NUMBER: _ClassVar[int]
    DST_STR_FIELD_NUMBER: _ClassVar[int]
    EDGE_ID_U64_FIELD_NUMBER: _ClassVar[int]
    KIND_FIELD_NUMBER: _ClassVar[int]
    WEIGHT_FIELD_NUMBER: _ClassVar[int]
    src_u64: _containers.RepeatedScalarFieldContainer[int]
    dst_u64: _containers.RepeatedScalarFieldContainer[int]
    src_str: _containers.RepeatedScalarFieldContainer[str]
    dst_str: _containers.RepeatedScalarFieldContainer[str]
    edge_id_u64: _containers.RepeatedScalarFieldContainer[int]
    kind: _containers.RepeatedScalarFieldContainer[int]
    weight: _containers.RepeatedScalarFieldContainer[float]
    def __init__(self, src_u64: _Optional[_Iterable[int]] = ..., dst_u64: _Optional[_Iterable[int]] = ..., src_str: _Optional[_Iterable[str]] = ..., dst_str: _Optional[_Iterable[str]] = ..., edge_id_u64: _Optional[_Iterable[int]] = ..., kind: _Optional[_Iterable[int]] = ..., weight: _Optional[_Iterable[float]] = ...) -> None: ...

class ColumnChunk(_message.Message):
    __slots__ = ("name", "type", "v_bool", "v_u32", "v_u64", "v_f32", "v_f64", "v_string")
    NAME_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    V_BOOL_FIELD_NUMBER: _ClassVar[int]
    V_U32_FIELD_NUMBER: _ClassVar[int]
    V_U64_FIELD_NUMBER: _ClassVar[int]
    V_F32_FIELD_NUMBER: _ClassVar[int]
    V_F64_FIELD_NUMBER: _ClassVar[int]
    V_STRING_FIELD_NUMBER: _ClassVar[int]
    name: str
    type: ColumnType
    v_bool: _containers.RepeatedScalarFieldContainer[bool]
    v_u32: _containers.RepeatedScalarFieldContainer[int]
    v_u64: _containers.RepeatedScalarFieldContainer[int]
    v_f32: _containers.RepeatedScalarFieldContainer[float]
    v_f64: _containers.RepeatedScalarFieldContainer[float]
    v_string: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, name: _Optional[str] = ..., type: _Optional[_Union[ColumnType, str]] = ..., v_bool: _Optional[_Iterable[bool]] = ..., v_u32: _Optional[_Iterable[int]] = ..., v_u64: _Optional[_Iterable[int]] = ..., v_f32: _Optional[_Iterable[float]] = ..., v_f64: _Optional[_Iterable[float]] = ..., v_string: _Optional[_Iterable[str]] = ...) -> None: ...

class CreateViewRequest(_message.Message):
    __slots__ = ("graph", "spec")
    GRAPH_FIELD_NUMBER: _ClassVar[int]
    SPEC_FIELD_NUMBER: _ClassVar[int]
    graph: GraphRef
    spec: ViewSpec
    def __init__(self, graph: _Optional[_Union[GraphRef, _Mapping]] = ..., spec: _Optional[_Union[ViewSpec, _Mapping]] = ...) -> None: ...

class CreateViewResponse(_message.Message):
    __slots__ = ("view", "vcount", "ecount")
    VIEW_FIELD_NUMBER: _ClassVar[int]
    VCOUNT_FIELD_NUMBER: _ClassVar[int]
    ECOUNT_FIELD_NUMBER: _ClassVar[int]
    view: ViewRef
    vcount: int
    ecount: int
    def __init__(self, view: _Optional[_Union[ViewRef, _Mapping]] = ..., vcount: _Optional[int] = ..., ecount: _Optional[int] = ...) -> None: ...

class ViewSpec(_message.Message):
    __slots__ = ("vfilter", "efilter", "induce_vertices_u64", "induce_vertices_str", "neighborhood", "exclude_vertices_u64", "exclude_edges_u64")
    VFILTER_FIELD_NUMBER: _ClassVar[int]
    EFILTER_FIELD_NUMBER: _ClassVar[int]
    INDUCE_VERTICES_U64_FIELD_NUMBER: _ClassVar[int]
    INDUCE_VERTICES_STR_FIELD_NUMBER: _ClassVar[int]
    NEIGHBORHOOD_FIELD_NUMBER: _ClassVar[int]
    EXCLUDE_VERTICES_U64_FIELD_NUMBER: _ClassVar[int]
    EXCLUDE_EDGES_U64_FIELD_NUMBER: _ClassVar[int]
    vfilter: VertexFilter
    efilter: EdgeFilter
    induce_vertices_u64: _containers.RepeatedScalarFieldContainer[int]
    induce_vertices_str: _containers.RepeatedScalarFieldContainer[str]
    neighborhood: NeighborhoodSpec
    exclude_vertices_u64: _containers.RepeatedScalarFieldContainer[int]
    exclude_edges_u64: _containers.RepeatedScalarFieldContainer[int]
    def __init__(self, vfilter: _Optional[_Union[VertexFilter, _Mapping]] = ..., efilter: _Optional[_Union[EdgeFilter, _Mapping]] = ..., induce_vertices_u64: _Optional[_Iterable[int]] = ..., induce_vertices_str: _Optional[_Iterable[str]] = ..., neighborhood: _Optional[_Union[NeighborhoodSpec, _Mapping]] = ..., exclude_vertices_u64: _Optional[_Iterable[int]] = ..., exclude_edges_u64: _Optional[_Iterable[int]] = ...) -> None: ...

class NeighborhoodSpec(_message.Message):
    __slots__ = ("seeds_u64", "hops", "mode")
    class Mode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
        __slots__ = ()
        MODE_ALL: _ClassVar[NeighborhoodSpec.Mode]
        MODE_OUT: _ClassVar[NeighborhoodSpec.Mode]
        MODE_IN: _ClassVar[NeighborhoodSpec.Mode]
    MODE_ALL: NeighborhoodSpec.Mode
    MODE_OUT: NeighborhoodSpec.Mode
    MODE_IN: NeighborhoodSpec.Mode
    SEEDS_U64_FIELD_NUMBER: _ClassVar[int]
    HOPS_FIELD_NUMBER: _ClassVar[int]
    MODE_FIELD_NUMBER: _ClassVar[int]
    seeds_u64: _containers.RepeatedScalarFieldContainer[int]
    hops: int
    mode: NeighborhoodSpec.Mode
    def __init__(self, seeds_u64: _Optional[_Iterable[int]] = ..., hops: _Optional[int] = ..., mode: _Optional[_Union[NeighborhoodSpec.Mode, str]] = ...) -> None: ...

class VertexFilter(_message.Message):
    __slots__ = ("predicates",)
    PREDICATES_FIELD_NUMBER: _ClassVar[int]
    predicates: _containers.RepeatedCompositeFieldContainer[Predicate]
    def __init__(self, predicates: _Optional[_Iterable[_Union[Predicate, _Mapping]]] = ...) -> None: ...

class EdgeFilter(_message.Message):
    __slots__ = ("predicates",)
    PREDICATES_FIELD_NUMBER: _ClassVar[int]
    predicates: _containers.RepeatedCompositeFieldContainer[Predicate]
    def __init__(self, predicates: _Optional[_Iterable[_Union[Predicate, _Mapping]]] = ...) -> None: ...

class Predicate(_message.Message):
    __slots__ = ("column", "op", "b", "u32", "u64", "f32", "f64", "s", "u32s", "u64s", "ss", "range_u32", "range_f64")
    class Op(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
        __slots__ = ()
        OP_EQ: _ClassVar[Predicate.Op]
        OP_IN: _ClassVar[Predicate.Op]
        OP_RANGE: _ClassVar[Predicate.Op]
        OP_EXISTS: _ClassVar[Predicate.Op]
    OP_EQ: Predicate.Op
    OP_IN: Predicate.Op
    OP_RANGE: Predicate.Op
    OP_EXISTS: Predicate.Op
    COLUMN_FIELD_NUMBER: _ClassVar[int]
    OP_FIELD_NUMBER: _ClassVar[int]
    B_FIELD_NUMBER: _ClassVar[int]
    U32_FIELD_NUMBER: _ClassVar[int]
    U64_FIELD_NUMBER: _ClassVar[int]
    F32_FIELD_NUMBER: _ClassVar[int]
    F64_FIELD_NUMBER: _ClassVar[int]
    S_FIELD_NUMBER: _ClassVar[int]
    U32S_FIELD_NUMBER: _ClassVar[int]
    U64S_FIELD_NUMBER: _ClassVar[int]
    SS_FIELD_NUMBER: _ClassVar[int]
    RANGE_U32_FIELD_NUMBER: _ClassVar[int]
    RANGE_F64_FIELD_NUMBER: _ClassVar[int]
    column: str
    op: Predicate.Op
    b: bool
    u32: int
    u64: int
    f32: float
    f64: float
    s: str
    u32s: U32List
    u64s: U64List
    ss: StringList
    range_u32: RangeU32
    range_f64: RangeF64
    def __init__(self, column: _Optional[str] = ..., op: _Optional[_Union[Predicate.Op, str]] = ..., b: bool = ..., u32: _Optional[int] = ..., u64: _Optional[int] = ..., f32: _Optional[float] = ..., f64: _Optional[float] = ..., s: _Optional[str] = ..., u32s: _Optional[_Union[U32List, _Mapping]] = ..., u64s: _Optional[_Union[U64List, _Mapping]] = ..., ss: _Optional[_Union[StringList, _Mapping]] = ..., range_u32: _Optional[_Union[RangeU32, _Mapping]] = ..., range_f64: _Optional[_Union[RangeF64, _Mapping]] = ...) -> None: ...

class U32List(_message.Message):
    __slots__ = ("values",)
    VALUES_FIELD_NUMBER: _ClassVar[int]
    values: _containers.RepeatedScalarFieldContainer[int]
    def __init__(self, values: _Optional[_Iterable[int]] = ...) -> None: ...

class U64List(_message.Message):
    __slots__ = ("values",)
    VALUES_FIELD_NUMBER: _ClassVar[int]
    values: _containers.RepeatedScalarFieldContainer[int]
    def __init__(self, values: _Optional[_Iterable[int]] = ...) -> None: ...

class StringList(_message.Message):
    __slots__ = ("values",)
    VALUES_FIELD_NUMBER: _ClassVar[int]
    values: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, values: _Optional[_Iterable[str]] = ...) -> None: ...

class RangeU32(_message.Message):
    __slots__ = ("lo", "hi")
    LO_FIELD_NUMBER: _ClassVar[int]
    HI_FIELD_NUMBER: _ClassVar[int]
    lo: int
    hi: int
    def __init__(self, lo: _Optional[int] = ..., hi: _Optional[int] = ...) -> None: ...

class RangeF64(_message.Message):
    __slots__ = ("lo", "hi")
    LO_FIELD_NUMBER: _ClassVar[int]
    HI_FIELD_NUMBER: _ClassVar[int]
    lo: float
    hi: float
    def __init__(self, lo: _Optional[float] = ..., hi: _Optional[float] = ...) -> None: ...

class RunRequest(_message.Message):
    __slots__ = ("graph", "view", "algo", "timeout", "priority", "allow_cache", "sync_timeout")
    GRAPH_FIELD_NUMBER: _ClassVar[int]
    VIEW_FIELD_NUMBER: _ClassVar[int]
    ALGO_FIELD_NUMBER: _ClassVar[int]
    TIMEOUT_FIELD_NUMBER: _ClassVar[int]
    PRIORITY_FIELD_NUMBER: _ClassVar[int]
    ALLOW_CACHE_FIELD_NUMBER: _ClassVar[int]
    SYNC_TIMEOUT_FIELD_NUMBER: _ClassVar[int]
    graph: GraphRef
    view: ViewRef
    algo: AlgoSpec
    timeout: _duration_pb2.Duration
    priority: int
    allow_cache: bool
    sync_timeout: _duration_pb2.Duration
    def __init__(self, graph: _Optional[_Union[GraphRef, _Mapping]] = ..., view: _Optional[_Union[ViewRef, _Mapping]] = ..., algo: _Optional[_Union[AlgoSpec, _Mapping]] = ..., timeout: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ..., priority: _Optional[int] = ..., allow_cache: bool = ..., sync_timeout: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ...) -> None: ...

class RunResponse(_message.Message):
    __slots__ = ("job", "completed")
    JOB_FIELD_NUMBER: _ClassVar[int]
    COMPLETED_FIELD_NUMBER: _ClassVar[int]
    job: JobRef
    completed: GetJobResponse
    def __init__(self, job: _Optional[_Union[JobRef, _Mapping]] = ..., completed: _Optional[_Union[GetJobResponse, _Mapping]] = ...) -> None: ...

class AlgoSpec(_message.Message):
    __slots__ = ("components", "communities", "shortest_path", "k_shortest_paths", "distances", "bfs", "neighborhood", "st_mincut", "corridor", "kcore", "betweenness", "closeness", "pagerank")
    COMPONENTS_FIELD_NUMBER: _ClassVar[int]
    COMMUNITIES_FIELD_NUMBER: _ClassVar[int]
    SHORTEST_PATH_FIELD_NUMBER: _ClassVar[int]
    K_SHORTEST_PATHS_FIELD_NUMBER: _ClassVar[int]
    DISTANCES_FIELD_NUMBER: _ClassVar[int]
    BFS_FIELD_NUMBER: _ClassVar[int]
    NEIGHBORHOOD_FIELD_NUMBER: _ClassVar[int]
    ST_MINCUT_FIELD_NUMBER: _ClassVar[int]
    CORRIDOR_FIELD_NUMBER: _ClassVar[int]
    KCORE_FIELD_NUMBER: _ClassVar[int]
    BETWEENNESS_FIELD_NUMBER: _ClassVar[int]
    CLOSENESS_FIELD_NUMBER: _ClassVar[int]
    PAGERANK_FIELD_NUMBER: _ClassVar[int]
    components: ComponentsSpec
    communities: CommunitiesSpec
    shortest_path: ShortestPathSpec
    k_shortest_paths: KShortestPathsSpec
    distances: DistancesSpec
    bfs: BFSSpec
    neighborhood: NeighborhoodQuerySpec
    st_mincut: STMinCutSpec
    corridor: CorridorSpec
    kcore: KCoreSpec
    betweenness: BetweennessSpec
    closeness: ClosenessSpec
    pagerank: PageRankSpec
    def __init__(self, components: _Optional[_Union[ComponentsSpec, _Mapping]] = ..., communities: _Optional[_Union[CommunitiesSpec, _Mapping]] = ..., shortest_path: _Optional[_Union[ShortestPathSpec, _Mapping]] = ..., k_shortest_paths: _Optional[_Union[KShortestPathsSpec, _Mapping]] = ..., distances: _Optional[_Union[DistancesSpec, _Mapping]] = ..., bfs: _Optional[_Union[BFSSpec, _Mapping]] = ..., neighborhood: _Optional[_Union[NeighborhoodQuerySpec, _Mapping]] = ..., st_mincut: _Optional[_Union[STMinCutSpec, _Mapping]] = ..., corridor: _Optional[_Union[CorridorSpec, _Mapping]] = ..., kcore: _Optional[_Union[KCoreSpec, _Mapping]] = ..., betweenness: _Optional[_Union[BetweennessSpec, _Mapping]] = ..., closeness: _Optional[_Union[ClosenessSpec, _Mapping]] = ..., pagerank: _Optional[_Union[PageRankSpec, _Mapping]] = ...) -> None: ...

class ComponentsSpec(_message.Message):
    __slots__ = ("mode",)
    class Mode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
        __slots__ = ()
        WEAK: _ClassVar[ComponentsSpec.Mode]
        STRONG: _ClassVar[ComponentsSpec.Mode]
    WEAK: ComponentsSpec.Mode
    STRONG: ComponentsSpec.Mode
    MODE_FIELD_NUMBER: _ClassVar[int]
    mode: ComponentsSpec.Mode
    def __init__(self, mode: _Optional[_Union[ComponentsSpec.Mode, str]] = ...) -> None: ...

class CommunitiesSpec(_message.Message):
    __slots__ = ("method", "resolution", "steps", "spins", "gamma", "trials")
    class Method(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
        __slots__ = ()
        LEIDEN: _ClassVar[CommunitiesSpec.Method]
        LOUVAIN: _ClassVar[CommunitiesSpec.Method]
        LABEL_PROPAGATION: _ClassVar[CommunitiesSpec.Method]
        INFOMAP: _ClassVar[CommunitiesSpec.Method]
        WALKTRAP: _ClassVar[CommunitiesSpec.Method]
        FAST_GREEDY: _ClassVar[CommunitiesSpec.Method]
        EDGE_BETWEENNESS: _ClassVar[CommunitiesSpec.Method]
        LEADING_EIGENVECTOR: _ClassVar[CommunitiesSpec.Method]
        SPINGLASS: _ClassVar[CommunitiesSpec.Method]
    LEIDEN: CommunitiesSpec.Method
    LOUVAIN: CommunitiesSpec.Method
    LABEL_PROPAGATION: CommunitiesSpec.Method
    INFOMAP: CommunitiesSpec.Method
    WALKTRAP: CommunitiesSpec.Method
    FAST_GREEDY: CommunitiesSpec.Method
    EDGE_BETWEENNESS: CommunitiesSpec.Method
    LEADING_EIGENVECTOR: CommunitiesSpec.Method
    SPINGLASS: CommunitiesSpec.Method
    METHOD_FIELD_NUMBER: _ClassVar[int]
    RESOLUTION_FIELD_NUMBER: _ClassVar[int]
    STEPS_FIELD_NUMBER: _ClassVar[int]
    SPINS_FIELD_NUMBER: _ClassVar[int]
    GAMMA_FIELD_NUMBER: _ClassVar[int]
    TRIALS_FIELD_NUMBER: _ClassVar[int]
    method: CommunitiesSpec.Method
    resolution: float
    steps: int
    spins: int
    gamma: float
    trials: int
    def __init__(self, method: _Optional[_Union[CommunitiesSpec.Method, str]] = ..., resolution: _Optional[float] = ..., steps: _Optional[int] = ..., spins: _Optional[int] = ..., gamma: _Optional[float] = ..., trials: _Optional[int] = ...) -> None: ...

class ShortestPathSpec(_message.Message):
    __slots__ = ("source_u64", "target_u64", "weight_column", "return_edges", "return_vertices")
    SOURCE_U64_FIELD_NUMBER: _ClassVar[int]
    TARGET_U64_FIELD_NUMBER: _ClassVar[int]
    WEIGHT_COLUMN_FIELD_NUMBER: _ClassVar[int]
    RETURN_EDGES_FIELD_NUMBER: _ClassVar[int]
    RETURN_VERTICES_FIELD_NUMBER: _ClassVar[int]
    source_u64: int
    target_u64: int
    weight_column: str
    return_edges: bool
    return_vertices: bool
    def __init__(self, source_u64: _Optional[int] = ..., target_u64: _Optional[int] = ..., weight_column: _Optional[str] = ..., return_edges: bool = ..., return_vertices: bool = ...) -> None: ...

class DistancesSpec(_message.Message):
    __slots__ = ("sources_u64", "targets_u64", "weight_column")
    SOURCES_U64_FIELD_NUMBER: _ClassVar[int]
    TARGETS_U64_FIELD_NUMBER: _ClassVar[int]
    WEIGHT_COLUMN_FIELD_NUMBER: _ClassVar[int]
    sources_u64: _containers.RepeatedScalarFieldContainer[int]
    targets_u64: _containers.RepeatedScalarFieldContainer[int]
    weight_column: str
    def __init__(self, sources_u64: _Optional[_Iterable[int]] = ..., targets_u64: _Optional[_Iterable[int]] = ..., weight_column: _Optional[str] = ...) -> None: ...

class BFSSpec(_message.Message):
    __slots__ = ("source_u64", "max_depth", "mode")
    SOURCE_U64_FIELD_NUMBER: _ClassVar[int]
    MAX_DEPTH_FIELD_NUMBER: _ClassVar[int]
    MODE_FIELD_NUMBER: _ClassVar[int]
    source_u64: int
    max_depth: int
    mode: NeighborhoodSpec.Mode
    def __init__(self, source_u64: _Optional[int] = ..., max_depth: _Optional[int] = ..., mode: _Optional[_Union[NeighborhoodSpec.Mode, str]] = ...) -> None: ...

class NeighborhoodQuerySpec(_message.Message):
    __slots__ = ("seeds_u64", "hops", "mode")
    SEEDS_U64_FIELD_NUMBER: _ClassVar[int]
    HOPS_FIELD_NUMBER: _ClassVar[int]
    MODE_FIELD_NUMBER: _ClassVar[int]
    seeds_u64: _containers.RepeatedScalarFieldContainer[int]
    hops: int
    mode: NeighborhoodSpec.Mode
    def __init__(self, seeds_u64: _Optional[_Iterable[int]] = ..., hops: _Optional[int] = ..., mode: _Optional[_Union[NeighborhoodSpec.Mode, str]] = ...) -> None: ...

class KShortestPathsSpec(_message.Message):
    __slots__ = ("source_u64", "target_u64", "k", "weight_column", "max_candidates", "max_corridor_edges", "per_path_timeout")
    SOURCE_U64_FIELD_NUMBER: _ClassVar[int]
    TARGET_U64_FIELD_NUMBER: _ClassVar[int]
    K_FIELD_NUMBER: _ClassVar[int]
    WEIGHT_COLUMN_FIELD_NUMBER: _ClassVar[int]
    MAX_CANDIDATES_FIELD_NUMBER: _ClassVar[int]
    MAX_CORRIDOR_EDGES_FIELD_NUMBER: _ClassVar[int]
    PER_PATH_TIMEOUT_FIELD_NUMBER: _ClassVar[int]
    source_u64: int
    target_u64: int
    k: int
    weight_column: str
    max_candidates: int
    max_corridor_edges: int
    per_path_timeout: _duration_pb2.Duration
    def __init__(self, source_u64: _Optional[int] = ..., target_u64: _Optional[int] = ..., k: _Optional[int] = ..., weight_column: _Optional[str] = ..., max_candidates: _Optional[int] = ..., max_corridor_edges: _Optional[int] = ..., per_path_timeout: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ...) -> None: ...

class STMinCutSpec(_message.Message):
    __slots__ = ("source_u64", "target_u64", "capacity_column")
    SOURCE_U64_FIELD_NUMBER: _ClassVar[int]
    TARGET_U64_FIELD_NUMBER: _ClassVar[int]
    CAPACITY_COLUMN_FIELD_NUMBER: _ClassVar[int]
    source_u64: int
    target_u64: int
    capacity_column: str
    def __init__(self, source_u64: _Optional[int] = ..., target_u64: _Optional[int] = ..., capacity_column: _Optional[str] = ...) -> None: ...

class CorridorSpec(_message.Message):
    __slots__ = ("source_u64", "target_u64", "method", "hops", "k")
    class Method(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
        __slots__ = ()
        SHORTEST_PATH_HULL: _ClassVar[CorridorSpec.Method]
        KSP_HULL: _ClassVar[CorridorSpec.Method]
        COMMUNITY_AWARE: _ClassVar[CorridorSpec.Method]
    SHORTEST_PATH_HULL: CorridorSpec.Method
    KSP_HULL: CorridorSpec.Method
    COMMUNITY_AWARE: CorridorSpec.Method
    SOURCE_U64_FIELD_NUMBER: _ClassVar[int]
    TARGET_U64_FIELD_NUMBER: _ClassVar[int]
    METHOD_FIELD_NUMBER: _ClassVar[int]
    HOPS_FIELD_NUMBER: _ClassVar[int]
    K_FIELD_NUMBER: _ClassVar[int]
    source_u64: int
    target_u64: int
    method: CorridorSpec.Method
    hops: int
    k: int
    def __init__(self, source_u64: _Optional[int] = ..., target_u64: _Optional[int] = ..., method: _Optional[_Union[CorridorSpec.Method, str]] = ..., hops: _Optional[int] = ..., k: _Optional[int] = ...) -> None: ...

class KCoreSpec(_message.Message):
    __slots__ = ("k",)
    K_FIELD_NUMBER: _ClassVar[int]
    k: int
    def __init__(self, k: _Optional[int] = ...) -> None: ...

class BetweennessSpec(_message.Message):
    __slots__ = ("sample_size", "normalized", "weight_column")
    SAMPLE_SIZE_FIELD_NUMBER: _ClassVar[int]
    NORMALIZED_FIELD_NUMBER: _ClassVar[int]
    WEIGHT_COLUMN_FIELD_NUMBER: _ClassVar[int]
    sample_size: int
    normalized: bool
    weight_column: str
    def __init__(self, sample_size: _Optional[int] = ..., normalized: bool = ..., weight_column: _Optional[str] = ...) -> None: ...

class ClosenessSpec(_message.Message):
    __slots__ = ("mode", "normalized", "weight_column")
    class Mode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
        __slots__ = ()
        ALL: _ClassVar[ClosenessSpec.Mode]
        OUT: _ClassVar[ClosenessSpec.Mode]
        IN: _ClassVar[ClosenessSpec.Mode]
    ALL: ClosenessSpec.Mode
    OUT: ClosenessSpec.Mode
    IN: ClosenessSpec.Mode
    MODE_FIELD_NUMBER: _ClassVar[int]
    NORMALIZED_FIELD_NUMBER: _ClassVar[int]
    WEIGHT_COLUMN_FIELD_NUMBER: _ClassVar[int]
    mode: ClosenessSpec.Mode
    normalized: bool
    weight_column: str
    def __init__(self, mode: _Optional[_Union[ClosenessSpec.Mode, str]] = ..., normalized: bool = ..., weight_column: _Optional[str] = ...) -> None: ...

class PageRankSpec(_message.Message):
    __slots__ = ("damping", "max_iterations", "epsilon", "weight_column")
    DAMPING_FIELD_NUMBER: _ClassVar[int]
    MAX_ITERATIONS_FIELD_NUMBER: _ClassVar[int]
    EPSILON_FIELD_NUMBER: _ClassVar[int]
    WEIGHT_COLUMN_FIELD_NUMBER: _ClassVar[int]
    damping: float
    max_iterations: int
    epsilon: float
    weight_column: str
    def __init__(self, damping: _Optional[float] = ..., max_iterations: _Optional[int] = ..., epsilon: _Optional[float] = ..., weight_column: _Optional[str] = ...) -> None: ...

class GetJobRequest(_message.Message):
    __slots__ = ("job",)
    JOB_FIELD_NUMBER: _ClassVar[int]
    job: JobRef
    def __init__(self, job: _Optional[_Union[JobRef, _Mapping]] = ...) -> None: ...

class GetJobResponse(_message.Message):
    __slots__ = ("state", "started_at", "finished_at", "result", "status")
    class State(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
        __slots__ = ()
        PENDING: _ClassVar[GetJobResponse.State]
        RUNNING: _ClassVar[GetJobResponse.State]
        SUCCEEDED: _ClassVar[GetJobResponse.State]
        FAILED: _ClassVar[GetJobResponse.State]
        CANCELED: _ClassVar[GetJobResponse.State]
    PENDING: GetJobResponse.State
    RUNNING: GetJobResponse.State
    SUCCEEDED: GetJobResponse.State
    FAILED: GetJobResponse.State
    CANCELED: GetJobResponse.State
    STATE_FIELD_NUMBER: _ClassVar[int]
    STARTED_AT_FIELD_NUMBER: _ClassVar[int]
    FINISHED_AT_FIELD_NUMBER: _ClassVar[int]
    RESULT_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    state: GetJobResponse.State
    started_at: _timestamp_pb2.Timestamp
    finished_at: _timestamp_pb2.Timestamp
    result: ResultRef
    status: Status
    def __init__(self, state: _Optional[_Union[GetJobResponse.State, str]] = ..., started_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., finished_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., result: _Optional[_Union[ResultRef, _Mapping]] = ..., status: _Optional[_Union[Status, _Mapping]] = ...) -> None: ...

class CancelJobRequest(_message.Message):
    __slots__ = ("job",)
    JOB_FIELD_NUMBER: _ClassVar[int]
    job: JobRef
    def __init__(self, job: _Optional[_Union[JobRef, _Mapping]] = ...) -> None: ...

class CancelJobResponse(_message.Message):
    __slots__ = ("canceled",)
    CANCELED_FIELD_NUMBER: _ClassVar[int]
    canceled: bool
    def __init__(self, canceled: bool = ...) -> None: ...

class GetResultRequest(_message.Message):
    __slots__ = ("result",)
    RESULT_FIELD_NUMBER: _ClassVar[int]
    result: ResultRef
    def __init__(self, result: _Optional[_Union[ResultRef, _Mapping]] = ...) -> None: ...

class ResultChunk(_message.Message):
    __slots__ = ("header", "u32", "u64", "f64", "bytes", "shortest_path", "st_mincut", "corridor", "kcore", "betweenness", "components", "closeness", "pagerank", "done")
    HEADER_FIELD_NUMBER: _ClassVar[int]
    U32_FIELD_NUMBER: _ClassVar[int]
    U64_FIELD_NUMBER: _ClassVar[int]
    F64_FIELD_NUMBER: _ClassVar[int]
    BYTES_FIELD_NUMBER: _ClassVar[int]
    SHORTEST_PATH_FIELD_NUMBER: _ClassVar[int]
    ST_MINCUT_FIELD_NUMBER: _ClassVar[int]
    CORRIDOR_FIELD_NUMBER: _ClassVar[int]
    KCORE_FIELD_NUMBER: _ClassVar[int]
    BETWEENNESS_FIELD_NUMBER: _ClassVar[int]
    COMPONENTS_FIELD_NUMBER: _ClassVar[int]
    CLOSENESS_FIELD_NUMBER: _ClassVar[int]
    PAGERANK_FIELD_NUMBER: _ClassVar[int]
    DONE_FIELD_NUMBER: _ClassVar[int]
    header: ResultHeader
    u32: U32Buffer
    u64: U64Buffer
    f64: F64Buffer
    bytes: BytesBuffer
    shortest_path: ShortestPathResult
    st_mincut: STMinCutResult
    corridor: CorridorResult
    kcore: KCoreResult
    betweenness: BetweennessResult
    components: ComponentsResult
    closeness: ClosenessResult
    pagerank: PageRankResult
    done: bool
    def __init__(self, header: _Optional[_Union[ResultHeader, _Mapping]] = ..., u32: _Optional[_Union[U32Buffer, _Mapping]] = ..., u64: _Optional[_Union[U64Buffer, _Mapping]] = ..., f64: _Optional[_Union[F64Buffer, _Mapping]] = ..., bytes: _Optional[_Union[BytesBuffer, _Mapping]] = ..., shortest_path: _Optional[_Union[ShortestPathResult, _Mapping]] = ..., st_mincut: _Optional[_Union[STMinCutResult, _Mapping]] = ..., corridor: _Optional[_Union[CorridorResult, _Mapping]] = ..., kcore: _Optional[_Union[KCoreResult, _Mapping]] = ..., betweenness: _Optional[_Union[BetweennessResult, _Mapping]] = ..., components: _Optional[_Union[ComponentsResult, _Mapping]] = ..., closeness: _Optional[_Union[ClosenessResult, _Mapping]] = ..., pagerank: _Optional[_Union[PageRankResult, _Mapping]] = ..., done: bool = ...) -> None: ...

class ResultHeader(_message.Message):
    __slots__ = ("result_id", "type", "vcount", "ecount", "meta")
    class MetaEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    RESULT_ID_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    VCOUNT_FIELD_NUMBER: _ClassVar[int]
    ECOUNT_FIELD_NUMBER: _ClassVar[int]
    META_FIELD_NUMBER: _ClassVar[int]
    result_id: str
    type: str
    vcount: int
    ecount: int
    meta: _containers.ScalarMap[str, str]
    def __init__(self, result_id: _Optional[str] = ..., type: _Optional[str] = ..., vcount: _Optional[int] = ..., ecount: _Optional[int] = ..., meta: _Optional[_Mapping[str, str]] = ...) -> None: ...

class U32Buffer(_message.Message):
    __slots__ = ("name", "values")
    NAME_FIELD_NUMBER: _ClassVar[int]
    VALUES_FIELD_NUMBER: _ClassVar[int]
    name: str
    values: _containers.RepeatedScalarFieldContainer[int]
    def __init__(self, name: _Optional[str] = ..., values: _Optional[_Iterable[int]] = ...) -> None: ...

class U64Buffer(_message.Message):
    __slots__ = ("name", "values")
    NAME_FIELD_NUMBER: _ClassVar[int]
    VALUES_FIELD_NUMBER: _ClassVar[int]
    name: str
    values: _containers.RepeatedScalarFieldContainer[int]
    def __init__(self, name: _Optional[str] = ..., values: _Optional[_Iterable[int]] = ...) -> None: ...

class F64Buffer(_message.Message):
    __slots__ = ("name", "values")
    NAME_FIELD_NUMBER: _ClassVar[int]
    VALUES_FIELD_NUMBER: _ClassVar[int]
    name: str
    values: _containers.RepeatedScalarFieldContainer[float]
    def __init__(self, name: _Optional[str] = ..., values: _Optional[_Iterable[float]] = ...) -> None: ...

class BytesBuffer(_message.Message):
    __slots__ = ("name", "data")
    NAME_FIELD_NUMBER: _ClassVar[int]
    DATA_FIELD_NUMBER: _ClassVar[int]
    name: str
    data: bytes
    def __init__(self, name: _Optional[str] = ..., data: _Optional[bytes] = ...) -> None: ...

class ShortestPathResult(_message.Message):
    __slots__ = ("vertices_u64", "edges_u64", "total_cost")
    VERTICES_U64_FIELD_NUMBER: _ClassVar[int]
    EDGES_U64_FIELD_NUMBER: _ClassVar[int]
    TOTAL_COST_FIELD_NUMBER: _ClassVar[int]
    vertices_u64: _containers.RepeatedScalarFieldContainer[int]
    edges_u64: _containers.RepeatedScalarFieldContainer[int]
    total_cost: float
    def __init__(self, vertices_u64: _Optional[_Iterable[int]] = ..., edges_u64: _Optional[_Iterable[int]] = ..., total_cost: _Optional[float] = ...) -> None: ...

class STMinCutResult(_message.Message):
    __slots__ = ("cut_value", "source_side_vertices_u64", "cut_edges_u64")
    CUT_VALUE_FIELD_NUMBER: _ClassVar[int]
    SOURCE_SIDE_VERTICES_U64_FIELD_NUMBER: _ClassVar[int]
    CUT_EDGES_U64_FIELD_NUMBER: _ClassVar[int]
    cut_value: float
    source_side_vertices_u64: _containers.RepeatedScalarFieldContainer[int]
    cut_edges_u64: _containers.RepeatedScalarFieldContainer[int]
    def __init__(self, cut_value: _Optional[float] = ..., source_side_vertices_u64: _Optional[_Iterable[int]] = ..., cut_edges_u64: _Optional[_Iterable[int]] = ...) -> None: ...

class CorridorResult(_message.Message):
    __slots__ = ("view", "meta")
    class MetaEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    VIEW_FIELD_NUMBER: _ClassVar[int]
    META_FIELD_NUMBER: _ClassVar[int]
    view: ViewRef
    meta: _containers.ScalarMap[str, str]
    def __init__(self, view: _Optional[_Union[ViewRef, _Mapping]] = ..., meta: _Optional[_Mapping[str, str]] = ...) -> None: ...

class KCoreResult(_message.Message):
    __slots__ = ("node_ids_u64", "coreness", "max_core")
    NODE_IDS_U64_FIELD_NUMBER: _ClassVar[int]
    CORENESS_FIELD_NUMBER: _ClassVar[int]
    MAX_CORE_FIELD_NUMBER: _ClassVar[int]
    node_ids_u64: _containers.RepeatedScalarFieldContainer[int]
    coreness: _containers.RepeatedScalarFieldContainer[int]
    max_core: int
    def __init__(self, node_ids_u64: _Optional[_Iterable[int]] = ..., coreness: _Optional[_Iterable[int]] = ..., max_core: _Optional[int] = ...) -> None: ...

class BetweennessResult(_message.Message):
    __slots__ = ("node_ids_u64", "scores")
    NODE_IDS_U64_FIELD_NUMBER: _ClassVar[int]
    SCORES_FIELD_NUMBER: _ClassVar[int]
    node_ids_u64: _containers.RepeatedScalarFieldContainer[int]
    scores: _containers.RepeatedScalarFieldContainer[float]
    def __init__(self, node_ids_u64: _Optional[_Iterable[int]] = ..., scores: _Optional[_Iterable[float]] = ...) -> None: ...

class ClosenessResult(_message.Message):
    __slots__ = ("node_ids_u64", "scores")
    NODE_IDS_U64_FIELD_NUMBER: _ClassVar[int]
    SCORES_FIELD_NUMBER: _ClassVar[int]
    node_ids_u64: _containers.RepeatedScalarFieldContainer[int]
    scores: _containers.RepeatedScalarFieldContainer[float]
    def __init__(self, node_ids_u64: _Optional[_Iterable[int]] = ..., scores: _Optional[_Iterable[float]] = ...) -> None: ...

class PageRankResult(_message.Message):
    __slots__ = ("node_ids_u64", "scores", "iterations", "converged")
    NODE_IDS_U64_FIELD_NUMBER: _ClassVar[int]
    SCORES_FIELD_NUMBER: _ClassVar[int]
    ITERATIONS_FIELD_NUMBER: _ClassVar[int]
    CONVERGED_FIELD_NUMBER: _ClassVar[int]
    node_ids_u64: _containers.RepeatedScalarFieldContainer[int]
    scores: _containers.RepeatedScalarFieldContainer[float]
    iterations: int
    converged: bool
    def __init__(self, node_ids_u64: _Optional[_Iterable[int]] = ..., scores: _Optional[_Iterable[float]] = ..., iterations: _Optional[int] = ..., converged: bool = ...) -> None: ...

class ComponentsResult(_message.Message):
    __slots__ = ("node_ids_u64", "membership", "num_components")
    NODE_IDS_U64_FIELD_NUMBER: _ClassVar[int]
    MEMBERSHIP_FIELD_NUMBER: _ClassVar[int]
    NUM_COMPONENTS_FIELD_NUMBER: _ClassVar[int]
    node_ids_u64: _containers.RepeatedScalarFieldContainer[int]
    membership: _containers.RepeatedScalarFieldContainer[int]
    num_components: int
    def __init__(self, node_ids_u64: _Optional[_Iterable[int]] = ..., membership: _Optional[_Iterable[int]] = ..., num_components: _Optional[int] = ...) -> None: ...

class ReleaseRequest(_message.Message):
    __slots__ = ("view", "result")
    VIEW_FIELD_NUMBER: _ClassVar[int]
    RESULT_FIELD_NUMBER: _ClassVar[int]
    view: ViewRef
    result: ResultRef
    def __init__(self, view: _Optional[_Union[ViewRef, _Mapping]] = ..., result: _Optional[_Union[ResultRef, _Mapping]] = ...) -> None: ...

class ReleaseResponse(_message.Message):
    __slots__ = ("released",)
    RELEASED_FIELD_NUMBER: _ClassVar[int]
    released: bool
    def __init__(self, released: bool = ...) -> None: ...
