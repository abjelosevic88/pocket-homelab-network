package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-network/internal/pocket"
)

// ---- a device's item in Pocket Inventory and server in Pocket Monitoring ----

func (s *Server) handleInventorySearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	conn, err := s.clients(ctx, actorID(ctx)).conn(ctx, appInventory, scopeItemsRead)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "items": []pocket.Item{}})
		return
	}
	items, err := conn.client.Items(ctx, strings.TrimSpace(r.URL.Query().Get("q")), 30)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "items": []pocket.Item{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items, "open_url": conn.public})
}

func (s *Server) handleMonitoringServers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	conn, err := s.clients(ctx, actorID(ctx)).conn(ctx, appMonitoring, scopeMonitoringRead)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "servers": []pocket.Server{}})
		return
	}
	list, err := conn.client.Servers(ctx)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "servers": []pocket.Server{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "servers": list, "open_url": conn.public})
}

// handleLinkDevice points a device at an item (which = inventory) or a server
// (which = monitoring); id 0 unlinks.
func (s *Server) handleLinkDevice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := decode(r, &in); err != nil || in.ID < 0 {
		writeErr(w, http.StatusBadRequest, "id is required (0 to unlink)")
		return
	}
	which := lastSegment(r.URL.Path)
	s.ingestMu.Lock()
	defer s.ingestMu.Unlock()
	d, err := s.store.GetDevice(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "device")
		return
	}
	name := strings.TrimSpace(in.Name)
	if in.ID == 0 {
		name = ""
	}
	switch which {
	case "inventory":
		d.InventoryID, d.InventoryName = in.ID, name
	case "monitoring":
		d.MonitoringID, d.MonitoringName = in.ID, name
	default:
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.store.SaveDevice(ctx, d); err != nil {
		s.fail(w, err, "save device")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func lastSegment(p string) string {
	p = strings.TrimRight(p, "/")
	return p[strings.LastIndex(p, "/")+1:]
}

// handleDeviceLinked fetches the linked item and server live, for the device page.
func (s *Server) handleDeviceLinked(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	d, err := s.store.GetDevice(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "device")
		return
	}
	cl := s.clients(ctx, uid)
	out := map[string]any{}
	status := func(a pocketApp) map[string]any {
		mode, via, detail := s.connStatus(ctx, cl.u, cl.st, a)
		return map[string]any{"mode": mode, "connected": via != "", "detail": detail}
	}
	inv := status(appInventory)
	if d.InventoryID > 0 {
		if conn, err := cl.conn(ctx, appInventory, scopeItemsRead); err != nil {
			inv["error"] = err.Error()
		} else if it, err := conn.client.Item(ctx, d.InventoryID); err != nil {
			inv["error"] = err.Error()
		} else {
			inv["item"] = it
			inv["open_url"] = fmt.Sprintf("%s/items/%d", strings.TrimRight(conn.public, "/"), it.ID)
			if it.Name != "" && it.Name != d.InventoryName {
				s.renameLink(r, uid, d.ID, "inventory", it.Name)
			}
		}
	}
	out["inventory"] = inv
	mon := status(appMonitoring)
	if d.MonitoringID > 0 {
		if conn, err := cl.conn(ctx, appMonitoring, scopeMonitoringRead); err != nil {
			mon["error"] = err.Error()
		} else if sv, err := conn.client.Server(ctx, d.MonitoringID); err != nil {
			mon["error"] = err.Error()
		} else {
			mon["server"] = sv
			mon["open_url"] = fmt.Sprintf("%s/server/%d", strings.TrimRight(conn.public, "/"), sv.ID)
			if sv.Name != "" && sv.Name != d.MonitoringName {
				s.renameLink(r, uid, d.ID, "monitoring", sv.Name)
			}
		}
	}
	out["monitoring"] = mon
	writeJSON(w, http.StatusOK, out)
}

// renameLink keeps the remembered name of a linked item or server current.
func (s *Server) renameLink(r *http.Request, uid, id int64, which, name string) {
	s.ingestMu.Lock()
	defer s.ingestMu.Unlock()
	d, err := s.store.GetDevice(r.Context(), uid, id)
	if err != nil {
		return
	}
	if which == "inventory" {
		d.InventoryName = name
	} else {
		d.MonitoringName = name
	}
	_ = s.store.SaveDevice(r.Context(), d)
}
