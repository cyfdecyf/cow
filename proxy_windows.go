package main

import (
	"fmt"
	"net"
	"syscall"
)

var _ = fmt.Print

func isErrConnReset(err error) bool {
	if ne, ok := err.(*net.OpError); ok {
		if errno, enok := ne.Err.(syscall.Errno); enok {
			// I got these number by print. Only tested on XP.
			// fmt.Println("network errno:", errno)
			return errno == 64 || errno == 10054
		}
	}
	return false
}
