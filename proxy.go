package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// What value is appropriate?
const readTimeout = 15 * time.Second
const dialTimeout = 10 * time.Second

// Lots of the code here are learnt from the http package

type Proxy struct {
	addr string // listen address
}

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
	state   handlerState
	lastUse time.Time
}

var one = make([]byte, 1)

func newHandler(c conn, host string) *Handler {
	return &Handler{
		conn: c,
		host: host,
		buf: bufio.NewReadWriter(bufio.NewReaderSize(c, bufSize),
			bufio.NewWriter(c)),
	}
}

type clientConn struct {
	buf      *bufio.ReadWriter
	net.Conn                     // connection to the proxy client
	handler  map[string]*Handler // request handler, host:port as key
}

var (
	errRetry    = errors.New("Retry")
	errPageSent = errors.New("Error page has sent")
	errInternal = errors.New("Internal error")
)

func NewProxy(addr string) *Proxy {
	return &Proxy{addr: addr}
}

func (py *Proxy) Serve() {
	ln, err := net.Listen("tcp", py.addr)
	if err != nil {
		fmt.Println("Server creation failed:", err)
		os.Exit(1)
	}
	info.Println("COW proxy listening", py.addr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			debug.Println("Client connection:", err)
			continue
		}
		if debug {
			debug.Println("New Client:", conn.RemoteAddr())
		}
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
	return c
}

func (c *clientConn) close() {
	for _, h := range c.handler {
		h.Close()
	}
	c.Close()
	if debug {
		debug.Printf("Client %v connection closed\n", c.RemoteAddr())
	}
	c = nil
	runtime.GC()
}

func isSelfURL(h string) bool {
	return h == "" || h == selfURLLH || h == selfURL127
}

// Close client connection if no new requests come in after 5 seconds.
const clientConnTimeout = 5 * time.Second

func (c *clientConn) getRequest() (r *Request) {
	var err error
	if c.SetReadDeadline(time.Now().Add(clientConnTimeout)) != nil {
		debug.Println("SetReadDeadline BEFORE receiving client request")
	}
	if r, err = parseRequest(c.buf.Reader); err != nil {
		c.handleClientReadError(r, err, "parse client request")
		return nil
	}
	if c.SetReadDeadline(time.Time{}) != nil {
		debug.Println("Un-SetReadDeadline AFTER receiving client request")
	}
	return r
}

func getHostFromQuery(query string) string {
	for _, s := range strings.Split(query, "&") {
		if strings.HasPrefix(s, "host=") {
			return s[len("host="):]
		}
	}
	return ""
}

func (c *clientConn) serveSelfURLBlocked(r *Request) (err error) {
	query := r.URL.Path[9:] // "/blocked?" has 9 characters
	return c.serveSelfURLAddHost(r, query, "blocked", addBlockedHost)
}

func (c *clientConn) serveSelfURLDirect(r *Request) (err error) {
	query := r.URL.Path[8:] // "/direct?" has 9 characters
	return c.serveSelfURLAddHost(r, query, "direct", addDirectHost)
}

type addHostFunc func(string) bool

func (c *clientConn) serveSelfURLAddHost(r *Request, query, listType string, addHost addHostFunc) (err error) {
	// Adding blocked or direct site has side effects, so should use POST in
	// this regard. But client should not redirect for POST request, so I
	// choose to use GET when submitting form.
	host := getHostFromQuery(query)
	if hostIsIP(host) {
		// sendBlockedErrorPage will not put IP address in form, this should not happen.
		// server side checking to be safe.
		errl.Println("Host is IP address, shouldn't happen")
		sendErrorPage(c.buf.Writer, "500 internal error", "Requsted host is IP address",
			"COW can only record blocked site based on domain name.")
		return errInternal
	}
	addHost(host)

	// As there's no reliable way to convert an encoded URL back (you don't
	// know whether a %2F should be converted to slash or not, conside Google
	// search results for example), so I rely on the browser sending Referer
	// header to redirect the client back to the original web page.
	if r.Referer != "" {
		// debug.Println("Sending refirect page.")
		sendRedirectPage(c.buf.Writer, r.Referer)
		return
	}
	sendErrorPage(c.buf.Writer, "404 not found", "No Referer header",
		"Domain added to "+listType+" list, but no referer header in request so can't redirect.")
	return
}

