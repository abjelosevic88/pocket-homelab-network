// Package agent is `pocket-network agent` (and the small pocket-network-agent
// binary for routers): sweep this machine's network and report to a server.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/abjelosevic88/pocket-homelab-network/internal/collect"
	"github.com/abjelosevic88/pocket-homelab-network/internal/netscan"
)

// Run sweeps this machine's network on a schedule and posts what it saw
// to a Pocket Network server (Sources → Add → Agent gives the command).
// Names from dnsmasq's lease file are added when this runs on the router.
func Run(args []string, version string) {
	fs := flag.NewFlagSet("agent", flag.ExitOnError)
	server := fs.String("server", os.Getenv("NETWORK_SERVER"), "Pocket Network address (https://network.example.com)")
	token := fs.String("token", os.Getenv("NETWORK_AGENT_TOKEN"), "the agent token (pna_…)")
	subnet := fs.String("subnet", os.Getenv("SCAN_SUBNET"), "network to sweep (default: this machine's)")
	iface := fs.String("interface", os.Getenv("SCAN_INTERFACE"), "interface to sweep from")
	every := fs.Duration("every", 0, "how often (default: what the server says, 5m)")
	names := fs.Bool("names", true, "look names up (mDNS, NetBIOS, reverse DNS)")
	once := fs.Bool("once", false, "report once and exit")
	_ = fs.Parse(args)
	if *server == "" || !strings.HasPrefix(*token, "pna_") {
		fmt.Fprintln(os.Stderr, "pocket-network agent needs -server and -token (Sources → Add → Agent in Pocket Network shows both)")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	host, _ := os.Hostname()
	interval := 5 * time.Minute
	for {
		rep := map[string]any{"version": version, "host": host}
		start := time.Now()
		sctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		res, err := netscan.Sweep(sctx, netscan.Options{Interface: *iface, Subnet: *subnet, Names: *names})
		cancel()
		if err != nil {
			rep["error"] = err.Error()
			fmt.Fprintln(os.Stderr, "sweep:", err)
		} else {
			leases := collect.Leases(collect.LeaseFiles)
			for i := range res.Hosts {
				if n := leases[res.Hosts[i].MAC]; n != "" && (res.Hosts[i].Hostname == "" || res.Hosts[i].NameSource == "rdns") {
					res.Hosts[i].Hostname, res.Hosts[i].NameSource = n, "dhcp"
				}
			}
			rep["subnet"], rep["method"], rep["note"], rep["hosts"] = res.Subnet, res.Method, res.Note, res.Hosts
		}
		rep["took_ms"] = time.Since(start).Milliseconds()
		next, err := report(ctx, *server, *token, version, rep)
		if err != nil {
			fmt.Fprintln(os.Stderr, "report:", err)
		} else if res != nil {
			fmt.Printf("%s reported %d devices on %s (%s)\n", time.Now().Format("15:04:05"), len(res.Hosts), res.Subnet, res.Method)
		}
		if *once {
			return
		}
		if next > 0 {
			interval = next
		}
		if *every > 0 {
			interval = *every
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

func report(ctx context.Context, server, token, version string, rep map[string]any) (time.Duration, error) {
	b, _ := json.Marshal(rep)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(server, "/")+"/api/v1/agent/report", bytes.NewReader(b))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "pocket-network-agent/"+version)
	res, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	if res.StatusCode >= 300 {
		return 0, fmt.Errorf("HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		IntervalS int `json:"interval_s"`
	}
	_ = json.Unmarshal(body, &out)
	return time.Duration(out.IntervalS) * time.Second, nil
}

// Scan sweeps once and prints a table.
func Scan(args []string) {
	fs := flag.NewFlagSet("scan", flag.ExitOnError)
	subnet := fs.String("subnet", "", "network to sweep (default: this machine's)")
	iface := fs.String("interface", "", "interface to sweep from")
	names := fs.Bool("names", true, "look names up")
	_ = fs.Parse(args)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	res, err := netscan.Sweep(ctx, netscan.Options{Interface: *iface, Subnet: *subnet, Names: *names})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("%s on %s: %d devices in %s\n", res.Subnet, res.Interface, len(res.Hosts), res.Took.Round(100*time.Millisecond))
	if res.Note != "" {
		fmt.Println("note:", res.Note)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ADDRESS\tMAC\tNAME\tMAKER\tLOOKS LIKE")
	for _, h := range res.Hosts {
		maker := h.Vendor
		if netscan.PrivateMAC(h.MAC) {
			maker = "(private address)"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", h.IP, h.MAC, h.Hostname, maker, netscan.KindName[netscan.GuessKind(h.Hostname, h.Vendor, h.Model, "")])
	}
	tw.Flush()
}
