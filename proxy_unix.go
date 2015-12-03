// +build darwin freebsd linux netbsd openbsd

package main

import (
	"net"
)

func isDNSError(err error) bool {
	if _, ok := err.(*net.DNSError); ok {
		return true
	}
	return false
}

func isErrOpWrite(err error) bool {
	ne, ok := err.(*net.OpError)
	if !ok {
		return false
	}
	return ne.Op == "write"
}

func isErrOpRead(err error) bool {
	ne, ok := err.(*net.OpError)
	if !ok {
		return false
	}
	return ne.Op == "read"
}
