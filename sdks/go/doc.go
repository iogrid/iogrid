// Package iogrid is the official Go SDK for the iogrid customer API.
//
// Quick start:
//
//	c, err := iogrid.NewClient(iogrid.Options{APIKey: os.Getenv("IOGRID_API_KEY")})
//	if err != nil { log.Fatal(err) }
//
//	w, err := c.CreateWorkload(ctx, iogrid.CreateWorkloadRequest{
//	    Type: iogrid.WorkloadTypeBandwidth,
//	    Bandwidth: &iogrid.BandwidthRequest{TargetURL: "https://example.com"},
//	})
//
// Methods exposed:
//
//	CreateWorkload, GetWorkload, ListWorkloads, CancelWorkload,
//	StreamWorkloadEvents (channel-based),
//	CreateAPIKey, ListAPIKeys, DeleteAPIKey,
//	GetUsage, GetInvoices.
//
// All methods are context-aware; cancelling the context aborts the
// underlying HTTP request (and, for streams, closes the channel).
package iogrid
