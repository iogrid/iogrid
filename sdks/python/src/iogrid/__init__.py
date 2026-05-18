"""Official Python SDK for the iogrid customer API.

Example::

    import asyncio
    from iogrid import IogridClient

    async def main() -> None:
        async with IogridClient(api_key="iog_...") as iogrid:
            w = await iogrid.create_workload(
                type="BANDWIDTH",
                bandwidth={"target_url": "https://example.com"},
            )
            print(w["id"], w["status"])

    asyncio.run(main())
"""

from __future__ import annotations

from .client import IogridClient
from .errors import IogridError, retry_after_seconds
from .types import (
    ApiKeyMetadata,
    BandwidthRequest,
    CreateApiKeyRequest,
    CreatedApiKey,
    CreateWorkloadRequest,
    DockerRequest,
    ErrorEnvelope,
    GetWorkloadResponse,
    GpuRequest,
    Invoice,
    IosBuildRequest,
    ListApiKeysResponse,
    ListInvoicesResponse,
    ListUsageResponse,
    ListWorkloadsResponse,
    Money,
    UsageRecord,
    Workload,
    WorkloadEvent,
    WorkloadResult,
)

__version__ = "0.1.0"

__all__ = [
    "ApiKeyMetadata",
    "BandwidthRequest",
    "CreateApiKeyRequest",
    "CreateWorkloadRequest",
    "CreatedApiKey",
    "DockerRequest",
    "ErrorEnvelope",
    "GetWorkloadResponse",
    "GpuRequest",
    "IogridClient",
    "IogridError",
    "Invoice",
    "IosBuildRequest",
    "ListApiKeysResponse",
    "ListInvoicesResponse",
    "ListUsageResponse",
    "ListWorkloadsResponse",
    "Money",
    "UsageRecord",
    "Workload",
    "WorkloadEvent",
    "WorkloadResult",
    "__version__",
    "retry_after_seconds",
]
