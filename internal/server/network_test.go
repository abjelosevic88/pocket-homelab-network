package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/abjelosevic88/pocket-homelab-network/internal/config"
	"github.com/abjelosevic88/pocket-homelab-network/internal/netscan"
	"github.com/abjelosevic88/pocket-homelab-network/internal/store"
)

type app struct {
	t     *testing.T
	url   string
	s     *Server
	st    *store.Store
	alice string
	bob   string
	ids   map[string]int64
}

func newApp(t *testing.T, tweak func(*config.Config)) *app {
	t.Helper()
	cfg := testConfig(t.TempDir())
	cfg.BaseURL = "http://network.test"
	cfg.MaxBodyBytes = 64 << 20
	if tweak != nil {
		tweak(cfg)
	}
	st := openTestStore(t, cfg)
	ctx := context.Background()
	a := &app{t: t, st: st, ids: map[string]int64{}}
	for _, u := range []*store.User{{Email: "alice@example.com", Name: "Alice", Role: "admin"}, {Email: "bob@example.com", Name: "Bob", Role: "member"}} {
		if err := st.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
		tok, _, _ := st.CreateAPIToken(ctx, u.ID, "t")
		a.ids[u.Name] = u.ID
		if u.Name == "Alice" {
			a.alice = tok
		} else {
			a.bob = tok
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	a.s = New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err := a.s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(a.s.Router())
	t.Cleanup(ts.Close)
	a.url = ts.URL
	return a
}

func (a *app) call(auth, method, path string, body any) (int, map[string]any, []any) {
	a.t.Helper()
	var rd io.Reader
	if body != nil {
		j, _ := json.Marshal(body)
		rd = bytes.NewReader(j)
	}
	req, _ := http.NewRequest(method, a.url+path, rd)
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var m map[string]any
	var l []any
	if json.Unmarshal(raw, &m) != nil {
		_ = json.Unmarshal(raw, &l)
	}
	return res.StatusCode, m, l
}

func (a *app) ok(auth, method, path string, body any) map[string]any {
	a.t.Helper()
	code, m, _ := a.call(auth, method, path, body)
	if code >= 300 {
		a.t.Fatalf("%s %s → %d %v", method, path, code, m)
	}
	return m
}

func (a *app) list(auth, path string) []any {
	a.t.Helper()
	code, m, l := a.call(auth, "GET", path, nil)
	if code != 200 {
		a.t.Fatalf("GET %s → %d %v", path, code, m)
	}
	return l
}

func (a *app) expect(want int, auth, method, path string, body any) map[string]any {
	a.t.Helper()
	code, m, _ := a.call(auth, method, path, body)
	if code != want {
		a.t.Fatalf("%s %s → %d, want %d (%v)", method, path, code, want, m)
	}
	return m
}

func num(m map[string]any, k string) float64 { v, _ := m[k].(float64); return v }

// agentFor makes an agent source for a person through the API and returns its token.
func (a *app) agentFor(auth string) (int64, string) {
	a.t.Helper()
	m := a.expect(201, auth, "POST", "/api/v1/sources", map[string]any{"kind": "agent", "name": "Garden office", "network": "Garden office"})
	src := m["source"].(map[string]any)
	return int64(num(src, "id")), m["token"].(string)
}

func (a *app) report(token string, hosts []netscan.Observation) map[string]any {
	a.t.Helper()
	return a.expect(200, token, "POST", "/api/v1/agent/report", map[string]any{"subnet": "10.0.0.0/24", "hosts": hosts})
}

// Strictly per person: another person's devices and sources are 404, lists are their own.
func TestPerPersonIsolation(t *testing.T) {
	a := newApp(t, nil)
	d := a.expect(201, a.alice, "POST", "/api/v1/devices", map[string]any{"name": "Alice's printer", "mac": "30:05:5c:00:00:01", "kind": "printer"})
	id := int64(num(d, "id"))
	a.expect(404, a.bob, "GET", fmt.Sprintf("/api/v1/devices/%d", id), nil)
	a.expect(404, a.bob, "PUT", fmt.Sprintf("/api/v1/devices/%d", id), map[string]any{"name": "mine now"})
	a.expect(404, a.bob, "DELETE", fmt.Sprintf("/api/v1/devices/%d", id), nil)
	a.expect(404, a.bob, "POST", fmt.Sprintf("/api/v1/devices/%d/merge", id), map[string]any{"ids": []int64{id}})
	if l := a.list(a.bob, "/api/v1/devices"); len(l) != 0 {
		t.Fatalf("bob sees %d devices", len(l))
	}
	srcID, _ := a.agentFor(a.alice)
	a.expect(404, a.bob, "PUT", fmt.Sprintf("/api/v1/sources/%d", srcID), map[string]any{"name": "x"})
	a.expect(404, a.bob, "GET", fmt.Sprintf("/api/v1/sources/%d/scans", srcID), nil)
	a.expect(404, a.bob, "DELETE", fmt.Sprintf("/api/v1/sources/%d", srcID), nil)
	m := a.ok(a.bob, "GET", "/api/v1/sources", nil)
	if l := m["sources"].([]any); len(l) != 0 {
		t.Fatalf("bob sees %d sources", len(l))
	}
	// Bob merging Alice's device into his own: 404, nothing moves.
	bd := a.expect(201, a.bob, "POST", "/api/v1/devices", map[string]any{"name": "Bob's phone"})
	a.expect(404, a.bob, "POST", fmt.Sprintf("/api/v1/devices/%d/merge", int64(num(bd, "id"))), map[string]any{"ids": []int64{id}})
	a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/devices/%d", id), nil)
	// A member cannot scan the server's own network.
	a.expect(400, a.bob, "POST", "/api/v1/sources", map[string]any{"kind": "scan"})
	// A member cannot read the server's Tailscale without an API key of their own.
	a.expect(400, a.bob, "POST", "/api/v1/sources", map[string]any{"kind": "tailscale"})
}

func TestAgentReportsDevices(t *testing.T) {
	a := newApp(t, nil)
	_, tok := a.agentFor(a.alice)
	a.expect(401, "pna_wrong", "POST", "/api/v1/agent/report", map[string]any{"hosts": []any{}})
	r := a.report(tok, []netscan.Observation{
		{IP: "10.0.0.1", MAC: "94:83:c4:ce:dc:52", Hostname: "GL-MT3000", NameSource: "dhcp"},
		{IP: "10.0.0.21", MAC: "10:c5:95:aa:bb:cc", Hostname: "LEO-THINKPAD", NameSource: "netbios"},
	})
	if num(r, "found") != 2 || num(r, "new") != 2 {
		t.Fatalf("report = %v", r)
	}
	devs := a.list(a.alice, "/api/v1/devices")
	if len(devs) != 2 {
		t.Fatalf("%d devices", len(devs))
	}
	var router map[string]any
	for _, d := range devs {
		if m := d.(map[string]any); m["hostname"] == "GL-MT3000" {
			router = m
		}
	}
	if router == nil || router["review"] != "new" || router["vendor"] != "GL Technologies (Hong Kong)" || router["kind_guess"] != "router" || router["watch"] != true || router["online"] != true {
		t.Fatalf("router = %v", router)
	}
	// Same MAC again: same device, the new address is noted.
	a.report(tok, []netscan.Observation{{IP: "10.0.0.2", MAC: "94:83:C4:CE:DC:52"}})
	if l := a.list(a.alice, "/api/v1/devices"); len(l) != 2 {
		t.Fatalf("a known MAC made a new device (%d)", len(l))
	}
	evs := a.list(a.alice, "/api/v1/events?kind=ip")
	if len(evs) != 1 || !strings.Contains(evs[0].(map[string]any)["text"].(string), "10.0.0.1 to 10.0.0.2") {
		t.Fatalf("ip events = %v", evs)
	}
	// Bob sees nothing of it.
	if l := a.list(a.bob, "/api/v1/devices"); len(l) != 0 {
		t.Fatal("bob sees alice's devices")
	}
}

// A phone that makes up a new private address keeps being one device.
func TestPrivateAddressJoinsSameName(t *testing.T) {
	a := newApp(t, nil)
	_, tok := a.agentFor(a.alice)
	a.report(tok, []netscan.Observation{{IP: "10.0.0.50", MAC: "be:1a:c9:62:bb:c1", Hostname: "Mayas-iPhone", NameSource: "mdns"}})
	a.report(tok, []netscan.Observation{{IP: "10.0.0.57", MAC: "b6:8a:2f:19:0b:a3", Hostname: "Mayas-iPhone", NameSource: "mdns"}})
	devs := a.list(a.alice, "/api/v1/devices")
	if len(devs) != 1 {
		t.Fatalf("%d devices, want 1", len(devs))
	}
	d := devs[0].(map[string]any)
	det := a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/devices/%d", int64(num(d, "id"))), nil)
	if ids := det["ids"].([]any); len(ids) != 2 || ids[0].(map[string]any)["private"] != true {
		t.Fatalf("ids = %v", ids)
	}
	// Generic names are never joined.
	a.report(tok, []netscan.Observation{{IP: "10.0.0.60", MAC: "c2:ff:ec:87:51:db", Hostname: "iPhone"}, {IP: "10.0.0.61", MAC: "c6:ff:ec:87:51:dc", Hostname: "iPhone"}})
	if l := a.list(a.alice, "/api/v1/devices"); len(l) != 3 {
		t.Fatalf("%d devices, want 3", len(l))
	}
	// With auto-merge off a new private address is a new device, grouped in the review.
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"auto_merge": false})
	a.report(tok, []netscan.Observation{{IP: "10.0.0.70", MAC: "46:73:59:45:f8:60", Hostname: "Mayas-iPhone", NameSource: "mdns"}})
	rev := a.ok(a.alice, "GET", "/api/v1/review", nil)
	groups := rev["groups"].([]any)
	if len(groups) != 1 {
		t.Fatalf("review groups = %v", groups)
	}
	g := groups[0].(map[string]any)
	if g["name"] != "Mayas-iPhone" || len(g["devices"].([]any)) != 2 {
		t.Fatalf("group = %v", g)
	}
	var fresh map[string]any
	for _, x := range g["devices"].([]any) {
		if m := x.(map[string]any); num(m, "id") != num(d, "id") {
			fresh = m
		}
	}
	m := a.ok(a.alice, "POST", fmt.Sprintf("/api/v1/devices/%d/merge", int64(num(d, "id"))), map[string]any{"ids": []int64{int64(num(fresh, "id"))}})
	if num(m, "id") != num(d, "id") {
		t.Fatalf("merge = %v", m)
	}
	if l := a.list(a.alice, "/api/v1/devices"); len(l) != 3 {
		t.Fatalf("%d devices after merge, want 3", len(l))
	}
}

