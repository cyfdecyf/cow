// Proxy statistics.

package main

import (
	"sync"
	"sync/atomic"
)

var status struct {
	cliCnt          int32          // number of client connections
	srvConnCnt      map[string]int // number of connections for each host:port
	srvConnCntMutex sync.Mutex
}

func initStat() {
	if !debug {
		return
	}
	status.srvConnCnt = make(map[string]int)
}

func incCliCnt() int32 {
	atomic.AddInt32(&status.cliCnt, 1)
	return status.cliCnt
}

func decCliCnt() int32 {
	atomic.AddInt32(&status.cliCnt, -1)
	return status.cliCnt
}

func addSrvConnCnt(srv string, delta int) int {
	status.srvConnCntMutex.Lock()
	status.srvConnCnt[srv] += delta
	cnt := status.srvConnCnt[srv]
	status.srvConnCntMutex.Unlock()
	return int(cnt)
}

func incSrvConnCnt(srv string) int {
	return addSrvConnCnt(srv, 1)
}

func decSrvConnCnt(srv string) int {
	return addSrvConnCnt(srv, -1)
}
