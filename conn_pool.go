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
	muxConn  chan *serverConn // connections support multiplexing
	sync.RWMutex
}

var connPool = &ConnPool{
	idleConn: map[string]chan *serverConn{},
	muxConn:  make(chan *serverConn, maxServerConnCnt),
}

func getConnFromChan(ch chan *serverConn) (sv *serverConn) {
	for {
		select {
		case sv = <-ch:
			if sv.mayBeClosed() {
				sv.Close()
				continue
			}
			return sv
		default:
			return nil
		}
	}
}

func putConnToChan(sv *serverConn, ch chan *serverConn) {
	select {
	case ch <- sv:
		debug.Printf("connPool channel %s: put back conn\n", sv.hostPort)
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

func (cp *ConnPool) Get(hostPort string) *serverConn {
	cp.RLock()
	ch, ok := cp.idleConn[hostPort]
	cp.RUnlock()

	if !ok {
		return nil
	}

	// get connection from connections of existing host
	if sv := getConnFromChan(ch); sv != nil {
		debug.Printf("connPool get site-specific conn")
		return sv
	}
	// get connection from multiplexing connection pool
	sv := getConnFromChan(cp.muxConn)
	if sv != nil {
		debug.Printf("connPool get mux conn")
	}
	return sv
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

	switch sv.Conn.(type) {
	case httpConn:
		// multiplexing connections
		debug.Printf("connPool put back mux conn")
		putConnToChan(sv, cp.muxConn)
	default:
		// site-specific connections
		debug.Printf("connPool put back site-specific conn")
		putConnToChan(sv, ch)
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
done:
	for {
		time.Sleep(defaultServerConnTimeout)
	cleanup:
		for {
			select {
			case sv := <-ch:
				if sv.mayBeClosed() {
					debug.Printf("connPool channel %s: close one conn\n", hostPort)
					sv.Close()
				} else {
					// Put it back and wait.
					debug.Printf("connPool channel %s: put back conn\n", hostPort)
					ch <- sv
					break cleanup
				}
			default:
				// no more connection in this channel
				// remove the channel from the map
				connPool.Lock()
				delete(connPool.idleConn, hostPort)
				connPool.Unlock()
				debug.Printf("connPool channel %s: removed\n", hostPort)
				break done
			}
		}
	}
	// Final wait and then close all left connections. In practice, there
	// should be no other goroutines holding reference to the channel.
	time.Sleep(2 * time.Second)
	for {
		select {
		case sv := <-ch:
			debug.Printf("connPool channel %s: close conn after removed\n", hostPort)
			sv.Close()
		default:
			debug.Printf("connPool channel %s: cleanup done\n", hostPort)
			return
		}
	}
}
