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
