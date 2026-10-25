// Package netscan finds devices on a local network in pure Go: an ARP sweep
// (raw sockets on Linux; the system ARP table elsewhere or without the
// permission), names from reverse DNS, mDNS and NetBIOS, the vendor from the
// MAC, a guess at what kind of device it is, and on demand the services it
// offers and Wake-on-LAN.
package netscan

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"
)

// Observation is one device seen by a source, in the same shape whichever
// source saw it (a scan here, an agent elsewhere, Tailscale, AdGuard, a router).
type Observation struct {
	MAC         string `json:"mac,omitempty"`
	IP          string `json:"ip,omitempty"`
	Hostname    string `json:"hostname,omitempty"`
	NameSource  string `json:"name_source,omitempty"` // rdns | mdns | netbios | dhcp | tailscale | adguard | router
	Vendor      string `json:"vendor,omitempty"`
	Model       string `json:"model,omitempty"`
	OS          string `json:"os,omitempty"`
	TailscaleID string `json:"tailscale_id,omitempty"`
	ClientName  string `json:"client_name,omitempty"` // AdGuard persistent client name
	// Offline marks a device the source knows about but that is not there now
	// (an offline Tailscale peer); LastSeen is when the source last saw it.
	Offline    bool      `json:"offline,omitempty"`
	LastSeen   time.Time `json:"last_seen,omitempty"`
	DNSQueries int       `json:"dns_queries,omitempty"`
	DNSBlocked int       `json:"dns_blocked,omitempty"`
	Self       bool      `json:"self,omitempty"` // the machine doing the scan
}

// Interface is a network interface a scan can sweep.
type Interface struct {
	Name   string `json:"name"`
	IP     string `json:"ip"`
	Subnet string `json:"subnet"`
	MAC    string `json:"mac"`
	Hosts  int    `json:"hosts"`
}

// virtual interfaces never worth sweeping: containers, bridges, VPNs.
var virtualPrefixes = []string{"lo", "docker", "br-", "veth", "virbr", "vnet", "tailscale", "tun", "tap", "wg", "zt", "utun", "cni", "flannel", "cali", "kube", "lxc", "lxd", "podman", "vmnet", "vboxnet", "awdl", "llw", "anpi", "bridge", "gif", "stf", "ap"}

func virtual(name string) bool {
	for _, p := range virtualPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// Interfaces lists the IPv4 interfaces with a MAC that look like a real LAN
// (not containers or VPNs), the one with the default route first.
func Interfaces() []Interface {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	def := defaultInterface()
	var out []Interface
	for _, ifi := range ifs {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 || len(ifi.HardwareAddr) != 6 || virtual(ifi.Name) {
			continue
		}
		addrs, _ := ifi.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.To4() == nil || ipn.IP.IsLinkLocalUnicast() {
				continue
			}
			ones, bits := ipn.Mask.Size()
			if bits != 32 || ones < 16 || ones > 30 {
				continue
			}
			sub := &net.IPNet{IP: ipn.IP.Mask(ipn.Mask), Mask: ipn.Mask}
			out = append(out, Interface{Name: ifi.Name, IP: ipn.IP.String(), Subnet: sub.String(), MAC: ifi.HardwareAddr.String(), Hosts: HostCount(sub)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name == def && out[j].Name != def })
	return out
}

// HostCount is the number of usable addresses in a subnet.
func HostCount(n *net.IPNet) int {
	ones, bits := n.Mask.Size()
	if bits != 32 {
		return 0
	}
	c := 1 << (32 - ones)
	if c > 2 {
		c -= 2
	}
	return c
}

// MaxHosts caps a sweep (a /22).
const MaxHosts = 1022

// ParseSubnet reads a CIDR ("192.168.1.0/24") and checks it is small enough to sweep.
func ParseSubnet(s string) (*net.IPNet, error) {
	_, n, err := net.ParseCIDR(strings.TrimSpace(s))
	if err != nil || n.IP.To4() == nil {
		return nil, fmt.Errorf("%q is not an IPv4 network like 192.168.1.0/24", s)
	}
	if HostCount(n) > MaxHosts {
		return nil, fmt.Errorf("%s has %d addresses; sweep at most a /22 (%d)", n, HostCount(n), MaxHosts)
	}
	return n, nil
}

// hostsOf lists every usable address of a subnet.
func hostsOf(n *net.IPNet) []net.IP {
	base := binary.BigEndian.Uint32(n.IP.To4())
	ones, _ := n.Mask.Size()
	size := uint32(1) << (32 - ones)
	var out []net.IP
	for i := uint32(0); i < size; i++ {
		if size > 2 && (i == 0 || i == size-1) {
			continue
		}
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, base+i)
		out = append(out, ip)
	}
	return out
}

// pick finds the interface to sweep a subnet from (by name, else the one in the subnet).
func pick(name string, subnet *net.IPNet) (*net.Interface, net.IP, *net.IPNet, error) {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, nil, nil, err
	}
	for i := range ifs {
		ifi := &ifs[i]
		if name != "" && ifi.Name != name {
			continue
		}
		addrs, _ := ifi.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.To4() == nil {
				continue
			}
			own := &net.IPNet{IP: ipn.IP.Mask(ipn.Mask), Mask: ipn.Mask}
			if subnet == nil {
				return ifi, ipn.IP.To4(), own, nil
			}
			if subnet.Contains(ipn.IP) || name != "" {
				return ifi, ipn.IP.To4(), subnet, nil
			}
		}
	}
	if name != "" {
		return nil, nil, nil, fmt.Errorf("this machine has no network interface %q with an IPv4 address", name)
	}
	return nil, nil, nil, fmt.Errorf("this machine has no address in %s; scan it with an agent on that network", subnet)
}

