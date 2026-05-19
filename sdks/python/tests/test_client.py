"""Behavioural tests for the iogrid Python SDK.

We use ``respx`` to intercept the underlying ``httpx`` calls so the
tests cover the full wire path (URL construction, headers, body
serialisation, error envelopes, SSE parsing) without binding to a
network.
"""

from __future__ import annotations

import json

import httpx
import pytest
import respx

from iogrid import IogridClient, IogridError


@pytest.fixture
async def client() -> IogridClient:
    return IogridClient(api_key="iog_test")


@respx.mock
async def test_create_workload_posts_json() -> None:
    route = respx.post("https://api.iogrid.org/v1/workloads").mock(
        return_value=httpx.Response(
            201,
            json={
                "id": "w1",
                "workspaceId": "ws1",
                "type": "BANDWIDTH",
                "status": "queued",
            },
        )
    )
    async with IogridClient(api_key="iog_test") as c:
        w = await c.create_workload(
            {"type": "BANDWIDTH", "bandwidth": {"targetUrl": "https://example.com"}}
        )
    assert w["id"] == "w1"
    sent = route.calls.last.request
    assert sent.headers["Authorization"] == "Bearer iog_test"
    assert json.loads(sent.content) == {
        "type": "BANDWIDTH",
        "bandwidth": {"targetUrl": "https://example.com"},
    }


@respx.mock
async def test_get_workload_url() -> None:
    respx.get("https://api.iogrid.org/v1/workloads/abc").mock(
        return_value=httpx.Response(
            200,
            json={"workload": {"id": "abc", "workspaceId": "ws", "type": "DOCKER", "status": "queued"}},
        )
    )
    async with IogridClient(api_key="iog_test") as c:
        r = await c.get_workload("abc")
    assert r["workload"]["id"] == "abc"


@respx.mock
async def test_list_workloads_drops_empty_params() -> None:
    route = respx.get("https://api.iogrid.org/v1/workloads").mock(
        return_value=httpx.Response(200, json={"workloads": []})
    )
    async with IogridClient(api_key="iog_test") as c:
        await c.list_workloads(page_size=50, type="DOCKER")
    sent = route.calls.last.request
    assert sent.url.params.get("pageSize") == "50"
    assert sent.url.params.get("type") == "DOCKER"
    assert "status" not in sent.url.params


@respx.mock
async def test_cancel_workload_with_reason() -> None:
    route = respx.delete("https://api.iogrid.org/v1/workloads/w1").mock(
        return_value=httpx.Response(
            200,
            json={"id": "w1", "workspaceId": "ws", "type": "BANDWIDTH", "status": "cancelled"},
        )
    )
    async with IogridClient(api_key="iog_test") as c:
        w = await c.cancel_workload("w1", reason="user requested")
    assert w["status"] == "cancelled"
    assert route.calls.last.request.url.params.get("reason") == "user requested"


@respx.mock
async def test_delete_api_key_returns_none_on_204() -> None:
    respx.delete("https://api.iogrid.org/v1/keys/k1").mock(return_value=httpx.Response(204))
    async with IogridClient(api_key="iog_test") as c:
        assert await c.delete_api_key("k1") is None


@respx.mock
async def test_list_api_keys_unwraps_envelope() -> None:
    respx.get("https://api.iogrid.org/v1/keys").mock(
        return_value=httpx.Response(
            200,
            json={
                "keys": [
                    {
                        "id": "k1",
                        "name": "ci",
                        "prefix": "iog_abcd",
                        "createdAt": "2026-01-01T00:00:00Z",
                    }
                ]
            },
        )
    )
    async with IogridClient(api_key="iog_test") as c:
        keys = await c.list_api_keys()
    assert len(keys) == 1
    assert keys[0]["prefix"] == "iog_abcd"


@respx.mock
async def test_4xx_raises_iogrid_error() -> None:
    respx.post("https://api.iogrid.org/v1/workloads").mock(
        return_value=httpx.Response(
            400,
            json={
                "code": "INVALID_ARGUMENT",
                "message": "bad target",
                "fieldPath": "bandwidth.targetUrl",
                "requestId": "req-123",
            },
        )
    )
    async with IogridClient(api_key="iog_test") as c:
        with pytest.raises(IogridError) as ei:
            await c.create_workload(
                {"type": "BANDWIDTH", "bandwidth": {"targetUrl": ""}}
            )
    assert ei.value.status == 400
    assert ei.value.code == "INVALID_ARGUMENT"
    assert ei.value.field_path == "bandwidth.targetUrl"
    assert ei.value.request_id == "req-123"


@respx.mock
async def test_stream_workload_events_parses_sse() -> None:
    sse_body = (
        'data: {"workloadId":"w1","newStatus":"queued","occurredAt":"2026-01-01T00:00:00Z"}\n\n'
        'data: {"workloadId":"w1","newStatus":"running","occurredAt":"2026-01-01T00:00:01Z"}\n\n'
        'data: {"workloadId":"w1","newStatus":"succeeded","occurredAt":"2026-01-01T00:00:02Z"}\n\n'
    )
    respx.get("https://api.iogrid.org/v1/workloads/w1/events").mock(
        return_value=httpx.Response(
            200,
            content=sse_body,
            headers={"Content-Type": "text/event-stream"},
        )
    )
    seen: list[str] = []
    async with IogridClient(api_key="iog_test") as c:
        async for ev in c.stream_workload_events("w1"):
            seen.append(ev["newStatus"])
    assert seen == ["queued", "running", "succeeded"]


@respx.mock
async def test_stream_workload_events_propagates_4xx() -> None:
    respx.get("https://api.iogrid.org/v1/workloads/nope/events").mock(
        return_value=httpx.Response(404, json={"code": "NOT_FOUND", "message": "x"})
    )
    async with IogridClient(api_key="iog_test") as c:
        with pytest.raises(IogridError) as ei:
            async for _ in c.stream_workload_events("nope"):
                pass
    assert ei.value.code == "NOT_FOUND"


def test_constructor_requires_api_key() -> None:
    with pytest.raises(ValueError, match="api_key is required"):
        IogridClient(api_key="")


@respx.mock
async def test_custom_base_url() -> None:
    route = respx.get("https://api.staging.iogrid.org/v1/workloads").mock(
        return_value=httpx.Response(200, json={"workloads": []})
    )
    async with IogridClient(
        api_key="iog_test", base_url="https://api.staging.iogrid.org/"
    ) as c:
        await c.list_workloads()
    assert route.called


@respx.mock
async def test_user_agent_header() -> None:
    route = respx.get("https://api.iogrid.org/v1/workloads").mock(
        return_value=httpx.Response(200, json={"workloads": []})
    )
    async with IogridClient(api_key="iog_test", user_agent="my-app/1.0") as c:
        await c.list_workloads()
    ua = route.calls.last.request.headers["User-Agent"]
    assert "iogrid-sdk-python/" in ua
    assert "my-app/1.0" in ua
