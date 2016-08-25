package main

import (
	"net"
	"strings"
	"os"
	"syscall"
)

func isErrConnReset(err error) bool {
	if ne, ok := err.(*net.OpError); ok {
		if se, seok := ne.Err.(*os.SyscallError); seok {
			return se.Err == syscall.WSAECONNRESET || se.Err == syscall.ECONNRESET
		}
	}
	return false
}

func isDNSError(err error) bool {
	errMsg := err.Error()
	return strings.Contains(errMsg, "No such host") ||
		strings.Contains(errMsg, "GetAddrInfoW") ||
		strings.Contains(errMsg, "dial tcp")
}

func isErrOpWrite(err error) bool {
	ne, ok := err.(*net.OpError)
	if !ok {
		return false
	}
	return ne.Op == "WSASend"
}

func isErrOpRead(err error) bool {
	ne, ok := err.(*net.OpError)
	if !ok {
		return false
	}
	return ne.Op == "WSARecv"
}
