// +build darwin freebsd linux netbsd openbsd

package main

import (
	"net"
	"syscall"
)

func isErrConnReset(err error) bool {
	if ne, ok := err.(*net.OpError); ok {
		return ne.Err == syscall.ECONNRESET
	}
	return false
}

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

func isErrTooManyOpenFd(err error) bool {
	if ne, ok := err.(*net.OpError); ok && (ne.Err == syscall.EMFILE || ne.Err == syscall.ENFILE) {
		errl.Println("too many open fd")
		return true
	}
	return false
}
