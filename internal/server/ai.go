package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-network/internal/ai"
	"github.com/abjelosevic88/pocket-homelab-network/internal/netscan"
	"github.com/abjelosevic88/pocket-homelab-network/internal/store"
)

// aiFor is the person's AI: Pocket AI through Core, their own
// OpenAI-compatible key, or nil with why.
func (s *Server) aiFor(ctx context.Context, uid int64, st store.Settings) (*ai.Client, string, string) {
	email := ""
	if u, err := s.store.GetUser(ctx, uid); err == nil {
		email = u.Email
	}
	pocket, status := s.core.SelectAI(ctx, email, st.AIProvider, st.OpenAIKey != "")
	if pocket != nil {
		if !status.Ready {
			return nil, "", "Pocket AI: " + status.Detail
		}
		c := ai.New("", "", "", 150*time.Second)
		c.Pocket = pocket.WithModel(st.AIPocketModel)
		return c, "pocket", ""
	}
	if st.OpenAIKey == "" {
		return nil, "", status.Detail
	}
	return ai.New(st.OpenAIBaseURL, st.OpenAIKey, st.OpenAIModel, 150*time.Second), "openai", ""
}

func (s *Server) handleAIStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, _ := s.store.GetSettings(ctx, actorID(ctx))
	cl, used, why := s.aiFor(ctx, actorID(ctx), st)
	writeJSON(w, http.StatusOK, map[string]any{"ready": cl != nil, "provider": used, "detail": why})
}

func (s *Server) handleAIModels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c, status := s.core.SelectAI(ctx, userFrom(ctx).Email, "pocket", false)
	if c == nil || !status.Ready {
		writeErr(w, http.StatusServiceUnavailable, status.Detail)
		return
	}
	models, err := c.Models(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models)
}

func validAIProvider(v string) bool { return core.ValidAIProvider(v) }

func validPocketModel(v string) bool { return core.ValidInferenceModel(v) }

const aiVoice = `You help a non-technical person understand the devices on their home network. You see what the network knows about each device: its name, the name it gave itself, the maker of its network card, whether its address is private (phones, watches and laptops make these up, so no maker is known), its model and system when known, which services it answers on, how much it uses DNS, and when it was seen. Use only the data given; never invent devices or numbers. Say plainly when something cannot be known. No jargon without a short explanation, no emojis, no exclamation marks.`

// networkDigest renders a person's devices as compact text for the model.
func (s *Server) networkDigest(ctx context.Context, uid int64) string {
	devs, err := s.store.ListDevices(ctx, uid, store.DeviceFilter{})
	if err != nil || len(devs) == 0 {
		return ""
	}
	var b strings.Builder
	c := s.store.CountDevices(ctx, uid)
	fmt.Fprintf(&b, "%d devices, %d online now, %d not reviewed yet, %d watched (%d of them offline).\n", c.Total, c.Online, c.New, c.Watched, c.WatchedOffline)
	b.WriteString("Devices (name | kind | owner | place | online | last seen | address | maker | private address | network | review):\n")
	for i, d := range devs {
		if i == 300 {
			fmt.Fprintf(&b, "… and %d more\n", len(devs)-300)
			break
		}
		on := "offline"
		if d.Online {
			on = "online"
		}
		fmt.Fprintf(&b, "%s | %s | %s | %s | %s | %s | %s | %s | %v | %s | %s\n", d.Label(), firstNonEmpty(netscan.KindName[kindOf(d)], "?"), d.Owner, d.Location, on,
			shortTime(d.LastSeen), d.LastIP, firstNonEmpty(d.Vendor, "?"), d.RandomMAC, d.LastNetwork, d.Review)
	}
	return b.String()
}

func shortTime(v string) string {
	t := parseTime(v)
	if t.IsZero() {
		return "never"
	}
	return t.Local().Format("2 Jan 15:04")
}

