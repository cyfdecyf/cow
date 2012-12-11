package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// What value is appropriate?
const readTimeout = 15 * time.Second
const dialTimeout = 10 * time.Second
const clientConnTimeout = 15 * time.Second
const sslLeastDuration = time.Second

// Some code are learnt from the http package

type Proxy struct {
	addr string // listen address
}

type connType byte

const (
	nilConn connType = iota
	directConn
	socksConn
	shadowSocksConn
)

type handlerState byte

const (
	hsConnected handlerState = iota
	hsSendRecvResponse
	hsStopped
)

type conn struct {
	net.Conn
	connType
}

var zeroConn = conn{}
var zeroTime = time.Time{}

// For both client and server connection, there's only read buffer. If we
// create write buffer for the connection and pass it to io.CopyN, there will
// be unnecessary copies: from read buffer to tmp buffer, then call io.Write.
//
// As HTTP uses TCP connection and net.TCPConn implements ReadFrom, io.CopyN
// can avoid unnecessary use the connection directly.
// There maybe more call to write, but the avoided copy should benefit more.

// TODO rename this to serverConn
type Handler struct {
	conn
	buf     *bufio.Reader
	host    string
	state   handlerState
	lastUse time.Time
}

func newHandler(c conn, host string) *Handler {
	return &Handler{
		conn: c,
		host: host,
		buf:  bufio.NewReaderSize(c, bufSize),
	}
}

type clientConn struct {
	net.Conn // connection to the proxy client
	buf      *bufio.Reader
	handler  map[string]*Handler // request handler, host:port as key
}

var (
	errRetry         = errors.New("Retry")
	errPageSent      = errors.New("Error page has sent")
	errShouldClose   = errors.New("Error can only be handled by close connection")
	errInternal      = errors.New("Internal error")
	errNoParentProxy = errors.New("No parent proxy")

	errChunkedEncode = errors.New("Invalid chunked encoding")
	errMalformHeader = errors.New("Malformed HTTP header")
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
		buf:     bufio.NewReaderSize(rwc, bufSize),
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
}

func isSelfURL(h string) bool {
	return h == "" || h == selfURLLH || h == selfURL127
}

func (c *clientConn) getRequest() (r *Request) {
	var err error

	setConnReadTimeout(c, clientConnTimeout, "BEFORE receiving client request")
	if r, err = parseRequest(c.buf); err != nil {
		c.handleClientReadError(r, err, "parse client request")
		return nil
	}
	unsetConnReadTimeout(c, "AFTER receiving client request")
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
		sendErrorPage(c, "500 internal error", "Requsted host is IP address",
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
		sendRedirectPage(c, r.Referer)
		return
	}
	sendErrorPage(c, "404 not found", "No Referer header",
		"Domain added to "+listType+" list, but no referer header in request so can't redirect.")
	return
}

