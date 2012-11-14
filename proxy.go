package main

import (
	"bufio"
	"container/heap"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	// "reflect"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Lots of the code here are learnt from the http package

type Proxy struct {
	addr string // listen address
}

// Number of the simultaneous requests in the pipeline
const requestNum = 5

type connType byte

const (
	nilConn connType = iota
	directConn
	socksConn
)

type handlerState byte

const (
	hsConnected handlerState = iota
	hsResponsReceived
	hsPipelining
	hsStopped
)

type conn struct {
	net.Conn
	connType
}

type Handler struct {
	conn
	buf     *bufio.ReadWriter
	host    string
	stop    notification  // Used to notify the handler to stop execution
	request chan *Request // Receive HTTP request from request goroutine
	state   handlerState
}

var one = make([]byte, 1)

func hasClosed(c net.Conn) bool {
	c.SetReadDeadline(time.Now())
	_, err := c.Read(one)
	if ne, ok := err.(*net.OpError); !ok || !ne.Timeout() {
		errl.Println("actively detected closed connection", c.RemoteAddr(), err)
		return true
	}
	c.SetReadDeadline(time.Time{})
	return false
}

func newHandler(c conn, host string) *Handler {
	return &Handler{
		conn: c,
		host: host,
		buf: bufio.NewReadWriter(bufio.NewReaderSize(c, bufSize),
			bufio.NewWriter(c)),
	}
}

type clientConn struct {
	buf        *bufio.ReadWriter
	net.Conn                       // connection to the proxy client
	handler    map[string]*Handler // request handler, host:port as key
	finHandler chan *Handler       // Finished handler will pass it's self to request goroutine
	reqNo      int                 // How many request has been received
	reqPQ      RequestPQ           // Holds redo request
}

var (
	errRetry = errors.New("Retry")
)

func NewProxy(addr string) *Proxy {
	return &Proxy{addr: addr}
}

func (py *Proxy) Serve() {
	ln, err := net.Listen("tcp", py.addr)
	if err != nil {
		info.Println("Server creation failed:", err)
		os.Exit(1)
	}
	info.Println("COW proxy listening", py.addr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			debug.Println("Client connection:", err)
			continue
		}
		debug.Println("New Client:", conn.RemoteAddr())
		c := newClientConn(conn)
		go c.serve()
	}
}

// Explicitly specify buffer size to avoid unnecessary copy using
// bufio.Reader's Read
const bufSize = 4096

func newClientConn(rwc net.Conn) *clientConn {
	c := &clientConn{
		Conn:    rwc,
		handler: map[string]*Handler{},
		buf: bufio.NewReadWriter(bufio.NewReaderSize(rwc, bufSize),
			bufio.NewWriter(rwc)),
	}
	if config.pipeline {
		c.reqPQ = make(RequestPQ, 0, 0)
		c.finHandler = make(chan *Handler, 5)
	}
	return c
}

func (c *clientConn) close() {
	// There's no need to wait response goroutine finish. They will finish
	// When see the stop notification or detects client connection error.
	if config.pipeline {
		for _, h := range c.handler {
			h.stop.notify()
		}
	}
	// Can't set c.buf to nil because maybe used in response goroutine
	c.Close()
	debug.Printf("Client %v connection closed\n", c.RemoteAddr())
	runtime.GC()
}

func isSelfURL(h string) bool {
	return h == "" || h == selfURL127 || h == selfURLLH
}

func (c *clientConn) getRedoRequest() *Request {
	// Collect all redo request into priority queue
loop:
	for {
		select {
		case h := <-c.finHandler:
			for {
				if r := h.nextRequest(); r != nil {
					errl.Println("Add redo request", r)
					heap.Push(&c.reqPQ, r)
				} else {
					break
				}
			}
			c.removeHandler(h)
		default:
			break loop
		}
	}
	if len(c.reqPQ) == 0 {
		return nil
	}
	return heap.Pop(&c.reqPQ).(*Request)
}

