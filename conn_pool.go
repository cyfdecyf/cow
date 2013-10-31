// Share server connections between different clients.

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
	muxConn:  make(chan *serverConn, maxServerConnCnt*2),
}

func init() {
	// make sure hostPort here won't match any actual hostPort
	go closeStaleServerConn(connPool.muxConn, "muxConn")
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

func putConnToChan(sv *serverConn, ch chan *serverConn, chname string) {
	select {
	case ch <- sv:
		debug.Printf("connPool channel %s: put conn\n", chname)
		return
	default:
		// Simply close the connection if can't put into channel immediately.
		// A better solution would remove old connections from the channel and
		// add the new one. But's it's more complicated and this should happen
		// rarely.
		debug.Printf("connPool channel %s: full", chname)
		sv.Close()
	}
}

func (cp *ConnPool) Get(hostPort string, asDirect bool) (sv *serverConn) {
	// Get from site specific connection first.
	// Direct connection are all site specific, so must use site specific
	// first to avoid using parent proxy for direct sites.
	cp.RLock()
	ch := cp.idleConn[hostPort]
	cp.RUnlock()

	if ch != nil {
		sv = getConnFromChan(ch)
	}
	if sv != nil {
		debug.Printf("connPool %s: get conn\n", hostPort)
		return sv
	}

	// All mulplexing connections are for blocked sites,
	// so for direct sites we should stop here.
	if asDirect {
		return nil
	}

	sv = getConnFromChan(cp.muxConn)
	if bool(debug) && sv != nil {
		debug.Println("connPool mux: get conn", hostPort)
	}
	return sv
}

func (cp *ConnPool) Put(sv *serverConn) {
	// Multiplexing connections.
	switch sv.Conn.(type) {
	case httpConn, cowConn:
		putConnToChan(sv, cp.muxConn, "muxConn")
		return
	}

	// Site specific connections.
	cp.RLock()
	ch := cp.idleConn[sv.hostPort]
	cp.RUnlock()

	if ch == nil {
		debug.Printf("connPool %s: new channel\n", sv.hostPort)
		ch = make(chan *serverConn, maxServerConnCnt)
		ch <- sv
		cp.Lock()
		cp.idleConn[sv.hostPort] = ch
		cp.Unlock()
		// start a new goroutine to close stale server connections
		go closeStaleServerConn(ch, sv.hostPort)
	} else {
		putConnToChan(sv, ch, sv.hostPort)
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
				// No more connection in this channel, remove the channel from the map.
				// Note: muxConn is not in idleConn, though delete would be a no-op,
				// it has to acquire the lock.
				if _, ok := connPool.idleConn[hostPort]; ok {
					debug.Printf("connPool channel %s: remove\n", hostPort)
					connPool.Lock()
					delete(connPool.idleConn, hostPort)
					connPool.Unlock()
				}
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