// A Tailscale peer whose LAN address matches a scanned device is that device.
func TestTailscaleJoinsLANDevice(t *testing.T) {
	a := newApp(t, nil)
	ctx := context.Background()
	uid := a.ids["Alice"]
	scan := &store.Source{UserID: uid, Kind: "agent", Name: "LAN", Network: "Home", Enabled: true, Presence: true, IntervalS: 300}
	ts := &store.Source{UserID: uid, Kind: "tailscale", Name: "Tailscale", Network: "Tailscale", Enabled: true, Presence: true, IntervalS: 120}
	for _, src := range []*store.Source{scan, ts} {
		if err := a.st.CreateSource(ctx, src); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	if _, _, err := a.s.ingest(ctx, scan, "Home", []netscan.Observation{{IP: "192.168.1.161", MAC: "14:75:5b:2f:99:64"}}, now); err != nil {
		t.Fatal(err)
	}
	obs := []netscan.Observation{
		{IP: "100.65.31.101", TailscaleID: "niASCX", Hostname: "home-nixos", NameSource: "tailscale", OS: "Linux"},
		{IP: "192.168.1.161", TailscaleID: "niASCX", NameSource: "tailscale-lan"},
		{IP: "100.78.74.70", TailscaleID: "nj7aty", Hostname: "iphone-17-pro", NameSource: "tailscale", OS: "iOS", Offline: true, LastSeen: now.Add(-time.Hour)},
	}
	found, fresh, err := a.s.ingest(ctx, ts, "Tailscale", obs, now)
	if err != nil {
		t.Fatal(err)
	}
	if found != 1 || fresh != 1 {
		t.Fatalf("found %d new %d, want 1 and 1 (the nixos box joins the LAN device)", found, fresh)
	}
	devs, _ := a.st.ListDevices(ctx, uid, store.DeviceFilter{})
	if len(devs) != 2 {
		t.Fatalf("%d devices, want 2", len(devs))
	}
	for _, d := range devs {
		switch d.LastMAC {
		case "14:75:5b:2f:99:64":
			if d.OS != "Linux" || d.Name != "home-nixos" || d.Review != "known" || d.LastIP != "192.168.1.161" {
				t.Fatalf("joined device = %+v", d)
			}
		default:
			if d.Online || d.Review != "known" || d.KindGuess != "phone" {
				t.Fatalf("tailscale phone = %+v", d)
			}
		}
	}
}

// Devices go offline when nobody saw them for the offline time, but only when
// a working source would have seen them.
func TestOfflineNeedsAWorkingSource(t *testing.T) {
	a := newApp(t, nil)
	ctx := context.Background()
	uid := a.ids["Alice"]
	srcID, tok := a.agentFor(a.alice)
	a.report(tok, []netscan.Observation{{IP: "10.0.0.10", MAC: "00:11:32:aa:bb:cc", Hostname: "diskstation"}})
	devs, _ := a.st.ListDevices(ctx, uid, store.DeviceFilter{})
	d := devs[0]
	if !d.Watch {
		t.Fatalf("a NAS is watched from the start: %+v", d)
	}
	d.LastSeen = fmtTime(time.Now().Add(-time.Hour))
	_ = a.st.SaveDevice(ctx, &d)
	// the agent is failing: nobody knows, the NAS stays online
	src, _ := a.st.GetSource(ctx, uid, srcID)
	_ = a.st.RecordRun(ctx, src, store.Now(), 0, 0, 0, "the agent has not reported")
	a.s.checkOffline(ctx)
	if got, _ := a.st.GetDevice(ctx, uid, d.ID); !got.Online {
		t.Fatal("went offline although its source is failing")
	}
	_ = a.st.RecordRun(ctx, src, store.Now(), 1, 0, 0, "")
	a.s.checkOffline(ctx)
	if got, _ := a.st.GetDevice(ctx, uid, d.ID); got.Online {
		t.Fatal("still online after an hour unseen")
	}
	evs := a.list(a.alice, "/api/v1/events?kind=offline")
	if len(evs) != 1 {
		t.Fatalf("offline events = %v", evs)
	}
	// it comes back
	a.report(tok, []netscan.Observation{{IP: "10.0.0.10", MAC: "00:11:32:aa:bb:cc"}})
	if l := a.list(a.alice, "/api/v1/events?kind=online"); len(l) != 1 {
		t.Fatalf("online events = %v", l)
	}
}

const naxCSV = `"devMac","devName","devOwner","devType","devVendor","devFavorite","devGroup","devComments","devFirstConnection","devLastConnection","devLastIP","devIsNew","devLocation","devIsArchived","devAlertDown","devNameSource","devFQDN"
"e4:82:10:74:2d:7c","Huawei Router (LAN gateway)","Aleksandar","Router","HUAWEI TECHNOLOGIES CO.,LTD","1","","Main router","2026-08-13 11:36:14","2026-10-25 01:55:54","192.168.100.1","0","Hallway","0","1","NEWDEV","_gateway."
"66:42:4f:bf:66:9f","Bjelke's Apple Watch","","Other","(Unknown: locally administered)","0","","","2026-08-13 11:36:14","2026-08-17 08:26:10","192.168.100.145","1","","0","0","AVAHISCAN","None"
"9a:a8:4c:40:d8:f9","Bjelke's Apple Watch","","Other","(Unknown: locally administered)","0","","","2026-08-15 11:36:14","2026-08-15 14:30:15","192.168.100.147","1","","0","0","AVAHISCAN","None"
"12:2e:91:ad:74:38","(name not found)","","","(Unknown: locally administered)","0","","","2026-08-26 07:56:14","2026-08-26 07:56:14","192.168.100.152","1","","0","0","NEWDEV","None"
"internet","Internet (public WAN IP)","","internet","","0","","","2026-08-13 11:36:14","2026-10-25 01:55:54","31.223.131.138","0","","0","0","",""
`

func (a *app) upload(auth, path, name, content string) map[string]any {
	a.t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	fw, _ := mw.CreateFormFile("file", name)
	_, _ = fw.Write([]byte(content))
	mw.Close()
	req, _ := http.NewRequest("POST", a.url+path, &b)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+auth)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(res.Body).Decode(&m)
	if res.StatusCode != 200 {
		a.t.Fatalf("upload → %d %v", res.StatusCode, m)
	}
	return m
}

