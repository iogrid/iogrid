# iogrid

Official Python SDK for the [iogrid](https://iogrid.org) customer API.

```bash
pip install iogrid
# or
poetry add iogrid
# or
uv add iogrid
```

Async-first, built on [httpx](https://www.python-httpx.org/). Python 3.10+.

## Quick start

```python
import asyncio
from iogrid import IogridClient

async def main() -> None:
    async with IogridClient(api_key="iog_…") as iogrid:
        w = await iogrid.create_workload({
            "type": "BANDWIDTH",
            "bandwidth": {"targetUrl": "https://example.com/page"},
        })
        print(w["id"], w["status"])

asyncio.run(main())
```

## Examples

### 1. Submit a Docker workload

```python
w = await iogrid.create_workload({
    "type": "DOCKER",
    "docker": {
        "image": "ghcr.io/example/scraper@sha256:abc...",
        "command": ["./run.sh"],
        "env": {"CONCURRENCY": "4"},
        "timeoutSeconds": 900,
        "minCpuCores": 2,
        "minMemoryMib": 1024,
    },
})
```

### 2. Stream a workload to completion

```python
w = await iogrid.create_workload({
    "type": "DOCKER",
    "docker": {"image": "ghcr.io/example/job:latest"},
})

async for ev in iogrid.stream_workload_events(w["id"]):
    print(ev["occurredAt"], ev["newStatus"], ev.get("note", ""))

resp = await iogrid.get_workload(w["id"])
print("done:", resp.get("result"))
```

### 3. Mint and rotate API keys

```python
created = await iogrid.create_api_key({"name": "ci-pipeline-2026"})
secret = created["secret"]  # only returned at creation; store securely.

for k in await iogrid.list_api_keys():
    print(k["id"], k["name"], k["prefix"])

await iogrid.delete_api_key("00000000-0000-0000-0000-000000000000")
```

### 4. Pull usage and invoices

```python
usage = await iogrid.get_usage(
    window_start="2026-01-01T00:00:00Z",
    window_end="2026-02-01T00:00:00Z",
    type="BANDWIDTH",
    page_size=200,
)
total_bytes = sum(r["quantity"] for r in usage)
print(f"bandwidth in January: {total_bytes / 1e9:.2f} GB")

for inv in await iogrid.get_invoices():
    print(inv["id"], inv["status"], inv["total"])
```

### 5. Error handling

```python
from iogrid import IogridError, retry_after_seconds

try:
    await iogrid.create_workload({"type": "BANDWIDTH", "bandwidth": {"targetUrl": ""}})
except IogridError as err:
    if err.code == "INVALID_ARGUMENT":
        print("field:", err.field_path)
    elif err.code == "ABUSE_RATE_LIMITED":
        delay = retry_after_seconds(err) or 5
        print(f"rate-limited; retry in {delay}s")
    else:
        print("iogrid error", err.code, err.args[0], "reqId:", err.request_id)
```

## Configuration

```python
IogridClient(
    api_key="iog_…",                            # required
    base_url="https://api.iogrid.org",          # default
    timeout=30.0,                                # per-request seconds, None = no timeout
    user_agent="my-app/1.0",                    # appended to the SDK UA
)
```

## License

Apache-2.0
