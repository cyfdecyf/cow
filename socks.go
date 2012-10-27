package main

import (
	"net"
)

type socksHandler struct {
	net.Conn
	stop chan bool // Used to notify the handler to stop execution
}

var socksVerMethodSelectionMsg = []byte{
	0x5, // version 5
	1,   // n method
	0,   // no authorization required
}

var socksVerMethodReplyBuf = make([]byte, 2, 2)

func (c *clientConn) createSocksHandler(host string, isConnect bool) (Handler, error) {
	var err error
	var socksconn net.Conn
	var n int
	var handler *socksHandler

	if socksconn, err = net.Dial("tcp", "127.0.0.1:1087"); err != nil {
		errl.Printf("Can't connect to socks server %v\n", err)
		return nil, err
	}

	if n, err = socksconn.Write(socksVerMethodSelectionMsg); n != 3 || err != nil {
		errl.Printf("sending ver/method selection msg %v n = %v\n", err, n)
		goto fail
	}

	n, err = socksconn.Read(socksVerMethodReplyBuf)
	if err != nil {
		errl.Printf("read ver/method selection error\n", err)
		goto fail
	}

	if socksVerMethodReplyBuf[0] != 5 {
		errl.Printf("socks server version %d not supported", socksVerMethodReplyBuf[0])
		goto fail
	}
	if socksVerMethodReplyBuf[1] != 0 {
		errl.Printf("socks ver/method selection reply %d ", socksVerMethodReplyBuf[1])
		goto fail
	}

	debug.Println("Connected to socks server")

	handler = &socksHandler{socksconn, make(chan bool)}
	c.handler[host] = handler

	return handler, nil

fail:
	socksconn.Close()
	return nil, err
}

func (socks *socksHandler) ServeRequest(r *Request, c *clientConn) (err error) {
	return nil
}

func (h *socksHandler) NotifyStop() {
	h.stop <- true
}
