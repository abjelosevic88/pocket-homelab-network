package netscan

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"net"
	"strings"
	"sync"
)

// oui.txt.gz maps MAC prefixes to vendors (scripts/oui.py, from Wireshark's
// manuf file: IEEE MA-L, MA-M and MA-S).
//
//go:embed oui.txt.gz
var ouiGz []byte

var (
	ouiOnce sync.Once
	ouiMap  map[string]string // hex prefix (6, 7 or 9 digits) → vendor
)

func loadOUI() {
	ouiMap = make(map[string]string, 60000)
	zr, err := gzip.NewReader(bytes.NewReader(ouiGz))
	if err != nil {
		return
	}
	sc := bufio.NewScanner(zr)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "\t")
		if ok {
			ouiMap[k] = v
		}
	}
}

// NormalizeMAC returns aa:bb:cc:dd:ee:ff, or "" when s is not a MAC.
func NormalizeMAC(s string) string {
	hw, err := net.ParseMAC(strings.TrimSpace(s))
	if err != nil || len(hw) != 6 {
		return ""
	}
	return hw.String()
}

// PrivateMAC reports a locally administered address: phones, watches and
// laptops make these up per network (or every so often) for privacy, so no
// vendor can be looked up and the same device may come back under a new one.
func PrivateMAC(mac string) bool {
	hw, err := net.ParseMAC(mac)
	if err != nil || len(hw) == 0 {
		return false
	}
	return hw[0]&0x02 != 0
}

// Vendor is the maker registered for a MAC's prefix ("" for private or unknown ones).
func Vendor(mac string) string {
	if PrivateMAC(mac) {
		return ""
	}
	hex := strings.ToUpper(strings.NewReplacer(":", "", "-", "", ".", "").Replace(mac))
	if len(hex) < 6 {
		return ""
	}
	ouiOnce.Do(loadOUI)
	for _, n := range []int{9, 7, 6} {
		if len(hex) >= n {
			if v, ok := ouiMap[hex[:n]]; ok {
				return v
			}
		}
	}
	return ""
}
