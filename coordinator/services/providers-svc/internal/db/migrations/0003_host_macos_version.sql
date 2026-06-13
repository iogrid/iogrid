-- Host macOS version on the provider capability record (#746, #737).
--
-- Background
--   The daemon's only providers-svc write was the pairing handshake,
--   which carries the CSR + display name but NO capabilities — the row
--   was created with supported_types={}, ios_build_enabled=false,
--   platform=NULL, and nothing refreshed it afterwards (the dispatch
--   stream advertises capabilities to workloads-svc, and the heartbeat
--   only bumps last_seen_at). So a Mac that gained iOS-build capability
--   after first pairing (e.g. Xcode installed later) stayed stale in the
--   admin / provider dashboard. #746 adds a startup capability report
--   (UpdateCapabilityInventory) that upserts the live capabilities.
--
--   #737 additionally routes iOS-build jobs by the host macOS major
--   version (Apple Virtualization.framework requires host macOS >= guest
--   macOS). This column makes the providers row version-bearing so the
--   admin dashboard can show which Xcode a Mac can actually run.
--
-- Semantics
--   host_macos_version is the macOS MAJOR version (14 = Sonoma, 15 =
--   Sequoia, 16 = Tahoe). 0 / NULL = unknown or not a macOS host. Stored
--   as a plain INT; defaulted to 0 so existing rows read back cleanly.

-- +goose Up
-- +goose StatementBegin

ALTER TABLE providers
    ADD COLUMN host_macos_version INTEGER NOT NULL DEFAULT 0;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE providers DROP COLUMN IF EXISTS host_macos_version;

-- +goose StatementEnd
