package netscan

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

var errUnsupported = errors.New("raw ARP is only used on Linux")

// tableSweep works without raw sockets: a tiny UDP packet to every address
// makes the operating system resolve it with ARP; the answers then sit in
// the system's ARP table.
func tableSweep(ctx context.Context, ifi *net.Interface, self net.IP, subnet *net.IPNet) (map[string]string, error) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 64)
	for _, ip := range hostsOf(subnet) {
		if ip.Equal(self) || ctx.Err() != nil {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(ip net.IP) {
			defer wg.Done()
			defer func() { <-sem }()
			c, err := net.DialTimeout("udp4", net.JoinHostPort(ip.String(), "9"), 300*time.Millisecond)
			if err == nil {
				_, _ = c.Write([]byte{0})
				c.Close()
			}
		}(ip)
	}
	wg.Wait()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(2 * time.Second):
	}
	all, err := ARPTable()
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for ip, e := range all {
		if p := net.ParseIP(ip); p != nil && subnet.Contains(p) && (e.Iface == "" || e.Iface == ifi.Name) {
			out[ip] = e.MAC
		}
	}
	return out, nil
}

// ARPEntry is one line of the system's ARP table.
type ARPEntry struct {
	MAC   string
	Iface string
}

var arpLine = regexp.MustCompile(`\(?(\d+\.\d+\.\d+\.\d+)\)?\s+(?:at\s+)?([0-9a-fA-F]{1,2}(?:[:-][0-9a-fA-F]{1,2}){5})(?:.*\son\s(\S+))?`)

// ARPTable reads the system's ARP table (/proc/net/arp on Linux, `arp -an` elsewhere).
func ARPTable() (map[string]ARPEntry, error) {
	out := map[string]ARPEntry{}
	if runtime.GOOS == "linux" {
		f, err := os.Open("/proc/net/arp")
		if err != nil {
			return nil, err
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Scan() // header
		for sc.Scan() {
			fs := strings.Fields(sc.Text())
			// IP address, HW type, Flags, HW address, Mask, Device
			if len(fs) < 6 || fs[2] == "0x0" || fs[3] == "00:00:00:00:00:00" {
				continue
			}
			out[fs[0]] = ARPEntry{MAC: NormalizeMAC(fs[3]), Iface: fs[5]}
		}
		return out, sc.Err()
	}
	b, err := exec.Command("arp", "-an").Output()
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		m := arpLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if mac := NormalizeMAC(padMAC(m[2])); mac != "" && mac != "ff:ff:ff:ff:ff:ff" {
			out[m[1]] = ARPEntry{MAC: mac, Iface: m[3]}
		}
	}
	return out, nil
}

// padMAC turns macOS's "a:b:c:d:e:f" into "0a:0b:…".
func padMAC(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == ':' || r == '-' })
	for i, p := range parts {
		if len(p) == 1 {
			parts[i] = "0" + p
		}
	}
	return strings.Join(parts, ":")
}