func (c *clientConn) serveSelfURL(r *Request) (err error) {
	if r.Method != "GET" {
		goto end
	}
	if r.URL.Path == "/pac" {
		sendPAC(c.buf.Writer)
		// Send non nil error to close client connection.
		return errPageSent
	}
	if strings.HasPrefix(r.URL.Path, "/blocked?") {
		return c.serveSelfURLBlocked(r)
	}
	if strings.HasPrefix(r.URL.Path, "/direct?") {
		return c.serveSelfURLDirect(r)
	}

end:
	sendErrorPage(c.buf.Writer, "404 not found", "Page not found", "Handling request to proxy itself.")
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
			if err = c.serveSelfURL(r); err != nil {
				return
			}
			continue
		}

	retry:
		if h, err = c.getHandler(r); err != nil {
			// Failed connection will send error page back to client
			// debug.Printf("Failed to get handler for %s %v\n", c.RemoteAddr(), r)
			continue
		}

		if r.isConnect {
			if h.doConnect(r, c) == errRetry {
				goto retry
			}
			// Why return after doConnect:
			// 1. proxy can only know whether the request is finished when either
			// the server or the client close connection
			// 2. if the web server closes connection, the only way to
			// tell the client this is to close client connection (proxy
			// don't know the protocol between the client and server)

			// debug.Printf("doConnect for %s to %s done\n", c.RemoteAddr(), r.URL.Host)
			return
		}

		if err = h.doRequest(r, c); err != nil {
			c.removeHandler(h)
			if err == errPageSent {
				continue
			} else if err == errRetry {
				debug.Printf("retry request %v\n", r)
				goto retry
			}
			return
		}
	}
}

func genErrMsg(r *Request, what string) string {
	return fmt.Sprintf("<p>HTTP Request <strong>%v</strong></p> <p>%s</p>", r, what)
}

func genBlockedSiteMsg(r *Request) string {
	if !hostIsIP(r.URL.Host) {
		return fmt.Sprintf(
			"<p>Domain <strong>%s</strong> maybe blocked. Refresh to retry.</p>",
			host2Domain(r.URL.Host))
	}
	return ""
}

const (
	errCodeReset   = "502 connection reset"
	errCodeTimeout = "504 time out reading response"
)

func (c *clientConn) handleBlockedRequest(r *Request, err error, errCode, msg string) error {
	// Domain in chou domain set is likely to be blocked, should automatically
	// restart request using parent proxy.
	// Reset is usually reliable in detecting blocked site, so retry for connection reset.
	if errCode == errCodeReset || config.autoRetry || isHostChouFeng(r.URL.Host) {
		if addBlockedHost(r.URL.Host) {
			return errRetry
		}
		msg += genBlockedSiteMsg(r)
		sendErrorPage(c.buf.Writer, errCode, err.Error(), msg)
		return errPageSent
	}
	// If autoRetry is not enabled, let the user decide whether add the domain to blocked list.
	sendBlockedErrorPage(c.buf.Writer, errCode, err.Error(), msg, r)
	return errPageSent
}

func (c *clientConn) handleServerReadError(h *Handler, r *Request, err error, msg string) error {
	var errMsg string
	if err == io.EOF {
		debug.Println("Read from server EOF")
		return errRetry
	}
	if h.maybeFake() {
		errMsg = genErrMsg(r, msg)
		if ne, ok := err.(*net.OpError); ok {
			// GFW may connection reset here, may also make it time out Is it
			// normal for connection to a site timeout? If so, it's better not add
			// it to blocked site.
			if ne.Err == syscall.ECONNRESET {
				return c.handleBlockedRequest(r, err, errCodeReset, errMsg)
			}
			if ne.Timeout() {
				return c.handleBlockedRequest(r, err, errCodeTimeout, errMsg)
			}
			// fall through to send general error message
		}
	}
	errl.Printf("Read from server unhandled error for %v %v\n", r, err)
	sendErrorPage(c.buf.Writer, "502 read error", err.Error(), errMsg)
	return errPageSent
}

