package main

import (
	"net"
	"strings"
)

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
