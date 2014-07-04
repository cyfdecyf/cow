package main

import (
	"fmt"
	"net"
	"strings"
	"syscall"
)

var _ = fmt.Println

func isErrConnReset(err error) bool {
	// fmt.Printf("calling isErrConnReset for err type: %v Error() %s\n",
	// reflect.TypeOf(err), err.Error())
	if ne, ok := err.(*net.OpError); ok {
		// fmt.Println("isErrConnReset net.OpError.Err type:", reflect.TypeOf(ne))
		if errno, enok := ne.Err.(syscall.Errno); enok {
			// I got these number by print. Only tested on XP.
			// fmt.Printf("isErrConnReset errno: %d\n", errno)
			return errno == 64 || errno == 10054
		}
	}
	return false
}

func isDNSError(err error) bool {
	/*
		fmt.Printf("calling isDNSError for err type: %v %s\n",
			reflect.TypeOf(err), err.Error())
	*/
	// DNS error are not of type DNSError on Windows, so I used this ugly
	// hack.
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

func isErrTooManyOpenFd(err error) bool {
	// TODO implement this.
	return false
}