func (c *clientConn) handleServerWriteError(r *Request, h *Handler, err error, msg string) error {
	// This function is only called in doRequest, no response is sent to client.
	// So if visiting blocked site, can always retry request.
	if h.maybeFake() {
		errMsg := genErrMsg(r, msg)
		if ne, ok := err.(*net.OpError); ok {
			if ne.Err == syscall.ECONNRESET {
				return c.handleBlockedRequest(r, err, errCodeReset, errMsg)
			}
			// TODO What about broken PIPE?
		}
		sendErrorPage(c.buf.Writer, "502 write error", err.Error(), errMsg)
		return errPageSent
	}
	return errRetry
}

func (c *clientConn) handleClientReadError(r *Request, err error, msg string) error {
	if err == io.EOF {
		debug.Printf("%s client closed connection", msg)
	} else if ne, ok := err.(*net.OpError); ok {
		if ne.Err == syscall.ECONNRESET {
			debug.Printf("%s connection reset", msg)
		} else if ne.Timeout() {
			debug.Printf("%s client read timeout, maybe has closed\n", msg)
		}
	} else {
		errl.Printf("%s %v %v\n", msg, err, r)
	}
	return err
}

func (c *clientConn) handleClientWriteError(r *Request, err error, msg string) error {
	// Write to client error could be either broken pipe or connection reset
	if ne, ok := err.(*net.OpError); ok {
		if ne.Err == syscall.EPIPE {
			debug.Printf("%s broken pipe %v\n", msg, r)
		} else if ne.Err == syscall.ECONNRESET {
			debug.Println("%s connection reset %v\n", msg, r)
		}
	} else {
		errl.Printf("%s %v %v\n", msg, err, r)
	}
	return err
}

func isErrOpWrite(err error) bool {
	if ne, ok := err.(*net.OpError); ok && ne.Op == "write" {
		return true
	}
	return false
}

func (c *clientConn) readResponse(h *Handler, r *Request) (err error) {
	var rp *Response

	if h.maybeFake() && h.SetReadDeadline(time.Now().Add(readTimeout)) != nil {
		debug.Println("SetReadDeadline BEFORE receiving the first response")
	}
	if rp, err = parseResponse(h.buf.Reader); err != nil {
		return c.handleServerReadError(h, r, err, "Parse response from server.")
	}
	// After have received the first reponses from the server, we consider
	// ther server as real instead of fake one caused by wrong DNS reply. So
	// don't time out later.
	if h.maybeFake() && h.SetReadDeadline(time.Time{}) != nil {
		debug.Println("Un-SetReadDeadline AFTER receiving the first response")
	}
	h.setStateResponsReceived(r.URL.Host)

	if _, err = c.buf.WriteString(rp.raw.String()); err != nil {
		return c.handleClientWriteError(r, err, "Write response header back to client")
	}
	// Flush response header to the client ASAP
	if err = c.buf.Flush(); err != nil {
		return c.handleClientWriteError(r, err, "Flushing response header to client")
	}

	// Wrap inside if to avoid function argument evaluation.
	if dbgRep {
		dbgRep.Printf("%v %s %v %v", c.RemoteAddr(), r.Method, r.URL, rp)
	}

	if rp.hasBody(r.Method) {
		if err = sendBody(c.buf.Writer, nil, h.buf.Reader, rp.Chunking, rp.ContLen); err != nil {
			// Non persistent connection will return nil upon successful response reading
			if err == io.EOF {
				// For persistent connection, EOF from server is error.
				// Response header has been read, server using persistent
				// connection indicates the end of response and proxy should
				// not got EOF while reading response.
				// The client connection will be closed to indicate this error.
				// Proxy can't send error page here because response header has
				// been sent.
				errl.Println("Unexpected EOF reading body from server", r)
			} else if isErrOpWrite(err) {
				err = c.handleClientWriteError(r, err, "Write to client response body.")
			} else {
				err = c.handleServerReadError(h, r, err, "Read response body from server.")
			}
		} else {
			if err = c.buf.Flush(); err != nil {
				return c.handleClientWriteError(r, err, "Flushing response body to client")
			}
		}
	}
	/*
		if debug {
			debug.Printf("[Finished] %v request %s %s\n", c.RemoteAddr(), r.Method, r.URL)
		}
	*/

	if !rp.KeepAlive {
		c.removeHandler(h)
	} else {
		h.lastUse = time.Now()
	}
	return
}

