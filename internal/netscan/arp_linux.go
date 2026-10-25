//go:build linux

package netscan

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"runtime"
	"syscall"
	"time"
)

const ethPArp = 0x0806

func htons(v uint16) uint16 { return v<<8 | v>>8 }

// arpSweep sends an ARP request to every address of the subnet from a raw
// packet socket and collects the replies (needs CAP_NET_RAW).
func arpSweep(ctx context.Context, ifi *net.Interface, self net.IP, subnet *net.IPNet) (map[string]string, error) {
	runtime.LockOSThread()
	raiseNetRaw()
	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW, int(htons(ethPArp)))
	dropNetRaw()
	runtime.UnlockOSThread()
	if err != nil {
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
			return nil, ErrNoPermission
		}
		return nil, err
	}
	defer syscall.Close(fd)
	if err := syscall.Bind(fd, &syscall.SockaddrLinklayer{Protocol: htons(ethPArp), Ifindex: ifi.Index}); err != nil {
		return nil, err
	}
	tv := syscall.NsecToTimeval(int64(150 * time.Millisecond))
	_ = syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv)

	found := map[string]string{}
	done := make(chan struct{})
	stop := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 1500)
		for {
			select {
			case <-stop:
				return
			default:
			}
			n, _, err := syscall.Recvfrom(fd, buf, 0)
			if err != nil || n < 42 {
				continue
			}
			f := buf[:n]
			if binary.BigEndian.Uint16(f[12:14]) != ethPArp || binary.BigEndian.Uint16(f[20:22]) != 2 {
				continue
			}
			ip := net.IP(append([]byte(nil), f[28:32]...))
			if !subnet.Contains(ip) || ip.Equal(self) {
				continue
			}
			mac := net.HardwareAddr(append([]byte(nil), f[22:28]...))
			found[ip.String()] = mac.String()
		}
	}()

	bcast := [8]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	to := &syscall.SockaddrLinklayer{Protocol: htons(ethPArp), Ifindex: ifi.Index, Halen: 6, Addr: bcast}
	frame := make([]byte, 42)
	copy(frame[0:6], bcast[:6])
	copy(frame[6:12], ifi.HardwareAddr)
	binary.BigEndian.PutUint16(frame[12:14], ethPArp)
	binary.BigEndian.PutUint16(frame[14:16], 1)      // Ethernet
	binary.BigEndian.PutUint16(frame[16:18], 0x0800) // IPv4
	frame[18], frame[19] = 6, 4
	binary.BigEndian.PutUint16(frame[20:22], 1) // request
	copy(frame[22:28], ifi.HardwareAddr)
	copy(frame[28:32], self.To4())
	hosts := hostsOf(subnet)
	var sendErr error
	for round := 0; round < 2 && sendErr == nil; round++ {
		for _, ip := range hosts {
			if ip.Equal(self) {
				continue
			}
			if ctx.Err() != nil {
				sendErr = ctx.Err()
				break
			}
			copy(frame[38:42], ip.To4())
			if err := syscall.Sendto(fd, frame, 0, to); err != nil {
				sendErr = err
				break
			}
			time.Sleep(1500 * time.Microsecond)
		}
	}
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
	}
	close(stop)
	<-done
	if sendErr != nil && len(found) == 0 {
		return nil, sendErr
	}
	return found, nil
}
