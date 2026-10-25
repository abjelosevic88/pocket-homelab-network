package netscan

import (
	"context"
	"encoding/binary"
	"math/rand/v2"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// ResolveNames fills in hostnames (and a model where mDNS tells it) for the
// hosts: mDNS first (phones, Macs, printers, smart speakers say their own
// name), then NetBIOS (Windows, Samba), then reverse DNS (the router's or
// DNS server's name for the address).
func ResolveNames(ctx context.Context, hosts []Observation, dnsServer string) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 32)
	res := resolver(dnsServer)
	for i := range hosts {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(h *Observation) {
			defer wg.Done()
			defer func() { <-sem }()
			if name := MDNSName(ctx, h.IP); name != "" {
				h.Hostname, h.NameSource = name, "mdns"
				if m := MDNSModel(ctx, h.IP, name); m != "" && !fakeAppleModel(m, h.Vendor) {
					h.Model = m
				}
				return
			}
			if name := NetBIOSName(ctx, h.IP); name != "" {
				h.Hostname, h.NameSource = name, "netbios"
				return
			}
			if name := ReverseDNS(ctx, res, h.IP); name != "" {
				h.Hostname, h.NameSource = name, "rdns"
			}
		}(&hosts[i])
	}
	wg.Wait()
}

func resolver(server string) *net.Resolver {
	if server == "" {
		return &net.Resolver{PreferGo: true}
	}
	if _, _, err := net.SplitHostPort(server); err != nil {
		server = net.JoinHostPort(server, "53")
	}
	return &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, server)
	}}
}

// CleanName trims what DNS and mDNS add to a device's own name.
func CleanName(n string) string {
	n = strings.TrimSuffix(strings.TrimSpace(n), ".")
	for _, suf := range []string{".local", ".lan", ".home", ".localdomain", ".home.arpa", ".fritz.box", ".station", ".router", ".gateway", ".internal"} {
		if strings.HasSuffix(strings.ToLower(n), suf) {
			n = n[:len(n)-len(suf)]
		}
	}
	switch strings.ToLower(n) {
	case "", "localhost", "_gateway", "gateway", "unknown", "(none)", "<unknown>", "(unknown)", "(name not found)", "name not found", "null", "none":
		return ""
	}
	return n
}

// ReverseDNS asks DNS for the name of an address.
func ReverseDNS(ctx context.Context, r *net.Resolver, ip string) string {
	c, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
	defer cancel()
	names, err := r.LookupAddr(c, ip)
	if err != nil || len(names) == 0 {
		return ""
	}
	return CleanName(names[0])
}

func reverseName(ip string) string {
	p := net.ParseIP(ip).To4()
	if p == nil {
		return ""
	}
	return strings.Join([]string{itoa(p[3]), itoa(p[2]), itoa(p[1]), itoa(p[0]), "in-addr", "arpa", ""}, ".")
}

func itoa(b byte) string {
	if b == 0 {
		return "0"
	}
	var s []byte
	for b > 0 {
		s = append([]byte{'0' + b%10}, s...)
		b /= 10
	}
	return string(s)
}

// mdnsQuery sends one unicast ("legacy") mDNS question straight to the device
// on port 5353; mDNSResponder and Avahi answer to the asking port.
func mdnsQuery(ctx context.Context, ip, name string, qt dnsmessage.Type) []dnsmessage.Resource {
	n, err := dnsmessage.NewName(name)
	if err != nil {
		return nil
	}
	msg := dnsmessage.Message{Header: dnsmessage.Header{ID: uint16(rand.IntN(65535))}, Questions: []dnsmessage.Question{{Name: n, Type: qt, Class: dnsmessage.ClassINET}}}
	pkt, err := msg.Pack()
	if err != nil {
		return nil
	}
	return udpAsk(ctx, ip, 5353, pkt, func(b []byte) []dnsmessage.Resource {
		var m dnsmessage.Message
		if m.Unpack(b) != nil || !m.Response {
			return nil
		}
		return append(m.Answers, m.Additionals...)
	})
}