func (c *clientConn) serveSelfURL(r *Request) (err error) {
	if r.Method != "GET" {
		goto end
	}
	if r.URL.Path == "/pac" {
		sendPAC(c)
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
	sendErrorPage(c, "404 not found", "Page not found", "Handling request to proxy itself.")
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
			if err = h.doConnect(r, c); err == errRetry {
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
			"<p>Domain <strong>%s</strong> maybe blocked.</p>",
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
		debug.Printf("Blocked site %s detected for request %v error: %v\n", r.URL.Host, r, err)
		if addBlockedHost(r.URL.Host) {
			return errRetry
		}
		msg += genBlockedSiteMsg(r)
		if r.responseNotSent() {
			sendErrorPage(c, errCode, err.Error(), msg)
			return errPageSent
		}
	}
	if r.responseNotSent() {
		// If autoRetry is not enabled, let the user decide whether add the domain to blocked list.
		sendBlockedErrorPage(c, errCode, err.Error(), msg, r)
		return errPageSent
	}
	errl.Println("blocked request with partial response sent to client:", err, r)
	return errShouldClose
}

func (c *clientConn) handleServerReadError(r *Request, h *Handler, err error, msg string) error {
	var errMsg string
	if err == io.EOF {
		if r.responseNotSent() {
			debug.Println("Read from server EOF, retry")
			return errRetry
		}
		errl.Println("Read from server EOF, partial data sent to client")
		return errShouldClose
	}
	errMsg = genErrMsg(r, msg)
	if h.maybeFake() || isErrConnReset(err) {
		// GFW may connection reset when reading from  server, may also make
		// it time out. But timeout is also normal if network condition is
		// bad, so should treate timeout separately.
		if isErrConnReset(err) {
			return c.handleBlockedRequest(r, err, errCodeReset, errMsg)
		}
		if isErrTimeout(err) {
			return c.handleBlockedRequest(r, err, errCodeTimeout, errMsg)
		}
		// fall through to send general error message
	}
	if r.responseNotSent() {
		sendErrorPage(c, "502 read error", err.Error(), errMsg)
		return errPageSent
	}
	errl.Println("Unhandled server read error:", err, r)
	return errShouldClose
}

func (c *clientConn) handleServerWriteError(r *Request, h *Handler, err error, msg string) error {
	// This function is only called in doRequest, no response is sent to client.
	// So if visiting blocked site, can always retry request.
	if h.maybeFake() || isErrConnReset(err) {
		errMsg := genErrMsg(r, msg)
		if ne, ok := err.(*net.OpError); ok {
			if ne.Err == syscall.ECONNRESET {
				return c.handleBlockedRequest(r, err, errCodeReset, errMsg)
			}
			// TODO What about broken PIPE?
		}
		sendErrorPage(c, "502 write error", err.Error(), errMsg)
		return errPageSent
	}
	return errRetry
}

func (c *clientConn) handleClientReadError(r *Request, err error, msg string) error {
	if err == io.EOF {
		debug.Printf("%s client closed connection", msg)
	} else if ne, ok := err.(*net.OpError); ok {
		if debug {
			if ne.Err == syscall.ECONNRESET {
				debug.Printf("%s connection reset", msg)
			} else if ne.Timeout() {
				debug.Printf("%s client read timeout, maybe has closed\n", msg)
			}
		}
	} else {
		// TODO is this possible?
		errl.Printf("handleClientReadError: %s %v %v\n", msg, err, r)
	}
	return err
}

func (c *clientConn) handleClientWriteError(r *Request, err error, msg string) error {
	// Write to client error could be either broken pipe or connection reset
	if ne, ok := err.(*net.OpError); ok {
		if debug {
			if ne.Err == syscall.EPIPE {
				debug.Printf("%s broken pipe %v\n", msg, r)
			} else if ne.Err == syscall.ECONNRESET {
				debug.Println("%s connection reset %v\n", msg, r)
			}
		}
	} else {
		// TODO is this possible?
		errl.Printf("handleClientWriteError: %s %v %v\n", msg, err, r)
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

	if h.state == hsConnected && h.maybeFake() {
		setConnReadTimeout(h, readTimeout, "BEFORE receiving the first response")
	}
	if rp, err = parseResponse(h.buf, r); err != nil {
		return c.handleServerReadError(r, h, err, "Parse response from server.")
	}
	// After have received the first reponses from the server, we consider
	// ther server as real instead of fake one caused by wrong DNS reply. So
	// don't time out later.
	if h.state == hsConnected && h.maybeFake() {
		unsetConnReadTimeout(h, "AFTER receiving the first response")
	}
	if h.state == hsConnected {
		if h.connType == directConn {
			addDirectHost(h.host)
		}
		h.state = hsSendRecvResponse
	}

	if _, err = c.Write(rp.raw.Bytes()); err != nil {
		return c.handleClientWriteError(r, err, "Write response header back to client")
	}

	// Wrap inside if to avoid function argument evaluation.
	if dbgRep {
		dbgRep.Printf("%v %s %v %v", c.RemoteAddr(), r.Method, r.URL, rp)
	}

	if rp.hasBody(r.Method) {
		r.state = rsRecvBody
		if err = sendBody(c, nil, h.buf, rp.Chunking, rp.ContLen); err != nil {
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
				err = c.handleServerReadError(r, h, err, "Read response body from server.")
			}
		}
	}
	r.state = rsDone
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

func createParentProxyConnection(host string) (srvconn conn, err error) {
	// Try shadowsocks server first
	if hasShadowSocksServer {
		if srvconn, err = createShadowSocksConnection(host); err == nil {
			return
		}
	}
	if hasSocksServer {
		if srvconn, err = createSocksConnection(host); err == nil {
			return
		}
	}
	return zeroConn, errNoParentProxy
}

func (c *clientConn) createConnection(host string, r *Request) (srvconn conn, err error) {
	if config.alwaysProxy {
		if srvconn, err = createParentProxyConnection(host); err == nil {
			return
		}
		goto fail
	}
	if isHostBlocked(host) && hasParentProxy {
		// In case of connection error to socks server, fallback to direct connection
		if srvconn, err = createParentProxyConnection(host); err == nil {
			return
		}
		if isHostAlwaysBlocked(host) {
			goto fail
		}
		if srvconn, err = createDirectConnection(host); err == nil {
			return
		}
	} else {
		// In case of error on direction connection, try socks server
		if srvconn, err = createDirectConnection(host); err == nil {
			return
		}
		if isHostAlwaysDirect(host) || hostIsIP(host) || !hasParentProxy {
			goto fail
		}
		// debug.Printf("type of err %v\n", reflect.TypeOf(err))
		// GFW may cause dns lookup fail, may also cause connection time out or reset
		if _, ok := err.(*net.DNSError); ok || maybeBlocked(err) {
			// Try to create socks connection
			var socksErr error
			if srvconn, socksErr = createParentProxyConnection(host); socksErr == nil {
				// Connection reset is very likely caused by GFW, directly add
				// to blocked list. Timeout error is not reliable detecting
				// blocked site, ask the user unless autoRetry is enabled.
				if config.autoRetry || isHostChouFeng(host) || isErrConnReset(err) {
					addBlockedHost(host)
					return srvconn, nil
				}
				srvconn.Close()
				sendBlockedErrorPage(c, connFailedErrCode, err.Error(),
					genErrMsg(r, "Connection failed.")+genBlockedSiteMsg(r), r)
				return zeroConn, errPageSent
			}
		}
	}

fail:
	debug.Printf("Failed to connect to %s %s", host, r)
	if r.Method != "CONNECT" {
		sendErrorPage(c, connFailedErrCode, err.Error(),
			genErrMsg(r, "Connection failed."))
	}
	return zeroConn, errPageSent
}

func (c *clientConn) createHandler(r *Request) (*Handler, error) {
	srvconn, err := c.createConnection(r.URL.Host, r)
	if err != nil {
		return nil, err
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

func setConnReadTimeout(cn net.Conn, d time.Duration, msg string) {
	if cn.SetReadDeadline(time.Now().Add(d)) != nil {
		errl.Println("Set readtimeout:", msg)
	}
}

func unsetConnReadTimeout(cn net.Conn, msg string) {
	if cn.SetReadDeadline(zeroTime) != nil {
		errl.Println("Unset readtimeout:", msg)
	}
}

func copyServer2Client(h *Handler, c *clientConn, cliStopped notification, r *Request) (err error) {
	buf := make([]byte, bufSize)
	var n int

	for {
		if cliStopped.hasNotified() {
			debug.Println("copyServer2Client client has stopped")
			return
		}
		setConnReadTimeout(h, readTimeout, "copyServer2Client")
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
		// set state to rsRecvBody to indicate the request has partial response sent to client
		r.state = rsRecvBody
		if h.state == hsConnected {
			if h.connType == directConn {
				addDirectHost(h.host)
			}
			h.state = hsSendRecvResponse
		}
	}
	return
}

func copyClient2Server(c *clientConn, h *Handler, srvStopped notification, r *Request) (err error) {
	buf := make([]byte, bufSize)
	var n int
	var timeout time.Duration

	if r.contBuf != nil {
		debug.Println("copyClient2Server retry request:", r)
		if _, err = h.Write(r.contBuf.Bytes()); err != nil {
			debug.Println("copyClient2Server send to server error")
			return
		}
	}
	start := time.Now()
	for {
		if srvStopped.hasNotified() {
			debug.Println("copyClient2Server server stopped")
			return
		}
		if h.maybeFake() && h.state == hsConnected {
			timeout = 2 * time.Second
		} else {
			timeout = readTimeout
		}
		setConnReadTimeout(c, timeout, "copyClient2Server")
		if n, err = c.buf.Read(buf); err != nil {
			if isErrTimeout(err) {
				// Applications like Twitter for Mac needs long connection for
				// live stream. So should not close connection here. But this
				// will have the risk that socks server will report too many
				// open connections.
				continue
			}
			if config.detectSSLErr && (isErrConnReset(err) || err == io.EOF) && h.maybeSSLErr(start) {
				debug.Println("client connection closed very soon, taken as SSL error:", r)
				addBlockedHost(r.URL.Host)
			}
			debug.Printf("copyClient2Server read data: %v\n", err)
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
	// forever on read. (e.g. twitter.com)
	return h.connType == directConn && !isHostAlwaysDirect(h.host)
}

func (h *Handler) maybeSSLErr(cliStart time.Time) bool {
	// If client closes connection very soon, maybe there's SSL error, maybe
	// not (e.g. user stopped request).
	// COW can't tell which is the case, so this detection is not reliable.
	return h.state > hsConnected && time.Now().Sub(cliStart) < sslLeastDuration
}

// Apache 2.2 keep-alive timeout defaults to 5 seconds.
const serverConnTimeout = 5 * time.Second

func (h *Handler) mayBeClosed() bool {
	if h.connType == socksConn {
		return false
	}
	return time.Now().Sub(h.lastUse) > serverConnTimeout
}

var connEstablished = []byte("HTTP/1.0 200 Connection established\r\nProxy-agent: cow-proxy\r\n\r\n")

// Do HTTP CONNECT
func (h *Handler) doConnect(r *Request, c *clientConn) (err error) {
	r.state = rsCreated
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
	r.state = rsCreated
	// Send request to the server
	if _, err = h.Write(r.raw.Bytes()); err != nil {
		// The srv connection maybe already closed.
		// Need to delete the connection and reconnect in that case.
		return c.handleServerWriteError(r, h, err, "Sending request header")
	}

	// Send request body
	if r.contBuf != nil {
		debug.Println("Retry request send buffered body:", r)
		if _, err = h.Write(r.contBuf.Bytes()); err != nil {
			sendErrorPage(h, "502 send request error", err.Error(),
				"Send retry request body")
			r.contBuf = nil // let gc recycle memory earlier
			return errPageSent
		}
	} else if r.Method == "POST" {
		r.contBuf = new(bytes.Buffer)
		if err = sendBody(h, r.contBuf, c.buf, r.Chunking, r.ContLen); err != nil {
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
	r.state = rsSent
	return c.readResponse(h, r)
}

// Send response body if header specifies content length
func sendBodyWithContLen(w, contBuf io.Writer, r *bufio.Reader, contLen int64) (err error) {
	// debug.Println("Sending body with content length", contLen)
	if contLen == 0 {
		return
	}
	if err = copyWithBuf(w, contBuf, r, contLen); err != nil {
		debug.Println("sendBodyWithContLen:", err)
	}
	return
}

func copyWithBuf(w, contBuf io.Writer, r io.Reader, size int64) (err error) {
	if contBuf == nil {
		_, err = io.CopyN(w, r, size)
	} else {
		buf := make([]byte, size, size)
		if _, err = r.Read(buf); err != nil {
			return
		}
		contBuf.Write(buf)
		_, err = w.Write(buf)
	}
	return
}

// Send response body if header specifies chunked encoding
func sendBodyChunked(w, contBuf io.Writer, r *bufio.Reader) (err error) {
	// debug.Println("Sending chunked body")
	for {
		var s string
		// Read chunk size line, ignore chunk extension if any
		if s, err = ReadLine(r); err != nil {
			errl.Println("Reading chunk size:", err)
			return
		}
		// debug.Println("Chunk size line", s)
		f := strings.SplitN(s, ";", 2)
		var size int64
		if size, err = strconv.ParseInt(strings.TrimSpace(f[0]), 16, 64); err != nil {
			errl.Println("Chunk size not valid:", err)
			return
		}
		sb := []byte(s + "\r\n")
		if contBuf != nil {
			contBuf.Write(sb)
		}
		// Write chunk size, not buffered
		if _, err = w.Write(sb); err != nil {
			debug.Println("Writing chunk size in sendBodyChunked:", err)
			return
		}
		if size == 0 { // end of chunked data, ignore any trailers
			// Send final blank line to client
			if err = copyWithBuf(w, contBuf, r, 2); err != nil {
				debug.Println("sendBodyChunked send ending CRLF:", err)
			}
			return
		}
		// If we assume correct data from read side, we can use size+2 to read
		// chunked data and the followed CRLF. Though we should be strict on
		// input data, almost all server on the internet implements chunked
		// encoding correctly, this assumption should almost always hold. In
		// the rare case of wrong data, parsing the followed chunk size line
		// may discover error and stop.
		if err = copyWithBuf(w, contBuf, r, size+2); err != nil {
			debug.Println("sendBodyChunked copying chunk data:", err)
			return
		}
	}
	return
}

var CRLFbytes = []byte("\r\n")
var chunkEndbytes = []byte("0\r\n\r\n")

// Client can't use closed connection to indicate end of request, so must
// content length or use chunked encoding. Thus contBuf is not necessary for
// sendBodySplitIntoChunk.
func sendBodySplitIntoChunk(w io.Writer, r *bufio.Reader) (err error) {
	buf := make([]byte, bufSize)
	var n int
	for {
		n, err = r.Read(buf)
		// debug.Println("split into chunk n =", n, "err =", err)
		if err != nil {
			if err == io.EOF {
				// EOF is expected here as the server is closing connection.
				// debug.Println("end chunked encoding")
				if _, err = w.Write(chunkEndbytes); err != nil {
					debug.Println("Write chunk end 0")
				}
				return
			}
			debug.Println("Reading error in sendBodySplitIntoChunk", err)
			return
		}

		sb := []byte(fmt.Sprintf("%x\r\n", n))
		buf = append(buf[:n], CRLFbytes...)
		n += 2
		if _, err = w.Write(sb); err != nil {
			debug.Printf("Writing chunk size %v\n", err)
			return
		}
		if _, err = w.Write(buf[:n]); err != nil {
			debug.Println("Writing chunk data:", err)
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
		if contBuf != nil {
			errl.Println("Should not happen! Client request with body but no length specified.")
		}
		err = sendBodySplitIntoChunk(w, r)
	}
	return
}

func hostIsIP(host string) bool {
	host, _ = splitHostPort(host)
	return net.ParseIP(host) != nil
}