func (c *clientConn) getRequest() (r *Request) {
	var err error
	if !config.pipeline {
		if r, err = parseRequest(c.buf.Reader); err != nil {
			if err != io.EOF {
				errl.Printf("Reading client %s request: %v", c.RemoteAddr(), err)
			}
			return nil
		}
		return r
	}
	// TODO It's difficult to send the request to the server in the same order
	// as they arrived.

	// Between the time we get a new request from the client and sending it to
	// the server (newly connected as previous connection is closed), there
	// maybe previous failed request which needs retry.

	// One possible solution is to disallow pipelining request to different
	// servers.
retry:
	if r = c.getRedoRequest(); r != nil {
		if hasClosed(c) {
			return nil
		}
		errl.Println("Redo")
		return
	}
	if c.SetReadDeadline(time.Now().Add(time.Second)) != nil {
		debug.Println("Setting read deadline before parsing request")
	}
	if r, err = parseRequest(c.buf.Reader); err != nil {
		if ne, ok := err.(*net.OpError); ok && ne.Timeout() {
			goto retry
		}
		if err != io.EOF {
			errl.Printf("Reading client %s request: %v", c.RemoteAddr(), err)
		}
		return nil
	}
	if redoReq := c.getRedoRequest(); redoReq != nil {
		errl.Println("Redo")
		heap.Push(&c.reqPQ, r)
		return redoReq
	}
	return
}

func (c *clientConn) serve() {
	defer c.close()
	var r *Request
	var err error
	var h *Handler

	// Refer to implementation.md for the design choices on parsing the request
	// and response.
	for {
		if r = c.getRequest(); r == nil {
			return
		}
		if dbgRq {
			dbgRq.Printf("%v %v\n", c.RemoteAddr(), r)
		}

		if isSelfURL(r.URL.Host) {
			// Send PAC file if requesting self
			sendPAC(c.buf.Writer)
			return
		}

	retry:
		if h, err = c.getHandler(r); err != nil {
			// Failed connection will send error page back to client
			errl.Printf("Failed to get handler for %s %v\n", c.RemoteAddr(), r)
			continue
		}

		if r.isConnect {
			// Why return after doConnect:
			// 1. proxy can only know the request is finished when either
			// the server or the client closed connection
			// 2. if the web server closes connection, the only way to
			// tell the client this is to close client connection (proxy
			// don't know the protocol between the client and server)
			h.doConnect(r, c)
			debug.Printf("doConnect for %s to %s done\n", c.RemoteAddr(), r.URL.Host)
			return
		}

		if err = h.doRequest(r, c); err != nil {
			c.removeHandler(h)
			if err == errRetry {
				if !config.pipeline {
					errl.Printf("%s retry request %v\n", c.RemoteAddr(), r)
					goto retry
				} else {
					heap.Push(&c.reqPQ, r)
				}
				continue
			}
			errl.Printf("Error doRequest for client %s %v: %v\n", c.RemoteAddr(), r, err)
			return
		}
	}
}

func genErrMsg(r *Request) string {
	return fmt.Sprintf("<p>HTTP Request <strong>%v</strong></p>", r)
}

func genBlockedSiteMsg(r *Request) string {
	host, _ := splitHostPort(r.URL.Host)
	if !hostIsIP(host) {
		return fmt.Sprintf(
			"<p>Domain <strong>%s</strong> added to blocked list. <strong>Try to refresh.</strong></p>",
			host2Domain(host))
	}
	return ""
}

func (c *clientConn) sendErrorPage(r *Request, h *Handler, err error) {
	msg := genErrMsg(r)
	// debug.Println("Type of error", reflect.TypeOf(err))
	if ne, ok := err.(*net.OpError); ok && h.connType == directConn {
		// GFW may connection reset here, may also make it time out Is it
		// normal for connection to a site timeout? If so, it's better not add
		// it to blocked site
		if ne.Err == syscall.ECONNRESET {
			addBlockedRequest(r)
			sendErrorPage(c.buf.Writer, "503", "Connection reset", ne.Error(), msg+genBlockedSiteMsg(r))
			return
		} else if ne.Timeout() {
			addBlockedRequest(r)
			sendErrorPage(c.buf.Writer, "504", "Time out reading response",
				ne.Error(), msg+genBlockedSiteMsg(r))
			return
		}
		// fallthrough to send general error page
	}
	sendErrorPage(c.buf.Writer, "502", "read error", err.Error(), msg)
	return
}

// What value is appropriate?
var readTimeout = 10 * time.Second

