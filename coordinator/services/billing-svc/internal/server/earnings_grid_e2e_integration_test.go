//go:build integration

// End-to-end proof for #758: seed the EXACT prod grid_build_settlement
// rows for provider 808ce330 (= "Hatices-Mac-mini-2"), call the REAL
// EarningsService.GetEarningsSummary RPC, and marshal the response with
// protojson — the exact bytes gateway-bff's writeProtoJSON streams to the
// browser. Asserts the headline TotalEarned renders 4.25 $GRID (4_250_000
// micros) + settled_builds=3, proving the dashboard number traces to real
// on-chain settlement rows (tx sigs 4Zrmyw8…/24ZBknAf…/5gTP6ZA5…), never
// hardcoded.
//
// Run with the same throwaway PG fixture as the grid integration suite:
//
//	DATABASE_URL=postgres://postgres:postgres@localhost:55433/billing_svc_test?sslmode=disable \
//	    go test -tags=integration -run TestGridEarningsE2E ./internal/server/...
package server

import (
	"context"
	"os"
	"testing"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/encoding/protojson"

	billingv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1"
	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	shareddb "github.com/iogrid/iogrid/coordinator/shared/db"
	billingstore "github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/store"
)

const e2eDSN = "postgres://postgres:postgres@localhost:55433/billing_svc_test?sslmode=disable"

// The exact prod attribution: provider 808ce330's 3 settled build rows.
const provider808 = "808ce330-79c1-4390-8cc6-87c5ce5a94d8"

func TestGridEarningsE2E_ProdRowsRenderNonZeroGrid(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = e2eDSN
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("no Postgres at %s: %v", dsn, err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("Postgres unreachable: %v", err)
	}
	if err := shareddb.MigrateUpFS(ctx, dsn, billingstore.Migrations, "migrations"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := pool.Exec(ctx, `TRUNCATE grid_build_settlement`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	// Seed the 3 rows attributed to provider 808ce330 with the REAL prod
	// provider_share + tx_signatures, settled_at stamped (= on-chain confirmed).
	const seed = `
INSERT INTO grid_build_settlement
  (id, build_id, attempt_id, customer_wallet, provider_id, provider_wallet,
   escrowed_atomic, consumed_atomic, refund_atomic, provider_share, iogrid_share,
   tx_signature, settled_at)
VALUES
 (gen_random_uuid(), gen_random_uuid(), gen_random_uuid(), 'Cust', $1::uuid, 'Wal',
  3000000000, 3000000000, 0, 2550000000, 450000000,
  '5gTP6ZA5HWZKZnAFu7SL4LYLimBMHsvxMPYGC399kPFZW1ciaNRPJxD9aPy5k2eTKtwTHLvkhc13112fiFY2Who4', now()),
 (gen_random_uuid(), gen_random_uuid(), gen_random_uuid(), 'Cust', $1::uuid, 'Wal',
  1000000000, 1000000000, 0, 850000000, 150000000,
  '4Zrmyw8oT97CnCR54wugVVK5ihXqgpsNPjKYiggK7psVctLLcagtxTx7PW3EpgJ9omXAuxomXmkotB3TSq7LU5yX', now()),
 (gen_random_uuid(), gen_random_uuid(), gen_random_uuid(), 'Cust', $1::uuid, 'Wal',
  1000000000, 1000000000, 0, 850000000, 150000000,
  '24ZBknAf2syLtHkN9wsqGKD7hunubBCHPaFYNwQTvGBnPKmWEi7K3H9u5rssorw128PD691CsWPv5kecDkqFLAhc', now())`
	if _, err := pool.Exec(ctx, seed, provider808); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Call the REAL RPC handler — same code path the running service uses.
	h := NewEarningsHandler(billingstore.New(pool))
	resp, err := h.GetEarningsSummary(ctx, connect.NewRequest(&billingv1.GetEarningsSummaryRequest{
		ProviderId: &commonv1.UUID{Value: provider808},
	}))
	if err != nil {
		t.Fatalf("GetEarningsSummary: %v", err)
	}
	s := resp.Msg.GetSummary()

	// 2_550_000_000 + 850_000_000 + 850_000_000 = 4_250_000_000 atomic.
	// micros = atomic / 1000 = 4_250_000.  $GRID = 4.25.
	if got := s.GetTotalEarned().GetMicros(); got != 4_250_000 {
		t.Errorf("TotalEarned.micros = %d, want 4_250_000 (4.25 $GRID)", got)
	}
	if got := s.GetTotalEarned().GetCurrency(); got != "GRID" {
		t.Errorf("currency = %q, want GRID", got)
	}
	if got := s.GetSettledBuilds(); got != 3 {
		t.Errorf("settledBuilds = %d, want 3", got)
	}
	if got := s.GetSettledGrid().GetMicros(); got != 4_250_000 {
		t.Errorf("settledGrid.micros = %d, want 4_250_000", got)
	}

	// Emit the EXACT proto3-JSON bytes gateway-bff's writeProtoJSON streams
	// to the browser — this is the dashboard payload.
	js, _ := protojson.MarshalOptions{Multiline: true, Indent: "  "}.Marshal(resp.Msg)
	t.Logf("BFF→browser payload (real on-chain $GRID for Hatices-Mac-mini-2):\n%s", js)
}
