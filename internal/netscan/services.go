package netscan

import (
	"context"
	"errors"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Service is something a device answers on.
type Service struct {
	Port  int    `json:"port"`
	Name  string `json:"name"`
	Web   string `json:"web,omitempty"` // http | https: it has a page to open
	Notes string `json:"notes,omitempty"`
}

var commonPorts = []Service{
	{Port: 22, Name: "Remote login (SSH)"}, {Port: 23, Name: "Telnet (old, unencrypted)", Notes: "worth switching off"},
	{Port: 53, Name: "DNS"}, {Port: 80, Name: "Web page", Web: "http"}, {Port: 443, Name: "Web page (secure)", Web: "https"},
	{Port: 139, Name: "Windows file sharing (old)"}, {Port: 445, Name: "File sharing (SMB)"}, {Port: 548, Name: "Apple file sharing"},
	{Port: 554, Name: "Camera video stream (RTSP)"}, {Port: 631, Name: "Printing (IPP)"}, {Port: 9100, Name: "Printing (raw)"},
	{Port: 1883, Name: "Smart home messages (MQTT)"}, {Port: 3389, Name: "Remote desktop (Windows)"}, {Port: 5900, Name: "Screen sharing (VNC)"},
	{Port: 5000, Name: "App or NAS page", Web: "http"}, {Port: 5001, Name: "NAS page (secure)", Web: "https"}, {Port: 8006, Name: "Proxmox", Web: "https"},
	{Port: 8008, Name: "Chromecast / Google cast"}, {Port: 8009, Name: "Google cast"}, {Port: 8080, Name: "Web page (alternative)", Web: "http"},
	{Port: 8443, Name: "Web page (alternative, secure)", Web: "https"}, {Port: 8123, Name: "Home Assistant", Web: "http"},
	{Port: 32400, Name: "Plex", Web: "http"}, {Port: 8096, Name: "Jellyfin", Web: "http"}, {Port: 62078, Name: "iPhone or iPad sync"},
	{Port: 7000, Name: "AirPlay"}, {Port: 3000, Name: "Web app", Web: "http"}, {Port: 9000, Name: "Web app", Web: "http"},
}

// CheckServices tries the common ports of an address (a plain TCP connect,
// nothing more) and lists the ones that answer.
func CheckServices(ctx context.Context, ip string) ([]Service, error) {
	if p := net.ParseIP(ip); p == nil || p.To4() == nil {
		return nil, errors.New("not an IPv4 address")
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	out := []Service{}
	sem := make(chan struct{}, 16)
	for _, svc := range commonPorts {
		wg.Add(1)
		sem <- struct{}{}
		go func(svc Service) {
			defer wg.Done()
			defer func() { <-sem }()
			d := net.Dialer{Timeout: 700 * time.Millisecond}
			c, err := d.DialContext(ctx, "tcp4", net.JoinHostPort(ip, strconv.Itoa(svc.Port)))
			if err != nil {
				return
			}
			c.Close()
			mu.Lock()
			out = append(out, svc)
			mu.Unlock()
		}(svc)
	}
	wg.Wait()
	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out, ctx.Err()
}

// WakeOnLAN sends the magic packet for a MAC to the broadcast addresses
// (the subnet's own, when given, and 255.255.255.255) on UDP 9.
func WakeOnLAN(mac, subnet string) error {
	hw, err := net.ParseMAC(mac)
	if err != nil || len(hw) != 6 {
		return errors.New("not a MAC address")
	}
	pkt := make([]byte, 0, 102)
	pkt = append(pkt, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff)
	for i := 0; i < 16; i++ {
		pkt = append(pkt, hw...)
	}
	targets := []string{"255.255.255.255"}
	if _, n, err := net.ParseCIDR(subnet); err == nil && n.IP.To4() != nil {
		b := make(net.IP, 4)
		for i := range b {
			b[i] = n.IP.To4()[i] | ^n.Mask[i]
		}
		targets = append([]string{b.String()}, targets...)
	}
	var last error
	sent := 0
	for _, t := range targets {
		c, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.ParseIP(t), Port: 9})
		if err != nil {
			last = err
			continue
		}
		if _, err := c.Write(pkt); err != nil {
			last = err
		} else {
			sent++
		}
		c.Close()
	}
	if sent == 0 {
		return last
	}
	return nil
}