func (c *clientConn) readResponse(h *Handler, r *Request) (err error) {
	// If pipeline is enabled, request should always be received from h.request channel
	var rp *Response

	if h.mayBeFake() && h.SetReadDeadline(time.Now().Add(readTimeout)) != nil {
		debug.Println("Setting ReadDeadline before receiving the first response")
	}
	if rp, err = parseResponse(h.buf.Reader); err != nil {
		if h.stop.hasNotified() {
			debug.Println("readResponse stop requested after parseResponse error:", err)
			return
		}
		if err == io.EOF {
			// Don't receive request from channel because there may be none
			debug.Println("Server closed connection", h.host)
			return errRetry
		}
		// Handle other types of error, which should send error page back to client
		if config.pipeline {
			if r = h.nextRequest(); r != nil {
				c.sendErrorPage(r, h, err)
				errl.Printf("Error %v parsing response for client %s %v\n", err, c.RemoteAddr(), r)
				return
			}
			errl.Printf("Error %v parsing response for client %s\n", err, c.RemoteAddr())
		} else {
			c.sendErrorPage(r, h, err)
		}
		return
	}
	// After have received the first reponses from the server, we consider
	// ther server as real instead of fake one caused by wrong DNS reply. So
	// don't time out later.
	if h.mayBeFake() && h.SetReadDeadline(time.Time{}) != nil {
		debug.Println("Unset ReadDeadline")
	}
	if h.state == hsConnected {
		h.state = hsResponsReceived
	}

	if _, err = c.buf.WriteString(rp.raw.String()); err != nil {
		errl.Println("Writing response back to client %s: %v\n", c.RemoteAddr(), err)
		return
	}
	// Flush response header to the client ASAP
	if err = c.buf.Flush(); err != nil {
		errl.Printf("Flushing response header to client %s: %v\n", c.RemoteAddr(), err)
		return
	}

	// Read response first then receive request, so we can detect closed
	// server connection earlier.
	// TODO baidu image still encounters many closed connection even with
	// this optimization.
	if config.pipeline {
		r = <-h.request
	}
	// Wrap inside if to avoid function argument evaluation.
	if dbgRep {
		dbgRep.Printf("%v %s %v %v", c.RemoteAddr(), r.Method, r.URL, rp)
	}

	if rp.hasBody(r.Method) {
		if err = sendBody(c.buf.Writer, h.buf.Reader, rp.Chunking, rp.ContLen); err != nil {
			// TODO need to identify whether the err is caused by the server connection,
			// in that case, need to retry request
			if err != io.EOF {
				errl.Println("readResponse sendBody error %v for client %s %v", err,
					c.RemoteAddr(), r)
			}
			return
		}
	}
	/*
		if debug {
			debug.Printf("[Finished] %v request %s %s\n", c.RemoteAddr(), r.Method, r.URL)
		}
	*/

	if !rp.KeepAlive {
		h.state = hsStopped
		return
	}
	return
}

func (c *clientConn) responseReader(h *Handler) {
	// debug.Printf("Started response goroutine for client %s for host %s\n", c.RemoteAddr(), h.host)
	for {
		if c.readResponse(h, nil) != nil {
			break
		}
	}
	h.state = hsStopped
	// It's possbile that request is being sent through the server
	// connection. The sending side will discover closed server connection
	// and try to redo the request.
	errl.Printf("%s handler %s added to finHandler\n", c.RemoteAddr(), h.host)
	c.finHandler <- h
	h.Close()
	debug.Printf("Closed server conn %s for client %s\n", h.host, c.RemoteAddr())
}

func (c *clientConn) getHandler(r *Request) (h *Handler, err error) {
	h, ok := c.handler[r.URL.Host]
	if ok && h.state == hsStopped {
		c.removeHandler(h)
		ok = false
	}

	if !ok {
		h, err = c.createHandler(r)
	}
	return
}

func (c *clientConn) removeHandler(h *Handler) {
	delete(c.handler, h.host)
}

var dialTimeout = 10 * time.Second

func createDirectConnection(host string) (conn, error) {
	c, err := net.DialTimeout("tcp", host, dialTimeout)
	if err != nil {
		// Time out is very likely to be caused by [GFW]
		debug.Printf("Connecting to: %s %v\n", host, err)
		return conn{nil, nilConn}, err
	}
	debug.Println("Connected to", host)
	return conn{c, directConn}, nil
}

