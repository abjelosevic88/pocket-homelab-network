package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-network/internal/config"
)

// newMoveApp is newApp behind a Pocket Core that vouches for Pocket Move's
// link tokens: pcx_<alice|bob>_<in|out|read> (move:import / move:export / another scope).
func newMoveApp(t *testing.T) *app {
	t.Helper()
	coreSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/auth/introspect" {
			io.WriteString(w, `{}`)
			return
		}
		var in struct{ Token string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		parts := strings.Split(in.Token, "_")
		if len(parts) != 3 || parts[0] != "pcx" {
			io.WriteString(w, `{"active":false}`)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"active": true, "kind": "delegated", "audience": "network",
			"scope": map[string]string{"in": core.ScopeMoveImport, "out": core.ScopeMoveExport, "read": "devices:read"}[parts[2]],
			"user":  map[string]any{"id": 1, "sub": parts[1] + "@example.com", "email": parts[1] + "@example.com", "name": parts[1], "role": "member"},
			"app":   map[string]any{"id": 9, "slug": "move", "name": "Pocket Move"}})
	}))
	t.Cleanup(coreSrv.Close)
	return newApp(t, func(c *config.Config) { c.CoreURL = coreSrv.URL })
}

func (a *app) raw(auth, method, path, ctype string, body []byte) (int, []byte) {
	a.t.Helper()
	req, _ := http.NewRequest(method, a.url+path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+auth)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

func TestMoveLinkScopes(t *testing.T) {
	a := newMoveApp(t)
	out, in, read := "pcx_bob_out", "pcx_bob_in", "pcx_bob_read"
	for _, c := range []struct {
		tok, method, path string
		want              int
	}{
		{out, "GET", core.PortablePath, 200},
		{out, "GET", "/api/v1/export.json", 200},
		{out, "GET", "/api/v1/export.csv", 200},
		{out, "POST", "/api/v1/import", 403},
		{out, "POST", "/api/v1/import/netalertx", 403},
		{out, "GET", "/api/v1/devices", 403},
		{out, "GET", "/api/v1/settings", 403},
		{out, "GET", "/api/v1/backup", 403},
		{in, "GET", core.PortablePath, 200},
		{in, "GET", "/api/v1/export.json", 403},
		{in, "POST", "/api/v1/devices", 403},
		{in, "POST", "/api/v1/sources", 403},
		{in, "PUT", "/api/v1/settings", 403},
		{read, "GET", core.PortablePath, 403},
		{read, "GET", "/api/v1/export.json", 403},
	} {
		if code, body := a.raw(c.tok, c.method, c.path, "application/json", []byte("{}")); code != c.want {
			t.Errorf("%s %s %s → %d, want %d (%s)", c.tok, c.method, c.path, code, c.want, body)
		}
	}
	_, body := a.raw(out, "GET", core.PortablePath, "", nil)
	var p core.Portable
	if err := json.Unmarshal(body, &p); err != nil || len(p.Exports) != 2 || len(p.Imports) != 2 || p.Imports[1].Form != "file" {
		t.Fatalf("portable: %s", body)
	}
}

// Move brings Alice's devices to Bob: each import adds once (a device kept
// by hand, without a MAC, too), and nothing crosses between them.
func TestMoveImportsAddOnce(t *testing.T) {
	a := newMoveApp(t)
	a.expect(201, a.alice, "POST", "/api/v1/devices", map[string]any{"name": "Alice's printer", "mac": "30:05:5c:00:00:01", "kind": "printer"})
	a.expect(201, a.alice, "POST", "/api/v1/devices", map[string]any{"name": "Garden lamp", "kind": "other", "notes": "no network card"})

	code, dump := a.raw("pcx_alice_out", "GET", "/api/v1/export.json", "", nil)
	if code != 200 || !strings.Contains(string(dump), "Garden lamp") {
		t.Fatalf("alice's export: %d %s", code, dump)
	}
	if _, b := a.raw("pcx_bob_out", "GET", "/api/v1/export.json", "", nil); strings.Contains(string(b), "printer") {
		t.Fatalf("bob's export holds alice's devices: %s", b)
	}
	for run, want := range []float64{2, 0} {
		code, body := a.raw("pcx_bob_in", "POST", "/api/v1/import", "application/json", dump)
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		if code != 200 || num(m, "created") != want || (run == 1 && num(m, "unchanged") != 2) {
			t.Fatalf("pocket run %d: %d %s", run+1, code, body)
		}
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "devices.csv")
	_, _ = fw.Write([]byte(naxCSV))
	_ = mw.Close()
	for run, want := range []float64{4, 0} {
		code, body := a.raw("pcx_bob_in", "POST", "/api/v1/import/netalertx", mw.FormDataContentType(), buf.Bytes())
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		if code != 200 || num(m, "created") != want {
			t.Fatalf("netalertx run %d: %d %s", run+1, code, body)
		}
	}
	if l := a.list(a.bob, "/api/v1/devices"); len(l) != 6 {
		t.Fatalf("bob has %d devices", len(l))
	}
	if l := a.list(a.alice, "/api/v1/devices"); len(l) != 2 {
		t.Fatalf("alice has %d devices", len(l))
	}
}
