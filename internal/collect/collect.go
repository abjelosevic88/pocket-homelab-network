// Package collect reads devices from places other than a sweep: Tailscale
// (the local daemon or the API), AdGuard Home (clients and DNS activity), an
// OpenWrt router (DHCP leases, Wi-Fi clients, its ARP table) and dnsmasq
// lease files (for the agent). Everything comes back as netscan.Observation.
package collect

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-network/internal/netscan"
)

// Obs is short for an observation.
type Obs = netscan.Observation

func readJSON(res *http.Response, v any) error {
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return fmt.Errorf("the login was refused (HTTP %d)", res.StatusCode)
	}
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 300))
		return fmt.Errorf("HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(io.LimitReader(res.Body, 32<<20)).Decode(v)
}

// ---------------- Tailscale ----------------

type tsPeer struct {
	ID           string    `json:"ID"`
	HostName     string    `json:"HostName"`
	DNSName      string    `json:"DNSName"`
	OS           string    `json:"OS"`
	TailscaleIPs []string  `json:"TailscaleIPs"`
	CurAddr      string    `json:"CurAddr"`
	Online       bool      `json:"Online"`
	LastSeen     time.Time `json:"LastSeen"`
}

// DefaultTailscaleSocket is where tailscaled listens on Linux.
const DefaultTailscaleSocket = "/var/run/tailscale/tailscaled.sock"

// TailscaleLocal reads the peers from the tailscaled on this machine.
func TailscaleLocal(ctx context.Context, socket string) ([]Obs, error) {
	if socket == "" {
		socket = DefaultTailscaleSocket
	}
	if _, err := os.Stat(socket); err != nil {
		return nil, fmt.Errorf("no Tailscale on this machine (%s is missing); mount the socket or use an API key", socket)
	}
	cl := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", socket)
	}}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://local-tailscaled.sock/localapi/v0/status", nil)
	req.Host = "local-tailscaled.sock"
	res, err := cl.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tailscaled: %w", err)
	}
	var st struct {
		Self *tsPeer           `json:"Self"`
		Peer map[string]tsPeer `json:"Peer"`
	}
	if err := readJSON(res, &st); err != nil {
		return nil, fmt.Errorf("tailscaled: %w", err)
	}
	var out []Obs
	add := func(p tsPeer, self bool) {
		o := Obs{TailscaleID: p.ID, Hostname: tsName(p.HostName, p.DNSName), NameSource: "tailscale", OS: tsOS(p.OS), Self: self, LastSeen: p.LastSeen}
		if len(p.TailscaleIPs) > 0 {
			o.IP = p.TailscaleIPs[0]
		}
		if !p.Online && !self {
			o.Offline = true
		}
		out = append(out, o)
		if lan := lanIP(p.CurAddr); lan != "" {
			out = append(out, Obs{IP: lan, TailscaleID: p.ID, NameSource: "tailscale-lan", Offline: o.Offline})
		}
	}
	if st.Self != nil {
		add(*st.Self, true)
	}
	for _, p := range st.Peer {
		if p.ID != "" {
			add(p, false)
		}
	}
	return out, nil
}

// TailscaleAPI reads the tailnet's devices with an API key (tailnet "-" = the key's own).
func TailscaleAPI(ctx context.Context, baseURL, tailnet, key string) ([]Obs, error) {
	if key == "" {
		return nil, errors.New("add a Tailscale API key (admin console → Settings → Keys)")
	}
	if baseURL == "" {
		baseURL = "https://api.tailscale.com"
	}
	if tailnet == "" {
		tailnet = "-"
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/api/v2/tailnet/"+tailnet+"/devices?fields=all", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	res, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	var body struct {
		Devices []struct {
			NodeID             string    `json:"nodeId"`
			Hostname           string    `json:"hostname"`
			Name               string    `json:"name"`
			OS                 string    `json:"os"`
			Addresses          []string  `json:"addresses"`
			LastSeen           time.Time `json:"lastSeen"`
			ConnectedToControl bool      `json:"connectedToControl"`
			ClientConnectivity *struct {
				Endpoints []string `json:"endpoints"`
			} `json:"clientConnectivity"`
		} `json:"devices"`
	}
	if err := readJSON(res, &body); err != nil {
		return nil, fmt.Errorf("Tailscale API: %w", err)
	}
	var out []Obs
	for _, d := range body.Devices {
		online := d.ConnectedToControl || time.Since(d.LastSeen) < 3*time.Minute
		o := Obs{TailscaleID: d.NodeID, Hostname: tsName(d.Hostname, d.Name), NameSource: "tailscale", OS: tsOS(d.OS), Offline: !online, LastSeen: d.LastSeen}
		if len(d.Addresses) > 0 {
			o.IP = d.Addresses[0]
		}
		out = append(out, o)
		if d.ClientConnectivity != nil {
			for _, ep := range d.ClientConnectivity.Endpoints {
				if lan := lanIP(ep); lan != "" {
					out = append(out, Obs{IP: lan, TailscaleID: d.NodeID, NameSource: "tailscale-lan", Offline: !online})
					break
				}
			}
		}
	}
	return out, nil
}