func (c *clientConn) createHandler(r *Request) (*Handler, error) {
	var err error
	var srvconn conn
	connFailed := false

	if isRequestBlocked(r) {
		// In case of connection error to socks server, fallback to direct connection
		if srvconn, err = createSocksConnection(r.URL.Host); err != nil {
			if srvconn, err = createDirectConnection(r.URL.Host); err != nil {
				connFailed = true
				goto connDone
			}
			addDirectRequest(r)
		}
	} else {
		// In case of error on direction connection, try socks server
		if srvconn, err = createDirectConnection(r.URL.Host); err != nil {
			// debug.Printf("type of err %v\n", reflect.TypeOf(err))
			// GFW may cause dns lookup fail, may also cause connection time out
			if _, ok := err.(*net.DNSError); ok {
			} else if ne, ok := err.(*net.OpError); ok && ne.Timeout() {
			} else {
				connFailed = true
				goto connDone
			}

			// Try to create socks connection
			if srvconn, err = createSocksConnection(r.URL.Host); err != nil {
				connFailed = true
				goto connDone
			}
			addBlockedRequest(r)
		} else {
			addDirectRequest(r)
		}
	}

connDone:
	if connFailed {
		sendErrorPage(c.buf.Writer, "504", "Connection failed", err.Error(), genErrMsg(r))
		return nil, err
	}

	h := newHandler(srvconn, r.URL.Host)
	if r.isConnect {
		// Don't put connection for CONNECT method for reuse
		return h, nil
	}

	if config.pipeline {
		h.stop = newNotification()
		h.request = make(chan *Request, requestNum)
	}

	c.handler[h.host] = h
	// client will connect to differnet servers in a single proxy connection
	// debug.Printf("handler to for client %v %v\n", c.RemoteAddr(), c.handler)

	return h, nil
}

func copyData(dst, src net.Conn, srcReader *bufio.Reader, dstStopped notification, dbgmsg string) (err error) {
	buf := make([]byte, bufSize)
	var n int
	for {
		if dstStopped.hasNotified() {
			debug.Println(dbgmsg, "dst has stopped")
			return
		}
		if err = src.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
			debug.Println("Set ReadDeadline in copyData:", err)
		}
		if n, err = srcReader.Read(buf); err != nil {
			if ne, ok := err.(*net.OpError); ok && ne.Timeout() {
				continue
			}
			if err != io.EOF {
				debug.Printf("%s read data: %v\n", dbgmsg, err)
			}
			return
		}

		_, err = dst.Write(buf[0:n])
		if err != nil {
			debug.Printf("%s write data: %v\n", dbgmsg, err)
			return
		}
	}
	return
}

func (h *Handler) mayBeFake() bool {
	// GFW may return wrong DNS record, which we can connect to but block
	// forever on read. (e.g. twitter.com) If we have never received any
	// response yet, then we should set a timeout for read/write.
	return h.state == hsConnected && h.connType == directConn
}

func (h *Handler) nextRequest() (r *Request) {
	select {
	case r = <-h.request:
		return
	default:
		return
	}
	return
}

var connEstablished = []byte("HTTP/1.0 200 Connection established\r\nProxy-agent: cow-proxy/0.1\r\n\r\n")

// Do HTTP CONNECT
func (srvconn *Handler) doConnect(r *Request, c *clientConn) (err error) {
	defer srvconn.Close()
	if debug {
		debug.Printf("%v 200 Connection established to %s\n", c.RemoteAddr(), r.URL.Host)
	}
	_, err = c.Write(connEstablished)
	if err != nil {
		errl.Printf("%v Error sending 200 Connecion established\n", c.RemoteAddr())
		return err
	}

	errchan := make(chan error)

	// Notify the destination has stopped in copyData is important. If the
	// client has stopped connection, while the server->client is blocked
	// reading data from the server, the server connection will not get chance
	// to stop (unless there's timeout in read). This may result too many open
	// connection error from the socks server.
	srvStopped := newNotification()
	clientStopped := newNotification()

	// Must wait this goroutine finish before returning from this function.
	// Otherwise, the server/client may have been closed and thus cause nil
	// pointer deference
	go func() {
		err := copyData(c, srvconn, bufio.NewReaderSize(srvconn, bufSize),
			clientStopped, "doConnect server->client")
		srvStopped.notify()
		errchan <- err
	}()

	err = copyData(srvconn, c, c.buf.Reader, srvStopped, "doConnect client->server")
	clientStopped.notify()

	// wait goroutine finish
	err2 := <-errchan
	if err2 != io.EOF {
		return err2
	}
	return
}