func (c *clientConn) getHandler(r *Request) (h *Handler, err error) {
	h, ok := c.handler[r.URL.Host]
	if ok && h.mayBeClosed() {
		// debug.Printf("Connection to %s maybe closed\n", h.host)
		c.removeHandler(h)
		ok = false
	}
	if !ok {
		h, err = c.createHandler(r)
	}
	return
}

func (c *clientConn) removeHandler(h *Handler) {
	h.Close()
	delete(c.handler, h.host)
}

func createDirectConnection(host string) (conn, error) {
	c, err := net.DialTimeout("tcp", host, dialTimeout)
	if err != nil {
		// Time out is very likely to be caused by GFW
		debug.Printf("Connecting to: %s %v\n", host, err)
		return conn{nil, nilConn}, err
	}
	// debug.Println("Connected to", host)
	return conn{c, directConn}, nil
}

func isErrConnReset(err error) bool {
	ne, ok := err.(*net.OpError)
	if ok {
		return ne.Err == syscall.ECONNRESET
	}
	return false
}

func isErrTimeout(err error) bool {
	ne, ok := err.(*net.OpError)
	if ok {
		return ne.Timeout()
	}
	return false
}

func maybeBlocked(err error) bool {
	ne, ok := err.(*net.OpError)
	if ok {
		return ne.Timeout() || ne.Err == syscall.ECONNRESET
	}
	return false
}

const connFailedErrCode = "504 Connection failed"

func (c *clientConn) createHandler(r *Request) (*Handler, error) {
	var err error
	var srvconn conn
	connFailed := false

	if isHostBlocked(r.URL.Host) {
		// In case of connection error to socks server, fallback to direct connection
		if srvconn, err = createSocksConnection(r.URL.Host); err != nil {
			if isHostAlwaysBlocked(r.URL.Host) {
				connFailed = true
				goto connDone
			}
			if srvconn, err = createDirectConnection(r.URL.Host); err != nil {
				connFailed = true
				goto connDone
			}
		}
	} else {
		// In case of error on direction connection, try socks server
		if srvconn, err = createDirectConnection(r.URL.Host); err != nil {
			if isHostAlwaysDirect(r.URL.Host) || hostIsIP(r.URL.Host) {
				connFailed = true
				goto connDone
			}
			// debug.Printf("type of err %v\n", reflect.TypeOf(err))
			// GFW may cause dns lookup fail, may also cause connection time out or reset
			if _, ok := err.(*net.DNSError); ok {
			} else if maybeBlocked(err) {
			} else {
				connFailed = true
				goto connDone
			}
			// Try to create socks connection
			var socksErr error
			if srvconn, socksErr = createSocksConnection(r.URL.Host); socksErr != nil {
				connFailed = true
				goto connDone
			}
			// If socks connection succeeds, it's very likely blocked
			if config.autoRetry || isHostChouFeng(r.URL.Host) || isErrConnReset(err) {
				addBlockedHost(r.URL.Host)
			} else {
				srvconn.Close()
				sendBlockedErrorPage(c.buf.Writer, connFailedErrCode, err.Error(),
					genErrMsg(r, "Connection failed.")+genBlockedSiteMsg(r), r)
				return nil, errPageSent
			}
		}
	}

connDone:
	if connFailed {
		sendErrorPage(c.buf.Writer, connFailedErrCode, err.Error(),
			genErrMsg(r, "Connection failed."))
		return nil, errPageSent
	}

	h := newHandler(srvconn, r.URL.Host)
	if r.isConnect {
		// Don't put connection for CONNECT method for reuse
		return h, nil
	}
	c.handler[h.host] = h
	// client will connect to differnet servers in a single proxy connection
	// debug.Printf("handler to for client %v %v\n", c.RemoteAddr(), c.handler)
	return h, nil
}

