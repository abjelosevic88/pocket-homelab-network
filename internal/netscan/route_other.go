//go:build !linux

package netscan

func defaultInterface() string { return "" }
