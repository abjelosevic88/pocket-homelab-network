package netscan

import (
	"context"
	"os"
	"testing"
	"time"
)

// NETSCAN_LIVE=1 go test -run Live -v ./internal/netscan sweeps this machine's network.
func TestLiveSweep(t *testing.T) {
	if os.Getenv("NETSCAN_LIVE") == "" {
		t.Skip("set NETSCAN_LIVE=1 to sweep the real network")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	t.Logf("interfaces: %+v", Interfaces())
	res, err := Sweep(ctx, Options{Names: true, Subnet: os.Getenv("NETSCAN_SUBNET")})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s on %s (%s) in %s %s", res.Method, res.Interface, res.Subnet, res.Took, res.Note)
	for _, h := range res.Hosts {
		t.Logf("%-16s %s %-28s %-8s %-30s model=%s guess=%s", h.IP, h.MAC, h.Hostname, h.NameSource, h.Vendor, h.Model, GuessKind(h.Hostname, h.Vendor, h.Model, ""))
	}
}
