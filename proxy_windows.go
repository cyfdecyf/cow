package main

import (
	"fmt"
	"net"
	"reflect"
	"strings"
	"syscall"
)

var _ = fmt.Println
var _ = reflect.TypeOf

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
	// fmt.Printf("calling isDNSError for err type: %v Error() %s\n",
	// reflect.TypeOf(err), err.Error())
	return strings.Contains(err.Error(), "No such host")
}
