# SPDX-License-Identifier: MIT
# Copyright (c) 2025 Naisa AI, Inc.

"""Custom exceptions for the Graph-engine client."""

from typing import Optional

import grpc


class GraphEngineError(Exception):
    """Base exception for Graph-engine errors."""

    def __init__(
        self,
        message: str,
        code: Optional[grpc.StatusCode] = None,
        cause: Optional[Exception] = None,
    ):
        super().__init__(message)
        self.message = message
        self.code = code
        self.cause = cause

    def __str__(self) -> str:
        if self.code:
            return f"{self.code.name}: {self.message}"
        return self.message


class NotFoundError(GraphEngineError):
    """Resource not found."""

    def __init__(self, message: str = "Resource not found", cause: Optional[Exception] = None):
        super().__init__(message, grpc.StatusCode.NOT_FOUND, cause)


class TimeoutError(GraphEngineError):
    """Operation timed out."""

    def __init__(self, message: str = "Operation timed out", cause: Optional[Exception] = None):
        super().__init__(message, grpc.StatusCode.DEADLINE_EXCEEDED, cause)


class CanceledError(GraphEngineError):
    """Operation was canceled."""

    def __init__(self, message: str = "Operation canceled", cause: Optional[Exception] = None):
        super().__init__(message, grpc.StatusCode.CANCELLED, cause)


class InvalidArgumentError(GraphEngineError):
    """Invalid argument provided."""

    def __init__(self, message: str = "Invalid argument", cause: Optional[Exception] = None):
        super().__init__(message, grpc.StatusCode.INVALID_ARGUMENT, cause)


class ResourceExhaustedError(GraphEngineError):
    """Resource exhausted (quota exceeded)."""

    def __init__(self, message: str = "Resource exhausted", cause: Optional[Exception] = None):
        super().__init__(message, grpc.StatusCode.RESOURCE_EXHAUSTED, cause)


class InternalError(GraphEngineError):
    """Internal server error."""

    def __init__(self, message: str = "Internal error", cause: Optional[Exception] = None):
        super().__init__(message, grpc.StatusCode.INTERNAL, cause)


class JobFailedError(GraphEngineError):
    """Job execution failed."""

    def __init__(self, message: str = "Job failed", cause: Optional[Exception] = None):
        super().__init__(message, None, cause)


def wrap_grpc_error(err: grpc.RpcError, operation: str = "") -> GraphEngineError:
    """Wrap a gRPC error with a more specific exception type."""
    code = err.code()
    details = err.details() or str(err)
    message = f"{operation}: {details}" if operation else details

    if code == grpc.StatusCode.NOT_FOUND:
        return NotFoundError(message, err)
    elif code == grpc.StatusCode.DEADLINE_EXCEEDED:
        return TimeoutError(message, err)
    elif code == grpc.StatusCode.CANCELLED:
        return CanceledError(message, err)
    elif code == grpc.StatusCode.INVALID_ARGUMENT:
        return InvalidArgumentError(message, err)
    elif code == grpc.StatusCode.RESOURCE_EXHAUSTED:
        return ResourceExhaustedError(message, err)
    elif code == grpc.StatusCode.INTERNAL:
        return InternalError(message, err)
    else:
        return GraphEngineError(message, code, err)


def is_retryable(err: Exception) -> bool:
    """Check if an error is retryable."""
    if isinstance(err, GraphEngineError):
        return err.code in (
            grpc.StatusCode.UNAVAILABLE,
            grpc.StatusCode.RESOURCE_EXHAUSTED,
            grpc.StatusCode.ABORTED,
            grpc.StatusCode.DEADLINE_EXCEEDED,
        )
    if isinstance(err, grpc.RpcError):
        return err.code() in (
            grpc.StatusCode.UNAVAILABLE,
            grpc.StatusCode.RESOURCE_EXHAUSTED,
            grpc.StatusCode.ABORTED,
            grpc.StatusCode.DEADLINE_EXCEEDED,
        )
    return False