func copyServer2Client(h *Handler, c *clientConn, cliStopped notification, r *Request) (err error) {
	buf := make([]byte, bufSize)
	var n int

	for {
		if cliStopped.hasNotified() {
			debug.Println("copyServer2Client client has stopped")
			return
		}
		if err = h.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
			debug.Println("copyServer2Client set ReadDeadline:", err)
		}
		if n, err = h.buf.Read(buf); err != nil {
			if h.maybeFake() && maybeBlocked(err) && addBlockedHost(r.URL.Host) {
				debug.Printf("copyServer2Client blocked site %s detected, retry\n", r.URL.Host)
				return errRetry
			} else if isErrTimeout(err) {
				// copyClient2Server will close the connection and notify cliStopped
				continue
			}
			debug.Printf("copyServer2Client read data: %v\n", err)
			return
		}
		if _, err = c.Write(buf[0:n]); err != nil {
			debug.Printf("copyServer2Client write data: %v\n", err)
			return
		}
		h.setStateResponsReceived(r.URL.Host)
	}
	return
}

func copyClient2Server(c *clientConn, h *Handler, srvStopped notification, r *Request) (err error) {
	buf := make([]byte, bufSize)
	var n int
	var timeout time.Time

	if r.contBuf != nil {
		debug.Println("copyClient2Server retry request:", r)
		if _, err = h.Write(r.contBuf.Bytes()); err != nil {
			debug.Println("copyClient2Server send to server error")
			return
		}
	}
	for {
		if srvStopped.hasNotified() {
			debug.Println("copyClient2Server server stopped")
			return
		}
		if h.maybeFake() {
			timeout = time.Now().Add(3 * time.Second)
		} else {
			timeout = time.Now().Add(readTimeout)
		}
		if err = c.SetReadDeadline(timeout); err != nil {
			debug.Println("copyClient2Server set ReadDeadline:", err)
		}
		if n, err = c.buf.Read(buf); err != nil {
			if isErrTimeout(err) {
				// close connection upon timeout to avoid too many open connections.
				if !h.maybeFake() {
					return
				}
				continue
			} else if err != io.EOF {
				debug.Printf("copyClient2Server read data: %v\n", err)
				return
			}
			return
		}
		// When retrying request, should use socks server. So maybeFake
		// will return false. Better to make sure about this.
		if h.maybeFake() {
			if r.contBuf == nil {
				r.contBuf = new(bytes.Buffer)
			}
			// store client data in case needing retry
			r.contBuf.Write(buf[0:n])
		}
		// Read is using buffer, so write what have been read directly
		if _, err = h.Write(buf[0:n]); err != nil {
			if h.maybeFake() && isErrConnReset(err) && addBlockedHost(r.URL.Host) {
				debug.Printf("copyClient2Server blocked site %d detected, retry\n", r.URL.Host)
				return errRetry
			}
			debug.Printf("copyClient2Server write data: %v\n", err)
			return
		}
	}
	return
}

func (h *Handler) maybeFake() bool {
	// GFW may return wrong DNS record, which we can connect to but block
	// forever on read. (e.g. twitter.com) If we have never received any
	// response yet, then we should set a timeout for read/write.
	return h.state == hsConnected && h.connType == directConn &&
		!isHostAlwaysDirect(h.host)
}

// Apache 2.2 keep-alive timeout defaults to 5 seconds.
const serverConnTimeout = 5 * time.Second

func (h *Handler) mayBeClosed() bool {
	if h.connType == socksConn {
		return false
	}
	return time.Now().Sub(h.lastUse) > serverConnTimeout
}