func tsName(host, dns string) string {
	if dns != "" {
		if first, _, ok := strings.Cut(strings.TrimSuffix(dns, "."), "."); ok && first != "" {
			return first
		}
	}
	return host
}

func tsOS(os string) string {
	switch strings.ToLower(os) {
	case "ios":
		return "iOS"
	case "macos":
		return "macOS"
	case "android":
		return "Android"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	case "tvos":
		return "tvOS"
	case "freebsd":
		return "FreeBSD"
	}
	return os
}

// lanIP is the private IPv4 of an "ip:port" endpoint ("" for public ones).
func lanIP(ep string) string {
	h, _, err := net.SplitHostPort(ep)
	if err != nil {
		h = ep
	}
	ip := net.ParseIP(h)
	if ip == nil || ip.To4() == nil || !ip.IsPrivate() {
		return ""
	}
	return ip.String()
}

// ---------------- AdGuard Home ----------------

// AdGuard reads AdGuard Home's clients and their DNS activity: queries in the
// stats period per address, and from the recent query log who asked
// something in the last ten minutes (online) and how much was blocked.
func AdGuard(ctx context.Context, base, user, pass string) ([]Obs, error) {
	base = strings.TrimRight(base, "/")
	if base == "" {
		return nil, errors.New("add the AdGuard Home address")
	}
	cl := &http.Client{Timeout: 20 * time.Second}
	get := func(path string, v any) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
		if err != nil {
			return err
		}
		if user != "" {
			req.SetBasicAuth(user, pass)
		}
		res, err := cl.Do(req)
		if err != nil {
			return err
		}
		return readJSON(res, v)
	}
	var clients struct {
		Clients []struct {
			Name string   `json:"name"`
			IDs  []string `json:"ids"`
		} `json:"clients"`
		Auto []struct {
			Name   string `json:"name"`
			IP     string `json:"ip"`
			Source string `json:"source"`
		} `json:"auto_clients"`
	}
	if err := get("/control/clients", &clients); err != nil {
		return nil, fmt.Errorf("AdGuard Home: %w", err)
	}
	var stats struct {
		TopClients []map[string]int `json:"top_clients"`
	}
	_ = get("/control/stats", &stats)
	var qlog struct {
		Data []struct {
			Client string `json:"client"`
			Time   string `json:"time"`
			Reason string `json:"reason"`
		} `json:"data"`
	}
	_ = get("/control/querylog?limit=1000&response_status=all", &qlog)

	byIP := map[string]*Obs{}
	at := func(ip string) *Obs {
		ip = strings.TrimSpace(ip)
		p := net.ParseIP(ip)
		if p == nil || p.IsLoopback() || p.To4() == nil {
			return nil
		}
		if o, ok := byIP[ip]; ok {
			return o
		}
		o := &Obs{IP: ip, Offline: true, NameSource: "adguard"}
		byIP[ip] = o
		return o
	}
	var macNamed []Obs
	for _, c := range clients.Clients {
		for _, id := range c.IDs {
			if mac := netscan.NormalizeMAC(id); mac != "" {
				macNamed = append(macNamed, Obs{MAC: mac, ClientName: c.Name, NameSource: "adguard", Offline: true})
			} else if o := at(id); o != nil {
				o.ClientName = c.Name
			}
		}
	}
	for _, a := range clients.Auto {
		if o := at(a.IP); o != nil && a.Name != "" && !strings.EqualFold(a.Name, a.IP) {
			o.Hostname = netscan.CleanName(a.Name)
		}
	}
	for _, m := range stats.TopClients {
		for ip, n := range m {
			if o := at(ip); o != nil {
				o.DNSQueries = n
			}
		}
	}
	cut := time.Now().Add(-10 * time.Minute)
	for _, q := range qlog.Data {
		o := at(q.Client)
		if o == nil {
			continue
		}
		if strings.HasPrefix(q.Reason, "Filtered") && q.Reason != "FilteredWhiteList" {
			o.DNSBlocked++
		}
		if t, err := time.Parse(time.RFC3339Nano, q.Time); err == nil && t.After(cut) {
			o.Offline = false
			if t.After(o.LastSeen) {
				o.LastSeen = t
			}
		}
	}
	out := macNamed
	for _, o := range byIP {
		out = append(out, *o)
	}
	return out, nil
}

// ---------------- OpenWrt (and GL.iNet with LuCI) ----------------

type ubus struct {
	url string
	sid string
	cl  *http.Client
}