// ErrNoPermission means raw sockets are not allowed (fallback used).
var ErrNoPermission = errors.New("no permission for raw network access")

// Result is one sweep.
type Result struct {
	Hosts     []Observation `json:"hosts"`
	Method    string        `json:"method"` // arp | arp-table
	Interface string        `json:"interface"`
	Subnet    string        `json:"subnet"`
	Took      time.Duration `json:"took"`
	Note      string        `json:"note,omitempty"`
}

// Options tune a sweep.
type Options struct {
	Interface string // "" = the one in Subnet, else the default one
	Subnet    string // "" = the interface's own subnet
	Names     bool   // look names up
	DNS       string // DNS server for reverse lookups ("" = the system's)
}

// Sweep finds every device answering ARP on a subnet, then (optionally) their names.
func Sweep(ctx context.Context, o Options) (*Result, error) {
	start := time.Now()
	var subnet *net.IPNet
	if o.Subnet != "" {
		n, err := ParseSubnet(o.Subnet)
		if err != nil {
			return nil, err
		}
		subnet = n
	}
	if o.Interface == "" && subnet == nil {
		if ifs := Interfaces(); len(ifs) > 0 {
			o.Interface = ifs[0].Name
		}
	}
	ifi, self, subnet, err := pick(o.Interface, subnet)
	if err != nil {
		return nil, err
	}
	if HostCount(subnet) > MaxHosts {
		return nil, fmt.Errorf("%s is too big to sweep (at most a /22); set a smaller network", subnet)
	}
	res := &Result{Interface: ifi.Name, Subnet: subnet.String(), Method: "arp"}
	found, err := arpSweep(ctx, ifi, self, subnet)
	if errors.Is(err, ErrNoPermission) || errors.Is(err, errUnsupported) {
		res.Method = "arp-table"
		if errors.Is(err, ErrNoPermission) {
			res.Note = "no permission for raw network access: devices were found through the system's ARP table, which can miss quiet ones (give the app CAP_NET_RAW)"
		}
		found, err = tableSweep(ctx, ifi, self, subnet)
	}
	if err != nil {
		return nil, err
	}
	// The scanning machine never answers its own ARP: add it.
	if subnet.Contains(self) {
		found[self.String()] = ifi.HardwareAddr.String()
	}
	for ip, mac := range found {
		h := Observation{IP: ip, MAC: NormalizeMAC(mac), Self: ip == self.String()}
		h.Vendor = Vendor(h.MAC)
		res.Hosts = append(res.Hosts, h)
	}
	sort.Slice(res.Hosts, func(i, j int) bool { return ipLess(res.Hosts[i].IP, res.Hosts[j].IP) })
	if o.Names {
		ResolveNames(ctx, res.Hosts, o.DNS)
	}
	res.Took = time.Since(start)
	return res, nil
}

func ipLess(a, b string) bool {
	x, y := net.ParseIP(a).To4(), net.ParseIP(b).To4()
	if x == nil || y == nil {
		return a < b
	}
	return binary.BigEndian.Uint32(x) < binary.BigEndian.Uint32(y)
}

// IPLess orders dotted IPv4 addresses numerically.
func IPLess(a, b string) bool { return ipLess(a, b) }