func udpAsk[T any](ctx context.Context, ip string, port int, pkt []byte, parse func([]byte) T) T {
	var zero T
	c, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return zero
	}
	defer c.Close()
	deadline := time.Now().Add(900 * time.Millisecond)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = c.SetDeadline(deadline)
	addr := &net.UDPAddr{IP: net.ParseIP(ip), Port: port}
	if _, err := c.WriteToUDP(pkt, addr); err != nil {
		return zero
	}
	buf := make([]byte, 4096)
	for {
		n, from, err := c.ReadFromUDP(buf)
		if err != nil {
			return zero
		}
		if !from.IP.Equal(addr.IP) {
			continue
		}
		return parse(buf[:n])
	}
}

// MDNSName asks a device over mDNS for its own name.
func MDNSName(ctx context.Context, ip string) string {
	rev := reverseName(ip)
	if rev == "" {
		return ""
	}
	for _, rr := range mdnsQuery(ctx, ip, rev, dnsmessage.TypePTR) {
		if p, ok := rr.Body.(*dnsmessage.PTRResource); ok {
			if n := CleanName(p.PTR.String()); n != "" {
				return n
			}
		}
	}
	return ""
}

// MDNSModel asks an Apple-style device for its model (_device-info TXT "model=").
func MDNSModel(ctx context.Context, ip, host string) string {
	for _, rr := range mdnsQuery(ctx, ip, host+"._device-info._tcp.local.", dnsmessage.TypeTXT) {
		if t, ok := rr.Body.(*dnsmessage.TXTResource); ok {
			for _, kv := range t.TXT {
				if v, ok := strings.CutPrefix(kv, "model="); ok {
					v, _, _ = strings.Cut(v, "@")
					return v
				}
			}
		}
	}
	return ""
}

// NetBIOSName asks a Windows or Samba machine for its name (node status, UDP 137).
func NetBIOSName(ctx context.Context, ip string) string {
	pkt := make([]byte, 50)
	binary.BigEndian.PutUint16(pkt[0:2], uint16(rand.IntN(65535)))
	binary.BigEndian.PutUint16(pkt[4:6], 1) // one question
	pkt[12] = 0x20
	// the wildcard name "*" padded with NULs, first-level encoded
	name := append([]byte{'*'}, make([]byte, 15)...)
	for i, b := range name {
		pkt[13+2*i] = 'A' + b>>4
		pkt[14+2*i] = 'A' + b&0x0f
	}
	pkt[45] = 0
	binary.BigEndian.PutUint16(pkt[46:48], 0x21) // NBSTAT
	binary.BigEndian.PutUint16(pkt[48:50], 1)    // IN
	return udpAsk(ctx, ip, 137, pkt, func(b []byte) string {
		// header 12, name 34, type 2, class 2, ttl 4, rdlength 2, count 1
		if len(b) < 57 {
			return ""
		}
		n := int(b[56])
		off := 57
		for i := 0; i < n && off+18 <= len(b); i++ {
			nm := strings.TrimRight(string(b[off:off+15]), " \x00")
			suffix := b[off+15]
			flags := binary.BigEndian.Uint16(b[off+16 : off+18])
			off += 18
			if suffix == 0x00 && flags&0x8000 == 0 && nm != "" {
				return nm
			}
		}
		return ""
	})
}

// fakeAppleModel: Samba (TrueNAS, Synology, a Linux box) advertises a Mac
// model so Time Machine shows a nice icon; on a non-Apple MAC it says nothing.
func fakeAppleModel(model, vendor string) bool {
	if vendor == "" || strings.Contains(strings.ToLower(vendor), "apple") {
		return false
	}
	for _, p := range []string{"Mac", "iMac", "Xserve", "RackMac", "TimeCapsule", "AirPort", "PowerMac"} {
		if strings.HasPrefix(model, p) {
			return true
		}
	}
	return false
}