func TestNetAlertXImport(t *testing.T) {
	a := newApp(t, nil)
	m := a.upload(a.alice, "/api/v1/import/netalertx", "devices.csv", naxCSV)
	if num(m, "created") != 4 || num(m, "skipped") != 1 {
		t.Fatalf("first import = %v", m)
	}
	m = a.upload(a.alice, "/api/v1/import/netalertx", "devices.csv", naxCSV)
	if num(m, "created") != 0 || num(m, "updated") != 4 {
		t.Fatalf("second import = %v", m)
	}
	devs := a.list(a.alice, "/api/v1/devices")
	if len(devs) != 4 {
		t.Fatalf("%d devices", len(devs))
	}
	var router map[string]any
	for _, d := range devs {
		if m := d.(map[string]any); m["last_mac"] == "e4:82:10:74:2d:7c" {
			router = m
		}
	}
	if router["name"] != "Huawei Router (LAN gateway)" || router["review"] != "known" || router["kind"] != "router" || router["watch"] != true || router["pinned"] != true || router["notes"] != "Main router" || router["location"] != "Hallway" {
		t.Fatalf("router = %v", router)
	}
	rev := a.ok(a.alice, "GET", "/api/v1/review", nil)
	if num(rev, "total") != 3 || len(rev["groups"].([]any)) != 1 {
		t.Fatalf("review = %v", rev)
	}
	if l := a.list(a.bob, "/api/v1/devices"); len(l) != 0 {
		t.Fatal("bob sees the import")
	}
	// not a NetAlertX file
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	fw, _ := mw.CreateFormFile("file", "x.csv")
	_, _ = fw.Write([]byte("a,b\n1,2\n"))
	mw.Close()
	req, _ := http.NewRequest("POST", a.url+"/api/v1/import/netalertx", &b)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+a.alice)
	res, _ := http.DefaultClient.Do(req)
	if res.StatusCode != 400 {
		t.Fatalf("bad file → %d", res.StatusCode)
	}
}

