package server

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-network/internal/store"
)

// Pocket Core app links (docs/links.md in pocket-homelab-core), Network as the
// target: another Pocket app the person allowed in Core may act here for
// them with a pcx_ token, only within these scopes. Every route not listed
// in linkRoutes stays closed to it (403).

var linkOffers = []core.Offer{
	{Scope: "network:read", Name: "See your network", Description: "Read your devices (names, kind, owner, whether they are online, when they were last seen), what happened on the network and who is home — for your day, dashboards and questions. Never your source passwords."},
	{Scope: "network:write", Name: "Name and describe devices", Description: "Add devices and change their name, owner, kind, place and notes. It may not change reviews or alerts of devices it did not add."},
}

// portable is what Pocket Move may copy out and bring in for a person
// (docs/move.md in pocket-homelab-core); Register adds move:export/import.
var portable = &core.Portable{
	Exports: []core.PortableExport{
		{Path: "/api/v1/export.json", File: "network.json", Kind: core.KindPocket, Title: "Your devices with their names, owners, places, notes and addresses, and where they are found (no passwords)"},
		{Path: "/api/v1/export.csv", File: "devices.csv", Kind: core.KindCSV, Title: "Your devices as a table"},
	},
	Imports: []core.PortableImport{
		{Path: "/api/v1/import", Kind: core.KindPocket, ContentType: "application/json", MaxBytes: 32 << 20, Title: "Adds the devices from another box; a device already here (same MAC, or same name for one kept by hand) only gets the details it was missing"},
		{Path: "/api/v1/import/netalertx", Kind: "netalertx-csv", Form: "file", MaxBytes: 16 << 20, Title: "Adds the devices of a NetAlertX devices.csv; devices already here only get the details they were missing"},
	},
}

type linkRoute struct {
	methods string
	path    *regexp.Regexp
	scope   string // "" = any linked app
}

var linkRoutes = []linkRoute{
	{"GET", regexp.MustCompile(`^/api/v1/auth/me$`), ""},
	{"GET", regexp.MustCompile(`^/api/v1/(overview|dashboard|devices|events|review|devices/meta)$`), "network:read"},
	{"GET", regexp.MustCompile(`^/api/v1/devices/\d+$`), "network:read"},
	{"GET", regexp.MustCompile(`^/api/v1/summary/\d{4}-\d{2}-\d{2}$`), "network:read"},
	{"POST", regexp.MustCompile(`^/api/v1/devices$`), "network:write"},
	{"PUT", regexp.MustCompile(`^/api/v1/devices/\d+$`), "network:write"},
}

var errLinkScope = errors.New("outside the link's scope")

// linkAllows reports whether a link token with these scopes may make this request.
func linkAllows(scopes []string, r *http.Request) bool {
	if portable.LinkAllows(scopes, r) {
		return true
	}
	for _, lr := range linkRoutes {
		if !strings.Contains(" "+lr.methods+" ", " "+r.Method+" ") || !lr.path.MatchString(r.URL.Path) {
			continue
		}
		if lr.scope == "" {
			return true
		}
		for _, sc := range scopes {
			if sc == lr.scope {
				return true
			}
		}
	}
	return false
}

// linkUser validates a pcx_ token, checks the route against its scopes and
// maps it to the person it acts for. The second value is the acting app's slug.
func (s *Server) linkUser(ctx context.Context, tok string, r *http.Request) (*store.User, string, error) {
	in, err := s.core.Introspect(ctx, tok)
	if err != nil {
		return nil, "", err
	}
	if !in.Active || !in.Delegated() || in.User == nil || in.User.Email == "" || in.App == nil || in.App.Slug == "" {
		return nil, "", http.ErrNoCookie
	}
	if !linkAllows(strings.Fields(in.Scope), r) {
		return nil, "", errLinkScope
	}
	u, err := s.coreIdentityUser(ctx, in)
	return u, in.App.Slug, err
}

const linkAppKey ctxKey = 2

// linkApp is the acting app's slug when the request came in on a link token.
func linkApp(ctx context.Context) string {
	a, _ := ctx.Value(linkAppKey).(string)
	return a
}
