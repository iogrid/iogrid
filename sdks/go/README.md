# iogrid Go SDK

Official Go SDK for the [iogrid](https://iogrid.org) customer API.

```bash
go get github.com/iogrid/go-sdk
```

Go 1.22+. Zero runtime dependencies — pure `net/http` + `encoding/json`.

## Quick start

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    iogrid "github.com/iogrid/go-sdk"
)

func main() {
    c, err := iogrid.NewClient(iogrid.Options{APIKey: os.Getenv("IOGRID_API_KEY")})
    if err != nil { log.Fatal(err) }

    w, err := c.CreateWorkload(context.Background(), iogrid.CreateWorkloadRequest{
        Type:      iogrid.WorkloadTypeBandwidth,
        Bandwidth: &iogrid.BandwidthRequest{TargetURL: "https://example.com"},
    })
    if err != nil { log.Fatal(err) }
    fmt.Println(w.ID, w.Status)
}
```

## Examples

### 1. Submit a Docker workload

```go
w, err := c.CreateWorkload(ctx, iogrid.CreateWorkloadRequest{
    Type: iogrid.WorkloadTypeDocker,
    Docker: &iogrid.DockerRequest{
        Image:          "ghcr.io/example/scraper@sha256:abc...",
        Command:        []string{"./run.sh"},
        Env:            map[string]string{"CONCURRENCY": "4"},
        TimeoutSeconds: 900,
        MinCPUCores:    2,
        MinMemoryMiB:   1024,
    },
})
```

### 2. Stream a workload to completion

```go
events, errs, err := c.StreamWorkloadEvents(ctx, w.ID)
if err != nil { log.Fatal(err) }
for ev := range events {
    fmt.Printf("[%s] %s — %s\n", ev.OccurredAt.Format(time.RFC3339), ev.NewStatus, ev.Note)
}
if e := <-errs; e != nil { log.Fatal(e) }
```

### 3. Mint and rotate API keys

```go
k, err := c.CreateAPIKey(ctx, iogrid.CreateAPIKeyRequest{Name: "ci-pipeline-2026"})
if err != nil { log.Fatal(err) }
secret := k.Secret // only returned at creation time

keys, _ := c.ListAPIKeys(ctx)
for _, k := range keys { fmt.Println(k.ID, k.Name, k.Prefix) }

_ = c.DeleteAPIKey(ctx, "00000000-0000-0000-0000-000000000000")
```

### 4. Pull usage and invoices

```go
usage, _ := c.GetUsage(ctx, iogrid.GetUsageOptions{
    WindowStart: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
    WindowEnd:   time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
    Type:        iogrid.WorkloadTypeBandwidth,
    PageSize:    200,
})
var total uint64
for _, r := range usage { total += r.Quantity }
fmt.Printf("bandwidth in January: %.2f GB\n", float64(total)/1e9)

invs, _ := c.GetInvoices(ctx, iogrid.GetInvoicesOptions{})
for _, inv := range invs { fmt.Println(inv.ID, inv.Status, inv.Total.Micros) }
```

### 5. Error handling

```go
_, err := c.CreateWorkload(ctx, iogrid.CreateWorkloadRequest{...})
var ie *iogrid.Error
if errors.As(err, &ie) {
    switch ie.Code {
    case iogrid.ErrCodeInvalidArgument:
        fmt.Println("field:", ie.FieldPath)
    case iogrid.ErrCodeAbuseRateLimited:
        if delay, ok := iogrid.RetryAfterSeconds(ie); ok {
            fmt.Println("retry in", delay, "s")
        }
    default:
        fmt.Printf("iogrid error: %s (%s) reqID=%s\n", ie.Message, ie.Code, ie.RequestID)
    }
}
```

## Configuration

```go
iogrid.NewClient(iogrid.Options{
    APIKey:    "iog_…",                       // required
    BaseURL:   "https://api.iogrid.org",      // default
    Timeout:   30 * time.Second,              // default if HTTPClient is nil
    UserAgent: "my-app/1.0",                  // appended to the SDK UA
})
```

## Versioning

The Go SDK lives in the iogrid monorepo at `sdks/go/` but is published
under the module path `github.com/iogrid/go-sdk`. We follow semver.

### Tag convention

Releases are cut by pushing a git tag of the form `sdks/go/vX.Y.Z` on
the `iogrid/iogrid` repo. The matching workflow
(`.github/workflows/sdk-go-publish.yml`) validates the tag, runs
`go test -race`, and warms the `proxy.golang.org` cache so the new
version is queryable immediately:

```bash
go get github.com/iogrid/go-sdk@v0.1.0
```

Use the release Makefile rather than tagging by hand — it runs the
pre-flight gates (working tree clean, tests green, version not yet
published):

```bash
make -C sdks release-go VERSION=0.1.0
```

See [`../RELEASING.md`](../RELEASING.md) for the full release procedure.

## License

Apache-2.0