func TestExportImportIdempotent(t *testing.T) {
	a := newApp(t, nil)
	a.upload(a.alice, "/api/v1/import/netalertx", "devices.csv", naxCSV)
	req, _ := http.NewRequest("GET", a.url+"/api/v1/export.json", nil)
	req.Header.Set("Authorization", "Bearer "+a.alice)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	_ = json.NewDecoder(res.Body).Decode(&doc)
	res.Body.Close()
	if len(doc["devices"].([]any)) != 4 {
		t.Fatalf("export = %v", doc)
	}
	m := a.ok(a.alice, "POST", "/api/v1/import", doc)
	if num(m, "created") != 0 || num(m, "updated") != 4 {
		t.Fatalf("import into the same account = %v", m)
	}
	m = a.ok(a.bob, "POST", "/api/v1/import", doc)
	if num(m, "created") != 4 {
		t.Fatalf("import into bob = %v", m)
	}
	m = a.ok(a.bob, "POST", "/api/v1/import", doc)
	if num(m, "created") != 0 {
		t.Fatalf("second import into bob = %v", m)
	}
	if l := a.list(a.alice, "/api/v1/devices"); len(l) != 4 {
		t.Fatalf("alice has %d devices", len(l))
	}
}

func TestSettingsArePersonal(t *testing.T) {
	a := newApp(t, nil)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"offline_min": 30, "notify_url": "https://ntfy.example/alice", "owners": []string{"Alice", "alice", " Kids "}, "quiet_from": "23:00", "quiet_to": "07:00"})
	alice := a.ok(a.alice, "GET", "/api/v1/settings", nil)
	bob := a.ok(a.bob, "GET", "/api/v1/settings", nil)
	if num(alice, "offline_min") != 30 || num(bob, "offline_min") != 15 || bob["notify_url"] != "" {
		t.Fatalf("alice %v / bob %v", alice["offline_min"], bob["offline_min"])
	}
	if o := alice["owners"].([]any); len(o) != 2 || o[1] != "Kids" {
		t.Fatalf("owners = %v", o)
	}
	a.expect(400, a.alice, "PUT", "/api/v1/settings", map[string]any{"offline_min": 0})
	a.expect(400, a.alice, "PUT", "/api/v1/settings", map[string]any{"quiet_from": "25:00"})
}

