"""Submit a workload and tail its events to stdout.

Run:
    python -m examples.python_stream_events

Requires ``IOGRID_API_KEY`` in the environment and the ``iogrid``
package installed (``pip install iogrid``).
"""

from __future__ import annotations

import asyncio
import os

from iogrid import IogridClient, IogridError


async def main() -> None:
    api_key = os.environ.get("IOGRID_API_KEY")
    if not api_key:
        raise SystemExit("set IOGRID_API_KEY first")

    async with IogridClient(api_key=api_key) as iogrid:
        w = await iogrid.create_workload(
            {
                "type": "DOCKER",
                "docker": {
                    "image": "ghcr.io/example/job:latest",
                    "command": ["./entrypoint.sh"],
                    "timeoutSeconds": 600,
                    "minCpuCores": 2,
                    "minMemoryMib": 1024,
                },
                "labels": {"example": "stream-events-py"},
            }
        )
        print("submitted", w["id"])

        try:
            async for ev in iogrid.stream_workload_events(w["id"]):
                print(ev.get("occurredAt"), ev.get("newStatus"), ev.get("note", ""))
        except IogridError as err:
            print("stream failed:", err.code, err.args[0])
            return

        final = await iogrid.get_workload(w["id"])
        result = final.get("result")
        if result:
            print(
                "terminal:",
                result.get("terminalStatus"),
                "cost:",
                result.get("cost"),
            )


if __name__ == "__main__":
    asyncio.run(main())