// Do HTTP request other that CONNECT
func (h *Handler) doRequest(r *Request, c *clientConn) (err error) {
	c.reqNo++
	r.no = c.reqNo
	// Send request to the server
	if _, err = h.buf.Write(r.raw.Bytes()); err != nil {
		// The srv connection maybe already closed.
		// Need to delete the connection and reconnect in that case.
		errl.Println("Sending request header:", err)
		return errRetry
	}
	if h.buf.Writer.Flush() != nil {
		errl.Println("Flushing request header:", err)
		return errRetry
	}

	// All possible error that caused by closed server connection should
	// redo request. (Otherwise, client request are lost.)
	// TODO It's possible that server connection is closed during POST, how to
	// identify this?

	// Send request body
	if r.Method == "POST" {
		if err = sendBody(h.buf.Writer, c.buf.Reader, r.Chunking, r.ContLen); err != nil {
			errl.Println("Sending request body:", err)
			return err
		}
	}

	if !config.pipeline {
		err = c.readResponse(h, r)
	} else {
		h.request <- r
		switch h.state {
		case hsConnected:
			// Enable pipelining for the connection only if it's persistent.
			// debug.Println("waiting response for req:", r)
			err = c.readResponse(h, nil)
		case hsResponsReceived:
			go c.responseReader(h)
			h.state = hsPipelining
		}
	}

	return
}

// Send response body if header specifies content length
func sendBodyWithContLen(w *bufio.Writer, r *bufio.Reader, contLen int64) (err error) {
	// debug.Println("Sending body with content length", contLen)
	if contLen == 0 {
		return
	}
	// CopyN will copy n bytes unless there's error of EOF. For EOF, it means
	// the connection is closed, return will propagate till serv function and
	// close client connection.
	if _, err = io.CopyN(w, r, contLen); err != nil {
		debug.Println("Sending response body to client", err)
		return err
	}
	return
}

// Send response body if header specifies chunked encoding
func sendBodyChunked(w *bufio.Writer, r *bufio.Reader) (err error) {
	// debug.Println("Sending chunked body")

	done := false
	for !done {
		var s string
		// Read chunk size line, ignore chunk extension if any
		if s, err = ReadLine(r); err != nil {
			errl.Println("Reading chunk size:", err)
			return err
		}
		// debug.Println("Chunk size line", s)
		f := strings.SplitN(s, ";", 2)
		var size int64
		if size, err = strconv.ParseInt(strings.TrimSpace(f[0]), 16, 64); err != nil {
			errl.Println("Chunk size not valid:", err)
			return err
		}
		w.WriteString(s)
		w.WriteString("\r\n")

		if size == 0 { // end of chunked data, ignore any trailers
			done = true
		} else {
			// Read chunk data and send to client
			if _, err = io.CopyN(w, r, size); err != nil {
				errl.Println("Reading chunked data from server:", err)
				return err
			}
		}

		// XXX maybe this kind of error handling should be passed to the
		// client? But if the proxy doesn't know when to stop reading from the
		// server, the only way to avoid blocked reading is to set read time
		// out on server connection. Would that be easier?
		if err = readCheckCRLF(r); err != nil {
			errl.Println("Reading chunked data CRLF:", err)
			return err
		}
		w.WriteString("\r\n")
	}
	return
}

func sendBodySplitIntoChunk(w *bufio.Writer, r *bufio.Reader) (err error) {
	buf := make([]byte, bufSize)
	var n int
	for {
		n, err = r.Read(buf)
		// debug.Println("split into chunk n =", n, "err =", err)
		if err != nil {
			// err maybe EOF which is expected here as the server is closing connection
			// For other errors, report the error it in readResponse
			w.WriteString("0\r\n\r\n")
			break
		}

		w.WriteString(fmt.Sprintf("%x\r\n", n))
		w.Write(buf[:n])
	}
	w.Flush()
	return
}

// Send message body
func sendBody(w *bufio.Writer, r *bufio.Reader, chunk bool, contLen int64) (err error) {
	if chunk {
		err = sendBodyChunked(w, r)
	} else if contLen >= 0 {
		err = sendBodyWithContLen(w, r, contLen)
	} else {
		// Server use close connection to indicate end of data
		err = sendBodySplitIntoChunk(w, r)
	}

	if err != nil {
		return
	}

	if err = w.Flush(); err != nil {
		// Maybe the client has closed the connection
		debug.Println("Flushing body to client:", err)
		return err
	}
	return
}

func hostIsIP(host string) bool {
	return net.ParseIP(host) != nil
}