// handleAsk answers a question about the person's network.
func (s *Server) handleAsk(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	cl, used, why := s.aiFor(ctx, uid, st)
	if cl == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": why})
		return
	}
	var in struct {
		Question string `json:"question"`
	}
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.Question) == "" {
		writeErr(w, http.StatusBadRequest, "question is required")
		return
	}
	digest := s.networkDigest(ctx, uid)
	if digest == "" {
		digest = "No devices found yet.\n"
	}
	evs, _ := s.store.ListEvents(ctx, uid, store.EventFilter{Limit: 60})
	var act strings.Builder
	for _, e := range evs {
		fmt.Fprintf(&act, "%s %s: %s\n", shortTime(e.At), e.DeviceName, e.Text)
	}
	prompt := "Question: " + strings.TrimSpace(in.Question) + "\n\nTheir network:\n" + digest + "\nRecent activity:\n" + act.String() +
		"\nAnswer in a few sentences, with the data that backs it. If the data cannot answer it, say so."
	text, err := cl.Complete(ctx, aiVoice, prompt, false)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": used, "text": strings.TrimSpace(text)})
}

// handleIdentify guesses what an unknown device is and suggests a name and kind.
func (s *Server) handleIdentify(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	d, err := s.store.GetDevice(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "device")
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	cl, used, why := s.aiFor(ctx, uid, st)
	if cl == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": why})
		return
	}
	ids, _ := s.store.DeviceIDs(ctx, uid, d.ID)
	pres, _ := s.store.Presence(ctx, uid, d.ID, store.HourKey(time.Now().AddDate(0, 0, -13)))
	hours := map[int]int{}
	for _, p := range pres {
		if t, err := time.ParseInLocation("2006-01-02T15", p.Hour, time.Local); err == nil {
			hours[t.Hour()] += p.Minutes
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Name given: %q\nName it gave itself: %q\nMaker of its network card: %q\nPrivate (made-up) address: %v\nModel: %q\nSystem: %q\n", d.Name, d.Hostname, d.Vendor, d.RandomMAC, d.Model, d.OS)
	fmt.Fprintf(&b, "Address: %s on %s\nFirst seen %s, last seen %s, online now %v\nDNS queries in the last day: %d (blocked %d)\n", d.LastIP, d.LastNetwork, shortTime(d.FirstSeen), shortTime(d.LastSeen), d.Online, d.DNSQueries, d.DNSBlocked)
	if d.Ports != "" {
		fmt.Fprintf(&b, "Services it answers on: %s\n", d.Ports)
	}
	for _, id := range ids {
		fmt.Fprintf(&b, "Identifier %s %s (maker %q)\n", id.Kind, id.Value, netscan.Vendor(id.Value))
	}
	if len(hours) > 0 {
		b.WriteString("Minutes online by hour of day over two weeks:")
		for h := 0; h < 24; h++ {
			if hours[h] > 0 {
				fmt.Fprintf(&b, " %02d:%d", h, hours[h])
			}
		}
		b.WriteString("\n")
	}
	var kinds []string
	for _, k := range netscan.Kinds {
		kinds = append(kinds, k)
	}
	prompt := "What is this device most likely? Reply as JSON: {\"kind\": one of " + strings.Join(kinds, ", ") + ", \"name\": a short plain name like \"Samsung phone\" or \"Living room TV\", \"confidence\": \"low\"|\"medium\"|\"high\", \"why\": one or two plain sentences, \"check\": one concrete thing the person can do to be sure}.\n\n" + b.String()
	text, err := cl.Complete(ctx, aiVoice, prompt, true)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	var out struct {
		Kind       string `json:"kind"`
		Name       string `json:"name"`
		Confidence string `json:"confidence"`
		Why        string `json:"why"`
		Check      string `json:"check"`
	}
	if err := json.Unmarshal([]byte(extractJSON(text)), &out); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": used, "why": strings.TrimSpace(text)})
		return
	}
	if !netscan.ValidKind(out.Kind) {
		out.Kind = ""
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": used, "kind": out.Kind, "name": out.Name, "confidence": out.Confidence, "why": out.Why, "check": out.Check})
}

func extractJSON(t string) string {
	a, b := strings.Index(t, "{"), strings.LastIndex(t, "}")
	if a < 0 || b <= a {
		return t
	}
	return t[a : b+1]
}