func TestQuietHours(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 10, 25, h, m, 0, 0, time.Local) }
	cases := []struct {
		from, to string
		t        time.Time
		want     bool
	}{
		{"23:00", "07:00", at(23, 30), true}, {"23:00", "07:00", at(6, 59), true}, {"23:00", "07:00", at(7, 0), false},
		{"13:00", "14:00", at(13, 15), true}, {"13:00", "14:00", at(12, 59), false}, {"", "", at(3, 0), false},
	}
	for _, c := range cases {
		if got := inQuiet(c.from, c.to, c.t); got != c.want {
			t.Errorf("inQuiet(%s, %s, %s) = %v", c.from, c.to, c.t.Format("15:04"), got)
		}
	}
}

func TestAlertWording(t *testing.T) {
	id := int64(7)
	one := alertMessage([]store.Event{{DeviceID: &id, Kind: "new", DeviceName: "Samsung phone", IP: "192.168.1.160"}}, "https://network.test")
	if one.Title != "New device: Samsung phone (192.168.1.160)" || one.Link != "https://network.test/devices/7" {
		t.Fatalf("one = %+v", one)
	}
	many := alertMessage([]store.Event{{Kind: "new", DeviceName: "A"}, {Kind: "offline", DeviceName: "NAS"}}, "https://network.test")
	if many.Title != "2 changes on your network" || many.Priority != "warning" || !strings.Contains(many.Body, "NAS went offline") {
		t.Fatalf("many = %+v", many)
	}
}

