-- +goose Up
-- +goose StatementBegin
-- #771: persist workloads-svc workloads + dispatch assignments in Postgres so
-- ANY replica can resolve GetWorkload / GetAssignment.
--
-- ROOT CAUSE this fixes: assignments lived ONLY in process memory
-- (store.NewInMemory). The deployment runs multiple replicas (HPA
-- maxReplicas:10). Poll-dispatch (#705/#714) is therefore split-brain:
--   1. Submit → TryAssign creates the assignment in replica A's memStore.
--   2. Daemon GET /assigned-workloads → LB → replica A → runs the build (~5.5m).
--   3. Daemon POST .../{attempt}/status (succeeded) → LB → replica B →
--      GetAssignment(attempt) not in B's memStore → 404 "assignment not found".
-- The build-gateway ForwardStatus call is gated AFTER that GetAssignment
-- lookup, so on the 404 it never runs → the customer-facing build stays
-- "running" forever → no metering / $GRID settle (#740 class). ping's real
-- 5.5-min build (#770) produced Ping.app but never transitioned.
--
-- Persisting both tables lets the terminal POST land on ANY replica: it reads
-- the assignment + workload from Postgres, forwards to the gateway, and the
-- build settles regardless of which replica the LB chose.
--
-- Schema mirrors the in-memory store's Workload + Assignment projections
-- (internal/store/store.go). The type-discriminated spec (bandwidth / docker /
-- gpu / ios_build) + the terminal Result are stored as JSONB blobs — the store
-- layer marshals the matching *Spec / *Result struct into the column. Only the
-- columns the poll → status → settle path reads back (status, latest_status,
-- provider_id, workload_id, labels, the ios_build spec) need first-class typing;
-- the rest ride along in the blob so a future spec field never needs a schema
-- change (this is the durable assignment store, not a reporting warehouse).

CREATE TABLE IF NOT EXISTS workloads (
    id                   UUID PRIMARY KEY,
    workspace_id         TEXT NOT NULL DEFAULT '',
    submitted_by_user_id TEXT NOT NULL DEFAULT '',
    type                 TEXT NOT NULL,
    priority             TEXT NOT NULL DEFAULT 'normal',
    status               TEXT NOT NULL,
    submitted_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at           TIMESTAMPTZ,
    finished_at          TIMESTAMPTZ,
    -- labels carries the build-gateway routing key ("build_id") that the
    -- status-forward path resolves attempt → workload → build_id with. This
    -- is THE column the gateway callback depends on surviving across replicas.
    labels               JSONB NOT NULL DEFAULT '{}'::jsonb,
    -- exactly one of these is non-null per row (type-discriminated spec).
    bandwidth_spec       JSONB,
    docker_spec          JSONB,
    gpu_spec             JSONB,
    ios_build_spec       JSONB,
    -- terminal Result blob (succeeded/failed/… outcome). NULL until a terminal
    -- status arrives.
    result               JSONB
);

CREATE INDEX IF NOT EXISTS idx_workloads_workspace_submitted
    ON workloads (workspace_id, submitted_at);
CREATE INDEX IF NOT EXISTS idx_workloads_status
    ON workloads (status);

CREATE TABLE IF NOT EXISTS workload_assignments (
    id               UUID PRIMARY KEY,
    workload_id      UUID NOT NULL REFERENCES workloads(id) ON DELETE CASCADE,
    provider_id      TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deadline         TIMESTAMPTZ,
    accepted         BOOLEAN NOT NULL DEFAULT FALSE,
    -- latest_status drives ListPendingAssignments (the poll list serves rows
    -- where latest_status = 'dispatched'). The terminal POST moves it off
    -- 'dispatched' so a daemon restart doesn't re-run a finished build.
    latest_status    TEXT NOT NULL DEFAULT 'dispatched',
    rejection_reason TEXT NOT NULL DEFAULT ''
);

-- The poll endpoint filters (provider_id, latest_status='dispatched'); the
-- forwarder + status path look up by id (PK). This index covers the poll scan.
CREATE INDEX IF NOT EXISTS idx_workload_assignments_provider_status
    ON workload_assignments (provider_id, latest_status);
CREATE INDEX IF NOT EXISTS idx_workload_assignments_workload
    ON workload_assignments (workload_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS workload_assignments;
DROP TABLE IF EXISTS workloads;
-- +goose StatementEnd
