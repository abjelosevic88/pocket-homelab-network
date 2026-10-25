//go:build !linux

package netscan

import (
	"context"
	"net"
)

func arpSweep(context.Context, *net.Interface, net.IP, *net.IPNet) (map[string]string, error) {
	return nil, errUnsupported
}
