// +build darwin freebsd linux netbsd openbsd

package main

import (
	"os"
	"net"
	"syscall"
)

func isErrConnReset(err error) bool {
        if ne, ok := err.(*net.OpError); ok {
                if se, seok := ne.Err.(*os.SyscallError); seok {
                        return se.Err == syscall.ECONNRESET
                }
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
