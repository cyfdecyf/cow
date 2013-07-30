// Shared server connections between different clients.

package main

import (
	"sync"
	"time"
)

// Maximum number of connections to a server.
const maxServerConnCnt = 20

// Store each server's connections in separate channels, getting
// connections for different servers can be done in parallel.
type ConnPool struct {
	idleConn map[string]chan *serverConn
	sync.RWMutex
}

var connPool *ConnPool

func initConnPool() {
	connPool = new(ConnPool)
	connPool.idleConn = make(map[string]chan *serverConn)
}

func (cp *ConnPool) Get(hostPort string) *serverConn {
	cp.RLock()
	ch, ok := cp.idleConn[hostPort]
	cp.RUnlock()

	if !ok {
		return nil
	}

	for {
		select {
		case sv := <-ch:
			if sv.mayBeClosed() {
				sv.Close()
				continue
			}
			debug.Printf("connPool %s: get conn\n", hostPort)
			return sv
		default:
			return nil
		}
	}
}

func (cp *ConnPool) Put(sv *serverConn) {
	var ch chan *serverConn

	cp.RLock()
	ch, ok := cp.idleConn[sv.hostPort]
	cp.RUnlock()

	if !ok {
		debug.Printf("connPool %s: new channel", sv.hostPort)
		ch = make(chan *serverConn, maxServerConnCnt)
		ch <- sv
		cp.Lock()
		cp.idleConn[sv.hostPort] = ch
		cp.Unlock()
		// start a new goroutine to close stale server connections
		go closeStaleServerConn(ch, sv.hostPort)
		return
	}

	select {
	case ch <- sv:
		debug.Printf("connPool %s: put one conn", sv.hostPort)
		return
	default:
		// Simply close the connection if can't put into channel immediately.
		// A better solution would remove old connections from the channel and
		// add the new one. But's it's more complicated and this should happen
		// rarely.
		debug.Printf("connPool %s: channel full", sv.hostPort)
		sv.Close()
	}
}

func closeStaleServerConn(ch chan *serverConn, hostPort string) {
	// Tricky here. When removing a channel from the map, there maybe
	// goroutines doing Put and Get using that channel.

	// For Get, there's no problem because it will return immediately.
	// For Put, it's possible that a new connection is added to the
	// channel, but the channel is no longer in the map.
	// So after removed the channel from the map, we wait for several seconds
	// and then close all connections left in it.

	// It's possible that Put add the connection after the final wait, but
	// that should not happen in practice, and the worst result is just lost
	// some memory and open fd.
DONE:
	for {
		time.Sleep(defaultServerConnTimeout)
	CLEANUP:
		for {
			select {
			case sv := <-ch:
				if sv.mayBeClosed() {
					debug.Printf("connPool %s: close one conn\n", hostPort)
					sv.Close()
				} else {
					// Put it back and wait.
					debug.Printf("connPool %s: put back conn\n", hostPort)
					ch <- sv
					break CLEANUP
				}
			default:
				// no more connection in this channel
				// remove the channel from the map
				connPool.Lock()
				delete(connPool.idleConn, hostPort)
				connPool.Unlock()
				debug.Printf("connPool %s: channeld removed\n", hostPort)
				break DONE
			}
		}
	}
	// Final wait and then close all left connections. In practice, there
	// should be no other goroutines holding reference to the channel.
	time.Sleep(2 * time.Second)
	for len(ch) > 0 {
		sv := <-ch
		sv.Close()
	}
	debug.Printf("connPool %s: cleanup routine finished\n", hostPort)
}