func (h *Handler) setStateResponsReceived(host string) {
	if h.state == hsConnected {
		h.state = hsResponsReceived
		if h.connType == directConn {
			addDirectHost(host)
		}
	}
}

var connEstablished = []byte("HTTP/1.0 200 Connection established\r\nProxy-agent: cow-proxy/0.1\r\n\r\n")

// Do HTTP CONNECT
func (h *Handler) doConnect(r *Request, c *clientConn) (err error) {
	defer h.Close()
	if debug {
		debug.Printf("%v 200 Connection established to %s\n", c.RemoteAddr(), r.URL.Host)
	}
	if _, err = c.Write(connEstablished); err != nil {
		errl.Printf("%v Error sending 200 Connecion established\n", c.RemoteAddr())
		return err
	}

	// Notify the destination has stopped in copyData is important. If the
	// client has stopped connection, while the server->client is blocked
	// reading data from the server, the server connection will not get chance
	// to stop (unless there's timeout in read). This may result too many open
	// connection error from the socks server.
	srvStopped := newNotification()
	cliStopped := newNotification()

	// Must wait this goroutine finish before returning from this function.
	// Otherwise, the server/client may have been closed and thus cause nil
	// pointer deference
	var srv2CliErr error
	doneChan := make(chan byte)
	go func() {
		srv2CliErr = copyServer2Client(h, c, cliStopped, r)
		srvStopped.notify()
		doneChan <- 1
	}()

	err = copyClient2Server(c, h, srvStopped, r)
	cliStopped.notify()

	<-doneChan
	// maybeFake is needed here. If server has sent response to client, should not retry.
	if h.maybeFake() && (err == errRetry || srv2CliErr == errRetry) {
		return errRetry
	}
	// Because this is in doConnect, there's no way reporting error to the client.
	// Just return and close connectino.
	return
}

// Do HTTP request other that CONNECT
func (h *Handler) doRequest(r *Request, c *clientConn) (err error) {
	// Send request to the server
	if _, err = h.buf.Write(r.raw.Bytes()); err != nil {
		// The srv connection maybe already closed.
		// Need to delete the connection and reconnect in that case.
		return c.handleServerWriteError(r, h, err, "Sending request header")
	}
	if h.buf.Writer.Flush() != nil {
		return c.handleServerWriteError(r, h, err, "Flushing request header")
	}

	// Send request body
	if r.contBuf != nil {
		debug.Println("Retry request send buffered body:", r)
		if _, err = h.buf.Write(r.contBuf.Bytes()); err != nil {
			sendErrorPage(h.buf.Writer, "502 send request error", err.Error(),
				"Send retry request body")
			r.contBuf = nil // let gc recycle memory earlier
			return errPageSent
		}
	} else if r.Method == "POST" {
		r.contBuf = new(bytes.Buffer)
		if err = sendBody(h.buf.Writer, r.contBuf, c.buf.Reader, r.Chunking, r.ContLen); err != nil {
			if err == io.EOF {
				if r.KeepAlive {
					errl.Println("Unexpected EOF reading request body from client", r)
				} else {
					err = nil
				}
			} else if isErrOpWrite(err) {
				err = c.handleServerWriteError(r, h, err, "Sending request body")
			} else {
				errl.Println("Reading request body:", err)
			}
			return
		}
	}
	if err = h.buf.Writer.Flush(); err != nil {
		if r.contBuf != nil {
			sendErrorPage(h.buf.Writer, "502 send request error", err.Error(),
				"Flush retry request")
			r.contBuf = nil
			return errPageSent
		}
		return c.handleServerWriteError(r, h, err, "Flushing request body")
	}

	return c.readResponse(h, r)
}

