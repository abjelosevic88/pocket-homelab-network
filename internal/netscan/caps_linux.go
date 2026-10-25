//go:build linux

package netscan

import (
	"syscall"
	"unsafe"
)

// The image gives the binary CAP_NET_RAW as a permitted (not effective)
// file capability, so it still starts where that capability is not allowed
// (it then falls back to the ARP table). Linux capabilities are per thread:
// raiseNetRaw makes it effective on the calling (locked) thread just long
// enough to open the raw socket, and dropNetRaw takes it away again.

const (
	capV3     = 0x20080522
	capNetRaw = 13
)

type capHeader struct {
	version uint32
	pid     int32
}

type capData struct {
	effective, permitted, inheritable uint32
}

func capSet(raise bool) {
	hdr := capHeader{version: capV3}
	var data [2]capData
	if _, _, e := syscall.RawSyscall(syscall.SYS_CAPGET, uintptr(unsafe.Pointer(&hdr)), uintptr(unsafe.Pointer(&data[0])), 0); e != 0 {
		return
	}
	if data[0].permitted&(1<<capNetRaw) == 0 {
		return
	}
	if raise {
		data[0].effective |= 1 << capNetRaw
	} else {
		data[0].effective &^= 1 << capNetRaw
	}
	_, _, _ = syscall.RawSyscall(syscall.SYS_CAPSET, uintptr(unsafe.Pointer(&hdr)), uintptr(unsafe.Pointer(&data[0])), 0)
}

func raiseNetRaw() { capSet(true) }
func dropNetRaw()  { capSet(false) }
