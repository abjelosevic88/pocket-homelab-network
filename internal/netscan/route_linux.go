//go:build linux

package netscan

import (
	"bufio"
	"os"
	"strings"
)

// defaultInterface is the interface of the IPv4 default route.
func defaultInterface() string {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Scan()
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) > 2 && fs[1] == "00000000" && !virtual(fs[0]) {
			return fs[0]
		}
	}
	return ""
}
