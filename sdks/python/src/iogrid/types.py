"""Typed wire models for the iogrid customer API.

We use ``TypedDict`` rather than dataclasses so the SDK can return raw
JSON-decoded dicts (the natural shape for ``httpx.Response.json()`` and
the most ergonomic for callers) while still giving editors + ``mypy``
strict field-name + type checking.

Field names mirror the JSON wire format (``camelCase`` — matches the
OpenAPI spec at ``proto/gen/openapi/iogrid.yaml`` and protobuf-JSON
convention). Method-level kwargs in ``IogridClient`` use ``snake_case``
for Pythonic ergonomics; the client converts to camelCase on the wire.
"""

from __future__ import annotations

from typing import Literal

from typing_extensions import NotRequired, TypedDict

WorkloadType = Literal["BANDWIDTH", "DOCKER", "GPU", "IOS_BUILD"]
WorkloadPriority = Literal["LOW", "NORMAL", "HIGH"]
ErrorCode = Literal[
    "INVALID_ARGUMENT",
    "NOT_FOUND",
    "ALREADY_EXISTS",
    "PERMISSION_DENIED",
    "UNAUTHENTICATED",
    "RESOURCE_EXHAUSTED",
    "FAILED_PRECONDITION",
    "INTERNAL",
    "UNAVAILABLE",
    "DEADLINE_EXCEEDED",
    "ABUSE_BLOCKED",
    "ABUSE_RATE_LIMITED",
    "ABUSE_CATEGORY_DISALLOWED",
    "ABUSE_DESTINATION_BLOCKED",
    "STEP_UP_REQUIRED",
    "BILLING_PAST_DUE",
]
InvoiceStatus = Literal["draft", "open", "paid", "void", "uncollectible"]


class Money(TypedDict):
    """Fixed-precision monetary amount.

    Example: ``{"currency": "USD", "micros": 12_340_000}`` for $12.34.
    """

    currency: str
    micros: int


class BandwidthRequest(TypedDict, total=False):
    targetUrl: str
    method: str
    sessionId: str
    preferredRegion: str
    category: str
    maxSpend: Money


class DockerRequest(TypedDict, total=False):
    image: str
    command: list[str]
    env: dict[str, str]
    timeoutSeconds: int
    minCpuCores: int
    minMemoryMib: int
    minGpuMemoryMib: int


class GpuRequest(TypedDict, total=False):
    image: str
    command: list[str]
    env: dict[str, str]
    timeoutSeconds: int
    minVramMib: int
    allowedVendors: list[str]


class IosBuildRequest(TypedDict, total=False):
    sourceTarballS3Key: str
    tartImage: str
    buildCommands: list[str]
    artifactS3Bucket: str
    artifactS3Prefix: str


class CreateWorkloadRequest(TypedDict, total=False):
    type: WorkloadType
    priority: WorkloadPriority
    labels: dict[str, str]
    bandwidth: BandwidthRequest
    docker: DockerRequest
    gpu: GpuRequest
    iosBuild: IosBuildRequest


class Workload(TypedDict, total=False):
    id: str
    workspaceId: str
    submittedByUserId: str
    type: WorkloadType
    priority: WorkloadPriority
    status: str
    submittedAt: str
    startedAt: str
    finishedAt: str
    labels: dict[str, str]
    bandwidth: BandwidthRequest
    docker: DockerRequest
    gpu: GpuRequest
    iosBuild: IosBuildRequest


class WorkloadResult(TypedDict, total=False):
    workloadId: str
    terminalStatus: str
    exitCode: int
    logsS3Key: str
    bytesIn: int
    bytesOut: int
    artifactS3Keys: list[str]
    cost: Money
    completedAt: str


class GetWorkloadResponse(TypedDict):
    workload: Workload
    result: NotRequired[WorkloadResult]


class WorkloadEvent(TypedDict, total=False):
    workloadId: str
    newStatus: str
    occurredAt: str
    note: str


class ListWorkloadsResponse(TypedDict, total=False):
    workloads: list[Workload]
    nextPageToken: str


class CreateApiKeyRequest(TypedDict, total=False):
    name: str
    expiresAt: str
    scopes: list[str]


class ApiKeyMetadata(TypedDict, total=False):
    id: str
    name: str
    prefix: str
    createdAt: str
    lastUsedAt: str
    expiresAt: str
    scopes: list[str]


class CreatedApiKey(ApiKeyMetadata, total=False):
    secret: str  # only returned at creation time


class ListApiKeysResponse(TypedDict, total=False):
    keys: list[ApiKeyMetadata]
    nextPageToken: str


class UsageRecord(TypedDict, total=False):
    id: str
    workloadId: str
    type: WorkloadType
    quantity: int
    cost: Money
    recordedAt: str


class ListUsageResponse(TypedDict, total=False):
    usage: list[UsageRecord]
    nextPageToken: str
    pageSubtotal: Money


class Invoice(TypedDict, total=False):
    id: str
    periodStart: str
    periodEnd: str
    subtotal: Money
    tax: Money
    total: Money
    status: InvoiceStatus
    issuedAt: str
    paidAt: str
    hostedInvoiceUrl: str


class ListInvoicesResponse(TypedDict, total=False):
    invoices: list[Invoice]
    nextPageToken: str


class ErrorEnvelope(TypedDict, total=False):
    code: ErrorCode
    message: str
    fieldPath: str
    metadata: dict[str, str]
    requestId: str


__all__ = [
    "ApiKeyMetadata",
    "BandwidthRequest",
    "CreateApiKeyRequest",
    "CreateWorkloadRequest",
    "CreatedApiKey",
    "DockerRequest",
    "ErrorCode",
    "ErrorEnvelope",
    "GetWorkloadResponse",
    "GpuRequest",
    "Invoice",
    "InvoiceStatus",
    "IosBuildRequest",
    "ListApiKeysResponse",
    "ListInvoicesResponse",
    "ListUsageResponse",
    "ListWorkloadsResponse",
    "Money",
    "UsageRecord",
    "Workload",
    "WorkloadEvent",
    "WorkloadPriority",
    "WorkloadResult",
    "WorkloadType",
]
