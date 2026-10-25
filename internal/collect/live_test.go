package collect

import (
	"context"
	"os"
	"testing"
	"time"
)

// COLLECT_LIVE=1 go test -run Live -v ./internal/collect reads this machine's tailscaled.
func TestLiveTailscale(t *testing.T) {
	if os.Getenv("COLLECT_LIVE") == "" {
		t.Skip("set COLLECT_LIVE=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	obs, err := TailscaleLocal(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range obs {
		t.Logf("%+v", o)
	}
}
