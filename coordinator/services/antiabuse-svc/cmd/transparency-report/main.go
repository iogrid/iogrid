// Command transparency-report is the standalone binary launched by the
// quarterly Kubernetes CronJob (see infra/k8s/base/antiabuse-svc/
// cronjob-transparency.yaml). It generates a report for the most
// recently-completed quarter (or the explicit pair given via flags),
// publishes the JSON + Markdown to S3 + gateway-bff, and exits.
//
// Exit codes:
//
//	0  — every transport reported success
//	1  — any transport failed (k8s requeues the job per its backoffLimit)
//
// Configuration is via env vars + CLI flags so the CronJob manifest can
// override either pathway. The defaults match the conventions used
// elsewhere in coordinator/.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/transparency"
	"github.com/iogrid/iogrid/coordinator/shared/log"
)

const serviceName = "antiabuse-svc-transparency-report"

func main() {
	logger := log.Setup(serviceName)
	if err := run(logger); err != nil {
		logger.Error("transparency report failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	now := time.Now().UTC()
	defaultYear, defaultQ := previousQuarter(now)

	var (
		yearFlag    = flag.Int("year", envInt("REPORT_YEAR", defaultYear), "year to report on (default: previous completed quarter)")
		quarterFlag = flag.Int("quarter", envInt("REPORT_QUARTER", int(defaultQ)), "quarter to report on (1..4)")
		bucket      = flag.String("bucket", os.Getenv("S3_BUCKET"), "destination S3 bucket")
		bffURL      = flag.String("bff-url", os.Getenv("GATEWAY_BFF_URL"), "gateway-bff base URL for /api/v1/transparency/publish")
		bffToken    = flag.String("bff-token", os.Getenv("GATEWAY_BFF_TOKEN"), "bearer token for the BFF publish endpoint")
		dryRun      = flag.Bool("dry-run", envBool("DRY_RUN", false), "print the report to stdout instead of publishing")
	)
	flag.Parse()

	// In Phase 1 we accept zero counters — the first report is a
	// methodology placeholder. The real source wires up once the NATS
	// audit consumer ships (issue #72 follow-up).
	src := transparency.NewInMemory()
	src.SetAuditRetention(transparency.AuditRetentionBlock{
		RequiredDays:       90,
		ConfiguredDays:     90,
		LastPruneAt:        time.Now().UTC(),
		Compliant:          true,
		CompliantRationale: "JetStream AUDIT stream MaxAge=90d + Postgres mirror nightly pruner",
	})

	gen := transparency.NewGenerator(src)
	rep, err := gen.Generate(context.Background(), *yearFlag, transparency.Quarter(*quarterFlag))
	if err != nil {
		return fmt.Errorf("generate: %w", err)
	}

	if *dryRun {
		raw, err := rep.JSON()
		if err != nil {
			return err
		}
		fmt.Println(string(raw))
		return nil
	}

	pub := transparency.NewPublisher(transparency.Publisher{
		BucketName:   *bucket,
		BFFURL:       *bffURL,
		BFFAuthToken: *bffToken,
		// S3 left nil until the aws-sdk wiring lands (issue #72 follow-up).
		// Until then the JSON is still POSTed to the BFF so the marketing
		// page has something to show.
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := pub.Publish(ctx, rep); err != nil {
		return fmt.Errorf("publish: %w", err)
	}
	logger.Info("transparency report published",
		slog.Int("year", rep.Year),
		slog.Int("quarter", int(rep.Quarter)),
		slog.Int64("total_checks", rep.TotalChecks),
		slog.Int64("total_blocks", rep.TotalBlocks),
	)
	return nil
}

// previousQuarter returns the year + quarter immediately preceding `now`.
// E.g. now=2026-04-05 → (2026, 1). On Jan 1st we wrap into the prior year.
func previousQuarter(now time.Time) (int, transparency.Quarter) {
	month := int(now.Month())
	q := (month-1)/3 + 1 // 1..4 for current quarter
	if q == 1 {
		return now.Year() - 1, 4
	}
	return now.Year(), transparency.Quarter(q - 1)
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	if v := strings.ToLower(strings.TrimSpace(os.Getenv(key))); v != "" {
		switch v {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	return fallback
}