func TestLinkScopes(t *testing.T) {
	req := func(method, path string) *http.Request { return httptest.NewRequest(method, path, nil) }
	read := []string{"network:read"}
	write := []string{"network:write"}
	cases := []struct {
		scopes []string
		r      *http.Request
		want   bool
	}{
		{read, req("GET", "/api/v1/devices"), true},
		{read, req("GET", "/api/v1/devices/3"), true},
		{read, req("GET", "/api/v1/summary/2026-10-25"), true},
		{read, req("PUT", "/api/v1/devices/3"), false},
		{read, req("GET", "/api/v1/sources"), false},
		{read, req("GET", "/api/v1/export.json"), false},
		{write, req("PUT", "/api/v1/devices/3"), true},
		{write, req("POST", "/api/v1/devices"), true},
		{write, req("DELETE", "/api/v1/devices/3"), false},
		{write, req("POST", "/api/v1/devices/3/merge"), false},
		{nil, req("GET", "/api/v1/auth/me"), true},
		{nil, req("GET", "/api/v1/devices"), false},
	}
	for _, c := range cases {
		if got := linkAllows(c.scopes, c.r); got != c.want {
			t.Errorf("%v %s %s = %v, want %v", c.scopes, c.r.Method, c.r.URL.Path, got, c.want)
		}
	}
}

func TestDemoSeeds(t *testing.T) {
	a := newApp(t, func(c *config.Config) { c.DemoData = true })
	ov := a.ok(a.alice, "GET", "/api/v1/overview", nil)
	c := ov["counts"].(map[string]any)
	if num(c, "total") < 20 || num(c, "new") < 4 || num(c, "online") < 5 {
		t.Fatalf("demo counts = %v", c)
	}
	if len(ov["hours"].([]any)) != 24 {
		t.Fatalf("hours = %v", ov["hours"])
	}
	rev := a.ok(a.alice, "GET", "/api/v1/review", nil)
	if len(rev["groups"].([]any)) < 1 {
		t.Fatalf("the demo's rotating phone is not grouped: %v", rev)
	}
	sum := a.ok(a.alice, "GET", "/api/v1/summary/"+time.Now().AddDate(0, 0, -12).Format("2006-01-02"), nil)
	if num(sum["counts"].(map[string]any), "offline") < 1 {
		t.Fatalf("the NAS outage is not in the day: %v", sum)
	}
	if l := a.list(a.bob, "/api/v1/devices"); len(l) != 0 {
		t.Fatal("demo data leaked to bob")
	}
}

func TestPrivateTargetsOnly(t *testing.T) {
	for ip, want := range map[string]bool{"192.168.1.5": true, "10.0.0.1": true, "100.78.74.70": true, "8.8.8.8": false, "": false, "1.1.1.1": false} {
		if privateTarget(ip) != want {
			t.Errorf("privateTarget(%q) != %v", ip, want)
		}
	}
}
