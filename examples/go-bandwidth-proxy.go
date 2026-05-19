// Build:
//   go run ./examples/go-bandwidth-proxy.go
//
// Requires IOGRID_API_KEY in the environment.
//
//go:build ignore

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	iogrid "github.com/iogrid/go-sdk"
)

func main() {
	apiKey := os.Getenv("IOGRID_API_KEY")
	if apiKey == "" {
		log.Fatal("set IOGRID_API_KEY first")
	}
	c, err := iogrid.NewClient(iogrid.Options{APIKey: apiKey})
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	w, err := c.CreateWorkload(ctx, iogrid.CreateWorkloadRequest{
		Type:     iogrid.WorkloadTypeBandwidth,
		Priority: iogrid.WorkloadPriorityNormal,
		Bandwidth: &iogrid.BandwidthRequest{
			TargetURL:       "https://example.com/api/products",
			Method:          "GET",
			PreferredRegion: "us-east-1",
			Category:        "e_commerce",
		},
		Labels: map[string]string{"example": "bandwidth-proxy-go"},
	})
	if err != nil {
		var ie *iogrid.Error
		if errors.As(err, &ie) {
			log.Fatalf("create failed: %s (%s)", ie.Message, ie.Code)
		}
		log.Fatal(err)
	}
	fmt.Println("submitted", w.ID)

	events, errs, err := c.StreamWorkloadEvents(ctx, w.ID)
	if err != nil {
		log.Fatal(err)
	}
	for ev := range events {
		fmt.Printf("[%s] %s — %s\n", ev.OccurredAt.Format(time.RFC3339), ev.NewStatus, ev.Note)
	}
	if e := <-errs; e != nil {
		log.Fatal(e)
	}

	got, err := c.GetWorkload(ctx, w.ID)
	if err != nil {
		log.Fatal(err)
	}
	if got.Result != nil {
		fmt.Printf("terminal: %s bytesIn=%d cost=%+v\n",
			got.Result.TerminalStatus, got.Result.BytesIn, got.Result.Cost)
	}
}