func (u *ubus) call(ctx context.Context, object, method string, args map[string]any, v any) error {
	if args == nil {
		args = map[string]any{}
	}
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "call", "params": []any{u.sid, object, method, args}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := u.cl.Do(req)
	if err != nil {
		return err
	}
	var out struct {
		Result []json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := readJSON(res, &out); err != nil {
		return err
	}
	if out.Error != nil {
		return fmt.Errorf("%s.%s: %s", object, method, out.Error.Message)
	}
	if len(out.Result) == 0 {
		return fmt.Errorf("%s.%s: empty answer", object, method)
	}
	var code int
	_ = json.Unmarshal(out.Result[0], &code)
	if code != 0 {
		if code == 6 {
			return fmt.Errorf("%s.%s: permission denied for this user", object, method)
		}
		return fmt.Errorf("%s.%s: ubus error %d", object, method, code)
	}
	if v != nil && len(out.Result) > 1 {
		return json.Unmarshal(out.Result[1], v)
	}
	return nil
}

// OpenWrt reads a router over ubus (LuCI's JSON-RPC at /ubus): DHCP leases
// and host hints name devices; Wi-Fi associations and the router's ARP table
// say who is there now.
func OpenWrt(ctx context.Context, base, user, pass string) ([]Obs, error) {
	base = strings.TrimRight(base, "/")
	if base == "" {
		return nil, errors.New("add the router's address")
	}
	if !strings.HasSuffix(base, "/ubus") {
		base += "/ubus"
	}
	if user == "" {
		user = "root"
	}
	u := &ubus{url: base, sid: "00000000000000000000000000000000", cl: &http.Client{Timeout: 20 * time.Second}}
	var login struct {
		Session string `json:"ubus_rpc_session"`
	}
	if err := u.call(ctx, "session", "login", map[string]any{"username": user, "password": pass}, &login); err != nil {
		return nil, fmt.Errorf("router login: %w", err)
	}
	u.sid = login.Session
	byMAC := map[string]*Obs{}
	at := func(mac string) *Obs {
		mac = netscan.NormalizeMAC(mac)
		if mac == "" {
			return nil
		}
		if o, ok := byMAC[mac]; ok {
			return o
		}
		o := &Obs{MAC: mac, Offline: true, NameSource: "dhcp"}
		byMAC[mac] = o
		return o
	}
	var leases struct {
		Leases []struct {
			Hostname string `json:"hostname"`
			IP       string `json:"ipaddr"`
			MAC      string `json:"macaddr"`
		} `json:"dhcp_leases"`
	}
	if err := u.call(ctx, "luci-rpc", "getDHCPLeases", map[string]any{"family": 4}, &leases); err != nil {
		return nil, err
	}
	for _, l := range leases.Leases {
		if o := at(l.MAC); o != nil {
			o.IP = l.IP
			if n := netscan.CleanName(l.Hostname); n != "" {
				o.Hostname = n
			}
		}
	}
	var hints map[string]struct {
		Name    string   `json:"name"`
		IPAddrs []string `json:"ipaddrs"`
	}
	if u.call(ctx, "luci-rpc", "getHostHints", nil, &hints) == nil {
		for mac, h := range hints {
			if o := at(mac); o != nil {
				if o.Hostname == "" {
					o.Hostname = netscan.CleanName(h.Name)
				}
				if o.IP == "" && len(h.IPAddrs) > 0 {
					o.IP = h.IPAddrs[0]
				}
			}
		}
	}
	var devs struct {
		Devices []string `json:"devices"`
	}
	if u.call(ctx, "iwinfo", "devices", nil, &devs) == nil {
		for _, d := range devs.Devices {
			var al struct {
				Results []struct {
					MAC string `json:"mac"`
				} `json:"results"`
			}
			if u.call(ctx, "iwinfo", "assoclist", map[string]any{"device": d}, &al) == nil {
				for _, r := range al.Results {
					if o := at(r.MAC); o != nil {
						o.Offline = false
					}
				}
			}
		}
	}
	var arp struct {
		Data string `json:"data"`
	}
	if u.call(ctx, "file", "read", map[string]any{"path": "/proc/net/arp"}, &arp) == nil {
		sc := bufio.NewScanner(strings.NewReader(arp.Data))
		sc.Scan()
		for sc.Scan() {
			fs := strings.Fields(sc.Text())
			if len(fs) >= 6 && fs[2] == "0x2" {
				if o := at(fs[3]); o != nil {
					o.Offline = false
					if o.IP == "" {
						o.IP = fs[0]
					}
				}
			}
		}
	}
	out := make([]Obs, 0, len(byMAC))
	for _, o := range byMAC {
		o.Vendor = netscan.Vendor(o.MAC)
		out = append(out, *o)
	}
	return out, nil
}

// ---------------- dnsmasq lease files (the agent on a router) ----------------

// LeaseFiles are where dnsmasq (OpenWrt, GL.iNet, Pi-hole, most Linux routers) keeps its leases.
var LeaseFiles = []string{"/tmp/dhcp.leases", "/var/lib/misc/dnsmasq.leases", "/var/lib/dnsmasq/dnsmasq.leases", "/etc/pihole/dhcp.leases"}

// Leases reads the first dnsmasq lease file found: MAC → hostname.
func Leases(paths []string) map[string]string {
	out := map[string]string{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			// expiry mac ip hostname client-id
			fs := strings.Fields(line)
			if len(fs) < 4 {
				continue
			}
			if mac := netscan.NormalizeMAC(fs[1]); mac != "" && fs[3] != "*" {
				out[mac] = netscan.CleanName(fs[3])
			}
		}
		break
	}
	return out
}