// Send response body if header specifies content length
func sendBodyWithContLen(w, contBuf io.Writer, r *bufio.Reader, contLen int64) (err error) {
	// debug.Println("Sending body with content length", contLen)
	if contLen == 0 {
		return
	}
	// CopyN will copy n bytes unless there's error of EOF. For EOF, it means
	// the connection is closed, return will propagate till serv function and
	// close client connection.
	if contBuf == nil {
		if _, err = io.CopyN(w, r, contLen); err != nil {
			debug.Println("Send response body", err)
			return err
		}
	} else {
		copyWithBuf(w, contBuf, r, contLen, "Read response body:", "Write response body:")
	}
	return
}

func copyWithBuf(w, contBuf io.Writer, r io.Reader, size int64, rMsg, wMsg string) {
	buf := make([]byte, size)
	if _, err := r.Read(buf); err != nil {
		errl.Println(rMsg, err)
		return
	}
	contBuf.Write(buf)
	if _, err := w.Write(buf); err != nil {
		errl.Println(wMsg, err)
		return
	}
}

// Send response body if header specifies chunked encoding
func sendBodyChunked(w, contBuf io.Writer, r *bufio.Reader) (err error) {
	// debug.Println("Sending chunked body")
	done := false
	for !done {
		var s string
		// Read chunk size line, ignore chunk extension if any
		if s, err = ReadLine(r); err != nil {
			errl.Println("Reading chunk size:", err)
			return
		}
		if contBuf != nil {
			io.WriteString(contBuf, s+"\r\n")
		}
		// debug.Println("Chunk size line", s)
		f := strings.SplitN(s, ";", 2)
		var size int64
		if size, err = strconv.ParseInt(strings.TrimSpace(f[0]), 16, 64); err != nil {
			errl.Println("Chunk size not valid:", err)
			return
		}
		if _, err = io.WriteString(w, s+"\r\n"); err != nil {
			errl.Println("Writing chunk size:", err)
			return
		}

		if size == 0 { // end of chunked data, ignore any trailers
			done = true
		} else if contBuf == nil {
			// Read chunk data and send to client
			if _, err = io.CopyN(w, r, size); err != nil {
				errl.Println("Copy chunked data:", err)
				return
			}
		} else {
			copyWithBuf(w, contBuf, r, size, "Read chunked data:", "Write chunked data:")
		}

		if err = readCheckCRLF(r); err != nil {
			errl.Println("Reading chunked data CRLF:", err)
			return
		}
		if contBuf != nil {
			io.WriteString(contBuf, "\r\n")
		}
		if _, err = io.WriteString(w, "\r\n"); err != nil {
			errl.Println("Writing end line in sendBodyChunked:", err)
			return
		}
	}
	return
}

func sendBodySplitIntoChunk(w, contBuf io.Writer, r *bufio.Reader) (err error) {
	buf := make([]byte, bufSize)
	var n int
	for {
		n, err = r.Read(buf)
		// debug.Println("split into chunk n =", n, "err =", err)
		if err != nil {
			io.WriteString(w, "0\r\n\r\n")
			if contBuf != nil {
				io.WriteString(contBuf, "0\r\n\r\n")
			}
			// EOF is expected here as the server is closing connection.
			if err == io.EOF {
				err = nil
			}
			return
		}

		sizeStr := fmt.Sprintf("%x\r\n", n)
		if contBuf != nil {
			io.WriteString(contBuf, sizeStr)
			contBuf.Write(buf[:n])
		}
		if _, err = io.WriteString(w, sizeStr); err != nil {
			errl.Printf("Writing chunk size %v\n", err)
			return
		}
		if _, err = w.Write(buf[:n]); err != nil {
			errl.Printf("Writing chunk %v\n", err)
			return
		}
	}
	return
}

// Send message body
func sendBody(w, contBuf io.Writer, r *bufio.Reader, chunk bool, contLen int64) (err error) {
	if chunk {
		err = sendBodyChunked(w, contBuf, r)
	} else if contLen >= 0 {
		err = sendBodyWithContLen(w, contBuf, r, contLen)
	} else {
		err = sendBodySplitIntoChunk(w, contBuf, r)
	}
	return
}

func hostIsIP(host string) bool {
	host, _ = splitHostPort(host)
	return net.ParseIP(host) != nil
}
