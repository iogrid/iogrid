"""Error model for the iogrid SDK."""

from __future__ import annotations

from typing import Any

from .types import ErrorEnvelope


class IogridError(Exception):
    """Raised for non-2xx HTTP responses returned by the iogrid API.

    The ``code`` attribute is the stable machine-readable error code
    (mirrors ``iogrid.common.v1.ErrorCode``); callers should ``switch``
    on this rather than parsing the human-readable message.
    """

    def __init__(self, status: int, envelope: ErrorEnvelope) -> None:
        msg = envelope.get("message") or f"iogrid: HTTP {status}"
        super().__init__(msg)
        self.status = status
        self.code: str = envelope.get("code", "INTERNAL")
        self.field_path: str | None = envelope.get("fieldPath")
        self.metadata: dict[str, str] = dict(envelope.get("metadata", {}))
        self.request_id: str | None = envelope.get("requestId")

    def __repr__(self) -> str:
        return (
            f"IogridError(status={self.status}, code={self.code!r}, "
            f"message={self.args[0]!r}, request_id={self.request_id!r})"
        )


def retry_after_seconds(err: Any) -> int | None:
    """Return server-suggested retry delay (seconds), or ``None`` if absent.

    Reads ``metadata.retry_after_seconds`` from an :class:`IogridError`.
    """

    if not isinstance(err, IogridError):
        return None
    raw = err.metadata.get("retry_after_seconds") or err.metadata.get("retryAfterSeconds")
    if not raw:
        return None
    try:
        return int(raw)
    except (TypeError, ValueError):
        return None


__all__ = ["IogridError", "retry_after_seconds"]
