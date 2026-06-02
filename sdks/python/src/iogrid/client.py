"""Async iogrid customer SDK client built on httpx.

Synchronous callers can use ``asyncio.run(...)`` or the convenience
:py:meth:`IogridClient.sync` factory described in the README; the
core wire path is async-first because the workload-event stream is the
hot path and benefits most from non-blocking I/O.
"""

from __future__ import annotations

import json as _json
from collections.abc import AsyncIterator, Mapping
from types import TracebackType
from typing import Any, cast

import httpx
from typing_extensions import Self

from .errors import IogridError
from .types import (
    ApiKeyMetadata,
    CreateApiKeyRequest,
    CreatedApiKey,
    CreateWorkloadRequest,
    ErrorEnvelope,
    GetWorkloadResponse,
    Invoice,
    ListApiKeysResponse,
    ListInvoicesResponse,
    ListUsageResponse,
    ListWorkloadsResponse,
    RequestMobileSessionRequest,
    RequestMobileSessionResponse,
    UsageRecord,
    Workload,
    WorkloadEvent,
    WorkloadType,
)

__version__ = "0.1.0"
DEFAULT_BASE_URL = "https://api.iogrid.org"
DEFAULT_TIMEOUT = 30.0


class IogridClient:
    """High-level async client for the iogrid customer API.

    Use as an async context manager:

    .. code-block:: python

        async with IogridClient(api_key="iog_…") as iogrid:
            w = await iogrid.create_workload(
                type="BANDWIDTH",
                bandwidth={"targetUrl": "https://example.com"},
            )
    """

    def __init__(
        self,
        *,
        api_key: str,
        base_url: str = DEFAULT_BASE_URL,
        timeout: float | None = DEFAULT_TIMEOUT,
        user_agent: str | None = None,
        transport: httpx.AsyncBaseTransport | None = None,
    ) -> None:
        if not api_key:
            raise ValueError("IogridClient: api_key is required")
        ua = f"iogrid-sdk-python/{__version__}"
        if user_agent:
            ua = f"{ua} ({user_agent})"
        headers = {
            "Authorization": f"Bearer {api_key}",
            "Accept": "application/json",
            "User-Agent": ua,
        }
        client_kwargs: dict[str, Any] = {
            "base_url": base_url.rstrip("/"),
            "headers": headers,
            "timeout": timeout,
        }
        if transport is not None:
            client_kwargs["transport"] = transport
        self._client = httpx.AsyncClient(**client_kwargs)

    # --- lifecycle ---------------------------------------------------------

    async def __aenter__(self) -> Self:
        return self

    async def __aexit__(
        self,
        exc_type: type[BaseException] | None,
        exc: BaseException | None,
        tb: TracebackType | None,
    ) -> None:
        await self.aclose()

    async def aclose(self) -> None:
        await self._client.aclose()

    # --- transport plumbing ------------------------------------------------

    async def _json_request(
        self,
        method: str,
        path: str,
        *,
        json: Any | None = None,
        params: Mapping[str, Any] | None = None,
    ) -> Any:
        cleaned = (
            {k: v for k, v in params.items() if v is not None and v != ""}
            if params
            else None
        )
        resp = await self._client.request(method, path, json=json, params=cleaned)
        if resp.status_code == 204:
            return None
        if not resp.is_success:
            envelope: ErrorEnvelope
            try:
                envelope = cast(ErrorEnvelope, resp.json())
            except (ValueError, _json.JSONDecodeError):
                envelope = cast(
                    ErrorEnvelope,
                    {"code": "INTERNAL", "message": f"HTTP {resp.status_code}"},
                )
            raise IogridError(resp.status_code, envelope)
        return resp.json()

    async def _stream_sse(
        self,
        path: str,
        *,
        params: Mapping[str, Any] | None = None,
    ) -> AsyncIterator[dict[str, Any]]:
        cleaned = (
            {k: v for k, v in params.items() if v is not None and v != ""}
            if params
            else None
        )
        req_headers = {"Accept": "text/event-stream"}
        async with self._client.stream(
            "GET", path, headers=req_headers, params=cleaned
        ) as resp:
            if not resp.is_success:
                try:
                    body = await resp.aread()
                    envelope = cast(ErrorEnvelope, _json.loads(body))
                except (ValueError, _json.JSONDecodeError):
                    envelope = cast(
                        ErrorEnvelope,
                        {"code": "INTERNAL", "message": f"HTTP {resp.status_code}"},
                    )
                raise IogridError(resp.status_code, envelope)

            buffer = ""
            async for chunk in resp.aiter_text():
                buffer += chunk
                while "\n\n" in buffer:
                    raw, buffer = buffer.split("\n\n", 1)
                    data = _parse_sse_event(raw)
                    if data is not None:
                        yield cast(dict[str, Any], _json.loads(data))

    # --- Workloads ---------------------------------------------------------

    async def create_workload(self, body: CreateWorkloadRequest) -> Workload:
        """Submit a new workload."""
        return cast(Workload, await self._json_request("POST", "/v1/workloads", json=body))

    async def get_workload(self, workload_id: str) -> GetWorkloadResponse:
        """Retrieve a workload (includes terminal result if finished)."""
        return cast(
            GetWorkloadResponse,
            await self._json_request("GET", f"/v1/workloads/{workload_id}"),
        )

    async def list_workloads(
        self,
        *,
        page_size: int | None = None,
        page_token: str | None = None,
        type: WorkloadType | None = None,
        status: str | None = None,
        submitted_after: str | None = None,
        submitted_before: str | None = None,
    ) -> ListWorkloadsResponse:
        """List workloads in the caller's workspace."""
        return cast(
            ListWorkloadsResponse,
            await self._json_request(
                "GET",
                "/v1/workloads",
                params={
                    "pageSize": page_size,
                    "pageToken": page_token,
                    "type": type,
                    "status": status,
                    "submittedAfter": submitted_after,
                    "submittedBefore": submitted_before,
                },
            ),
        )

    async def cancel_workload(
        self, workload_id: str, *, reason: str | None = None
    ) -> Workload:
        """Cancel a queued or running workload."""
        return cast(
            Workload,
            await self._json_request(
                "DELETE", f"/v1/workloads/{workload_id}", params={"reason": reason}
            ),
        )

    async def stream_workload_events(
        self, workload_id: str
    ) -> AsyncIterator[WorkloadEvent]:
        """Async iterator over SSE-streamed workload state transitions.

        Iteration completes when the server closes the stream (typically
        when the workload reaches a terminal status).
        """
        async for raw in self._stream_sse(f"/v1/workloads/{workload_id}/events"):
            yield cast(WorkloadEvent, raw)

    # --- API keys ----------------------------------------------------------

    async def create_api_key(self, body: CreateApiKeyRequest) -> CreatedApiKey:
        """Mint a new API key. The secret is returned only at creation time."""
        return cast(CreatedApiKey, await self._json_request("POST", "/v1/keys", json=body))

    async def list_api_keys(self) -> list[ApiKeyMetadata]:
        """List API keys for the caller's workspace (metadata only)."""
        r = cast(
            ListApiKeysResponse, await self._json_request("GET", "/v1/keys")
        )
        return list(r.get("keys", []))

    async def delete_api_key(self, key_id: str) -> None:
        """Revoke an API key."""
        await self._json_request("DELETE", f"/v1/keys/{key_id}")

    # --- Billing -----------------------------------------------------------

    async def get_usage(
        self,
        *,
        page_size: int | None = None,
        page_token: str | None = None,
        type: WorkloadType | None = None,
        window_start: str | None = None,
        window_end: str | None = None,
    ) -> list[UsageRecord]:
        """Paged list of metered usage records (returns one page)."""
        r = cast(
            ListUsageResponse,
            await self._json_request(
                "GET",
                "/v1/usage",
                params={
                    "pageSize": page_size,
                    "pageToken": page_token,
                    "type": type,
                    "windowStart": window_start,
                    "windowEnd": window_end,
                },
            ),
        )
        return list(r.get("usage", []))

    async def get_invoices(
        self,
        *,
        page_size: int | None = None,
        page_token: str | None = None,
    ) -> list[Invoice]:
        """Paged list of invoices issued against the caller's workspace."""
        r = cast(
            ListInvoicesResponse,
            await self._json_request(
                "GET",
                "/v1/invoices",
                params={"pageSize": page_size, "pageToken": page_token},
            ),
        )
        return list(r.get("invoices", []))

    # --- Mobile VPN session bring-up --------------------------------------

    async def request_mobile_session(
        self,
        body: RequestMobileSessionRequest,
    ) -> RequestMobileSessionResponse:
        """Request a one-shot mobile-app VPN session.

        POSTs to ``/v1/vpn/sessions/mobile`` and returns the full
        WireGuard peer config so the mobile PacketTunnelProvider can
        bring up the tunnel without a second round-trip.

        Distinct from the legacy daemon-driven flow at
        ``/v1/vpn/sessions``. On 503 the SDK raises
        :class:`IogridError`; the ``Retry-After`` header is preserved
        on the underlying response — for now callers should retry with
        backoff (15s server default).

        Required keys in ``body``: ``customer_id`` (UUID),
        ``client_public_key`` (base64 WG key). Optional: ``region``
        (default "auto"), ``api_key``, ``payment_authorization``.
        """
        if not body.get("customer_id"):
            raise ValueError(
                "request_mobile_session: customer_id is required"
            )
        if not body.get("client_public_key"):
            raise ValueError(
                "request_mobile_session: client_public_key is required"
            )
        return cast(
            RequestMobileSessionResponse,
            await self._json_request(
                "POST", "/v1/vpn/sessions/mobile", json=body
            ),
        )


def _parse_sse_event(raw: str) -> str | None:
    """Extract concatenated ``data:`` payload from a single SSE event."""
    parts: list[str] = []
    for line in raw.split("\n"):
        if line.startswith(":"):
            continue
        if line.startswith("data:"):
            parts.append(line[5:].lstrip())
    if not parts:
        return None
    return "\n".join(parts)


__all__ = ["IogridClient"]
