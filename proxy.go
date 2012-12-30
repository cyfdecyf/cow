package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	// "reflect"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// What value is appropriate?
const readTimeout = 5 * time.Second
const dialTimeout = 5 * time.Second
const clientConnTimeout = 15 * time.Second
const sslLeastDuration = time.Second

// Some code are learnt from the http package

type Proxy struct {
	addr string // listen address
}

type connType byte

const (
	ctNilConn connType = iota
	ctDirectConn
	ctSocksConn
	ctShadowctSocksConn
)

type serverConnState byte

const (
	svConnected serverConnState = iota
	svSendRecvResponse
	svStopped
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

// net.Conn implements ReadFrom, but it only works if the src is a regular
// file. io.Copy will try ReadFrom first and fallback to generic copy if src
// is not a regular file, introducing unnecessary overhead.
// Export only the write interface can avoid this try and fallback. Learnt
// from net/sock.go
type writerOnly struct {
	io.Writer
}

type serverConn struct {
	conn
	bufRd   *bufio.Reader
	host    string
	state   serverConnState
	lastUse time.Time
}

func newServerConn(c conn, host string) *serverConn {
	return &serverConn{
		conn:  c,
		host:  host,
		bufRd: bufio.NewReaderSize(c, bufSize),
	}
}

type clientConn struct {
	net.Conn   // connection to the proxy client
	bufRd      *bufio.Reader
	serverConn map[string]*serverConn // request serverConn, host:port as key
	buf        []byte                 // buffer for reading request, avoids repeatedly allocating buffer
}

var (
	errRetry             = errors.New("Retry")
	errPageSent          = errors.New("Error page has sent")
	errShouldClose       = errors.New("Error can only be handled by close connection")
	errInternal          = errors.New("Internal error")
	errNoParentProxy     = errors.New("No parent proxy")
	errFailedParentProxy = errors.New("Failed connecting to parent proxy")

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
	info.Printf("COW proxy address %s, PAC url %s\n", py.addr, "http://"+py.addr+"/pac")

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
		Conn:       rwc,
		serverConn: map[string]*serverConn{},
		bufRd:      bufio.NewReaderSize(rwc, bufSize),
	}
	return c
}

func (c *clientConn) close() {
	for _, sv := range c.serverConn {
		sv.Close()
	}
	c.Close()
	if debug {
		debug.Printf("Client %v connection closed\n", c.RemoteAddr())
	}
	c.buf = nil
}

func isSelfURL(url string) bool {
	return url == "" || url == selfURLLH || url == selfURL127
}

func (c *clientConn) getRequest() (r *Request) {
	var err error

	setConnReadTimeout(c, clientConnTimeout, "BEFORE receiving client request")
	if r, err = parseRequest(c.bufRd); err != nil {
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
	return c.serveSelfURLAddHost(r, query, "blocked", (*DomainSet).addBlockedHost)
}

func (c *clientConn) serveSelfURLDirect(r *Request) (err error) {
	query := r.URL.Path[8:] // "/direct?" has 9 characters
	return c.serveSelfURLAddHost(r, query, "direct", (*DomainSet).addDirectHost)
}

type addHostFunc func(*DomainSet, string) bool

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
	addHost(domainSet, host)

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
	if r.URL.Path == "/pac" || strings.HasPrefix(r.URL.Path, "/pac?") {
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
	var sv *serverConn

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
		if r.tryCnt > 5 {
			debug.Println("Retry too many times, abort")
			if r.isConnect {
				return
			}
			sendErrorPage(c, "502 retry failed", "Can't finish HTTP request",
				genErrMsg(r, "Has retried several times."))
			continue
		}
		r.tryCnt++
		if sv, err = c.getServerConn(r); err != nil {
			// Failed connection will send error page back to client
			// debug.Printf("Failed to get serverConn for %s %v\n", c.RemoteAddr(), r)
			if err == errPageSent {
				continue
			}
			return
		}

		if r.isConnect {
			if err = sv.doConnect(r, c); err == errRetry {
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

		if err = sv.doRequest(r, c); err != nil {
			c.removeServerConn(sv)
			if err == errPageSent {
				continue
			} else if err == errRetry {
				debug.Printf("retry request %v\n", r)
				goto retry
			}
			return
		}

		if !r.KeepAlive {
			// debug.Println("close client connection because request has no keep-alive")
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

func (c *clientConn) handleBlockedRequest(r *Request, err error, msg string) error {
	var errCode string
	if isErrConnReset(err) {
		if domainSet.addBlockedHost(r.URL.Host) {
			return errRetry
		}
		errCode = errCodeReset
	} else if domainSet.isHostChouFeng(r.URL.Host) || (isErrTimeout(err) && config.AutoRetry) {
		// Domain in chou domain set is likely to be blocked, should automatically
		// restart request using parent proxy.
		// If autoRetry is enabled, treat timeout domain as chou and retry.
		if domainSet.addChouHost(r.URL.Host) {
			return errRetry
		}
		errCode = errCodeTimeout
	}

	msg += genBlockedSiteMsg(r)
	// Let user decide what to do with with timeout error if autoRetry is not enabled
	if !config.AutoRetry && errCode == errCodeTimeout && r.responseNotSent() {
		sendBlockedErrorPage(c, errCode, err.Error(), msg, r)
		return errPageSent
	}
	if r.responseNotSent() {
		sendErrorPage(c, errCode, err.Error(), msg)
		return errPageSent
	}
	errl.Printf("%s blocked request with partial response sent to client: %v %v\n", msg, err, r)
	return errShouldClose
}

func (c *clientConn) handleServerReadError(r *Request, sv *serverConn, err error, msg string) error {
	var errMsg string
	if err == io.EOF {
		if r.responseNotSent() {
			debug.Println("Read from server EOF, retry")
			return errRetry
		}
		errl.Printf("%s read EOF with partial data sent to client %v\n", msg, r)
		return errShouldClose
	}
	errMsg = genErrMsg(r, msg)
	if sv.maybeFake() || isErrConnReset(err) {
		// GFW may connection reset when reading from  server, may also make
		// it time out. But timeout is also normal if network condition is
		// bad, so should treate timeout separately.
		if maybeBlocked(err) {
			return c.handleBlockedRequest(r, err, errMsg)
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

func (c *clientConn) handleServerWriteError(r *Request, sv *serverConn, err error, msg string) error {
	// This function is only called in doRequest, no response is sent to client.
	// So if visiting blocked site, can always retry request.
	if sv.maybeFake() || isErrConnReset(err) {
		errMsg := genErrMsg(r, msg)
		if isErrConnReset(err) {
			return c.handleBlockedRequest(r, err, errMsg)
		}
		// TODO What about broken PIPE?
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

func (c *clientConn) readResponse(sv *serverConn, r *Request) (err error) {
	var rp *Response

	if sv.state == svConnected && sv.maybeFake() {
		setConnReadTimeout(sv, readTimeout, "BEFORE receiving the first response")
	}
	if rp, err = parseResponse(sv.bufRd, r); err != nil {
		return c.handleServerReadError(r, sv, err, "Parse response from server.")
	}
	// After have received the first reponses from the server, we consider
	// ther server as real instead of fake one caused by wrong DNS reply. So
	// don't time out later.
	if sv.state == svConnected && sv.maybeFake() {
		unsetConnReadTimeout(sv, "AFTER receiving the first response")
	}
	if sv.state == svConnected {
		if sv.connType == ctDirectConn {
			domainSet.addDirectHost(sv.host)
		}
		sv.state = svSendRecvResponse
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
		if err = sendBody(c, sv, nil, rp); err != nil {
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
				err = c.handleServerReadError(r, sv, err, "Read response body from server.")
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
		c.removeServerConn(sv)
	} else {
		sv.lastUse = time.Now()
	}
	return
}

func (c *clientConn) getServerConn(r *Request) (sv *serverConn, err error) {
	sv, ok := c.serverConn[r.URL.Host]
	if ok && sv.mayBeClosed() {
		// debug.Printf("Connection to %s maybe closed\n", sv.host)
		c.removeServerConn(sv)
		ok = false
	}
	if !ok {
		sv, err = c.createServerConn(r)
	}
	return
}

func (c *clientConn) removeServerConn(sv *serverConn) {
	sv.Close()
	delete(c.serverConn, sv.host)
}

func createctDirectConnection(host string) (conn, error) {
	c, err := net.DialTimeout("tcp", host, dialTimeout)
	if err != nil {
		// Time out is very likely to be caused by GFW
		debug.Printf("Connecting to: %s %v\n", host, err)
		return conn{nil, ctNilConn}, err
	}
	// debug.Println("Connected to", host)
	return conn{c, ctDirectConn}, nil
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
		if srvconn, err = createctSocksConnection(host); err == nil {
			return
		}
	}
	if hasParentProxy {
		return zeroConn, errFailedParentProxy
	}
	return zeroConn, errNoParentProxy
}

func (c *clientConn) createConnection(host string, r *Request) (srvconn conn, err error) {
	if config.AlwaysProxy {
		if srvconn, err = createParentProxyConnection(host); err == nil {
			return
		}
		goto fail
	}
	if domainSet.isHostBlocked(host) && hasParentProxy {
		// In case of connection error to socks server, fallback to direct connection
		if srvconn, err = createParentProxyConnection(host); err == nil {
			return
		}
		if domainSet.isHostAlwaysBlocked(host) {
			goto fail
		}
		if srvconn, err = createctDirectConnection(host); err == nil {
			return
		}
	} else {
		// In case of error on direction connection, try socks server
		if srvconn, err = createctDirectConnection(host); err == nil {
			return
		}
		if domainSet.isHostAlwaysDirect(host) || hostIsIP(host) || !hasParentProxy {
			goto fail
		}
		// debug.Printf("type of err %v\n", reflect.TypeOf(err))
		// GFW may cause dns lookup fail (net.DNSError),
		// may also cause connection time out or reset (net.OpError)
		if _, ok := err.(*net.DNSError); ok || maybeBlocked(err) {
			// Try to create socks connection
			var socksErr error
			if srvconn, socksErr = createParentProxyConnection(host); socksErr == nil {
				handRes := c.handleBlockedRequest(r, err,
					genErrMsg(r, "create direct connection")+genBlockedSiteMsg(r))
				if handRes == errRetry {
					debug.Println("direct connection failed, use socks connection for ", r)
					return srvconn, nil
				}
				srvconn.Close()
				return zeroConn, handRes
			}
		}
	}

fail:
	debug.Printf("Failed connect to %s %s", host, r)
	if r.Method == "CONNECT" {
		return zeroConn, errShouldClose
	}
	sendErrorPage(c, connFailedErrCode, err.Error(),
		genErrMsg(r, "Connection failed."))
	return zeroConn, errPageSent
}

func (c *clientConn) createServerConn(r *Request) (*serverConn, error) {
	srvconn, err := c.createConnection(r.URL.Host, r)
	if err != nil {
		return nil, err
	}
	sv := newServerConn(srvconn, r.URL.Host)
	if r.isConnect {
		// Don't put connection for CONNECT method for reuse
		return sv, nil
	}
	c.serverConn[sv.host] = sv
	// client will connect to differnet servers in a single proxy connection
	// debug.Printf("serverConn to for client %v %v\n", c.RemoteAddr(), c.serverConn)
	return sv, nil
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

func copyServer2Client(sv *serverConn, c *clientConn, cliStopped notification, r *Request) (err error) {
	buf := make([]byte, bufSize)
	sv.bufRd = nil
	var n int

	for {
		if cliStopped.hasNotified() {
			debug.Println("copyServer2Client client has stopped")
			return
		}
		setConnReadTimeout(sv, readTimeout, "copyServer2Client")
		if n, err = sv.Read(buf); err != nil {
			if maybeBlocked(err) && sv.maybeFake() && domainSet.addBlockedHost(r.URL.Host) {
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
		if sv.state == svConnected {
			if sv.connType == ctDirectConn {
				domainSet.addDirectHost(sv.host)
			}
			sv.state = svSendRecvResponse
		}
	}
	return
}

func copyClient2Server(c *clientConn, sv *serverConn, srvStopped notification, r *Request) (err error) {
	// TODO this is complicated, simplify it.
	buf := make([]byte, bufSize)
	var buffered []byte
	var n int

	if c.bufRd != nil {
		n = c.bufRd.Buffered()
		if n > bufSize {
			buf = make([]byte, n, n)
		}
		if n > 0 {
			buffered, _ = c.bufRd.Peek(n) // should not return error
		}
		c.bufRd = nil
	}

	var timeout time.Duration
	if r.contBuf != nil {
		debug.Println("copyClient2Server retry request:", r)
		if _, err = sv.Write(r.contBuf.Bytes()); err != nil {
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
		if buffered != nil {
			goto writeBuf
		}
		if sv.maybeFake() && sv.state == svConnected {
			timeout = 2 * time.Second
		} else {
			timeout = readTimeout
		}
		setConnReadTimeout(c, timeout, "copyClient2Server")
		if n, err = c.Read(buf); err != nil {
			if isErrTimeout(err) {
				// Applications like Twitter for Mac needs long connection for
				// live stream. So should not close connection here. But this
				// will have the risk that socks server will report too many
				// open connections.
				continue
			}
			if config.DetectSSLErr && (isErrConnReset(err) || err == io.EOF) && sv.maybeSSLErr(start) {
				debug.Println("client connection closed very soon, taken as SSL error:", r)
				domainSet.addBlockedHost(r.URL.Host)
			}
			debug.Printf("copyClient2Server read data: %v\n", err)
			return
		}
		buffered = buf[:n]

	writeBuf:
		// When retrying request, should use socks server. So maybeFake will return false.
		if sv.state == svConnected && sv.maybeFake() {
			if r.contBuf == nil {
				r.contBuf = new(bytes.Buffer)
			}
			// store client data in case needing retry
			r.contBuf.Write(buffered)
		} else {
			// if connection has sent some data back and forth, it's not
			// likely blocked, so no need to retry. Set to nil to release memory.
			r.contBuf = nil
		}
		// Read is using buffer, so write what have been read directly
		if _, err = sv.Write(buffered); err != nil {
			if sv.maybeFake() && isErrConnReset(err) && domainSet.addBlockedHost(r.URL.Host) {
				debug.Printf("copyClient2Server blocked site %d detected, retry\n", r.URL.Host)
				return errRetry
			}
			debug.Printf("copyClient2Server write data: %v\n", err)
			return
		}
		buffered = nil
	}
	return
}

func (sv *serverConn) maybeFake() bool {
	// GFW may return wrong DNS record, which we can connect to but block
	// forever on read. (e.g. twitter.com)
	return sv.connType == ctDirectConn && !domainSet.isHostAlwaysDirect(sv.host)
}

func (sv *serverConn) maybeSSLErr(cliStart time.Time) bool {
	// If client closes connection very soon, maybe there's SSL error, maybe
	// not (e.g. user stopped request).
	// COW can't tell which is the case, so this detection is not reliable.
	return sv.state > svConnected && time.Now().Sub(cliStart) < sslLeastDuration
}

// Apache 2.2 keep-alive timeout defaults to 5 seconds.
const serverConnTimeout = 5 * time.Second

func (sv *serverConn) mayBeClosed() bool {
	if sv.connType == ctSocksConn {
		return false
	}
	return time.Now().Sub(sv.lastUse) > serverConnTimeout
}

var connEstablished = []byte("HTTP/1.0 200 Connection established\r\nProxy-agent: cow-proxy\r\n\r\n")

// Do HTTP CONNECT
func (sv *serverConn) doConnect(r *Request, c *clientConn) (err error) {
	r.state = rsCreated
	defer sv.Close()
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
		srv2CliErr = copyServer2Client(sv, c, cliStopped, r)
		srvStopped.notify()
		doneChan <- 1
	}()

	err = copyClient2Server(c, sv, srvStopped, r)
	cliStopped.notify()

	<-doneChan
	// maybeFake is needed here. If server has sent response to client, should not retry.
	if r.contBuf != nil && (err == errRetry || srv2CliErr == errRetry) && sv.maybeFake() {
		return errRetry
	}
	// Because this is in doConnect, there's no way reporting error to the client.
	// Just return and close connectino.
	return
}

// Do HTTP request other that CONNECT
func (sv *serverConn) doRequest(r *Request, c *clientConn) (err error) {
	if c.buf == nil {
		c.buf = make([]byte, bufSize*2, bufSize*2)
	}
	r.state = rsCreated
	// Send request to the server
	if _, err = sv.Write(r.raw.Bytes()); err != nil {
		// The srv connection maybe already closed.
		// Need to delete the connection and reconnect in that case.
		return c.handleServerWriteError(r, sv, err, "Sending request header")
	}

	// Send request body
	if r.contBuf != nil {
		debug.Println("Retry request send buffered body:", r)
		if _, err = sv.Write(r.contBuf.Bytes()); err != nil {
			sendErrorPage(sv, "502 send request error", err.Error(),
				"Send retry request body")
			r.contBuf = nil // let gc recycle memory earlier
			return errPageSent
		}
	} else if r.Chunking || r.ContLen > 0 {
		// Message body in request is signaled by the inclusion of a Content-
		// Length or Transfer-Encoding header. Refer to http://stackoverflow.com/a/299696/306935
		// The server connection may have been closed, need to retry request in that case.
		r.contBuf = new(bytes.Buffer)
		if err = sendBody(c, sv, r, nil); err != nil {
			if err == io.EOF {
				if r.KeepAlive {
					errl.Println("Unexpected EOF reading request body from client", r)
				} else {
					err = nil
				}
			} else if isErrOpWrite(err) {
				err = c.handleServerWriteError(r, sv, err, "Sending request body")
			} else {
				errl.Println("Reading request body:", err)
			}
			return
		}
	}
	r.state = rsSent
	return c.readResponse(sv, r)
}

// Send response body if header specifies content length
func sendBodyWithContLen(c *clientConn, r io.Reader, w, contBuf io.Writer, contLen int) (err error) {
	// debug.Println("Sending body with content length", contLen)
	if err = copyN(r, w, contBuf, contLen, c.buf, nil, nil); err != nil {
		debug.Println("sendBodyWithContLen error:", err)
	}
	return
}

// Send response body if header specifies chunked encoding
func sendBodyChunked(c *clientConn, r *bufio.Reader, w, contBuf io.Writer) (err error) {
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
		if size == 0 { // end of chunked data, TODO should ignore trailers
			if err = readCheckCRLF(r); err != nil {
				errl.Println("Check CRLF after chunked size 0:", err)
			}
			if contBuf != nil {
				contBuf.Write(chunkEndbytes)
			}
			if _, err = w.Write(chunkEndbytes); err != nil {
				debug.Println("Sending chunk ending:", err)
			}
			return
		}
		if err = copyN(r, w, contBuf, int(size), c.buf, []byte(s+"\r\n"), CRLFbytes); err != nil {
			debug.Println("Copying chunked data:", err)
			return
		}
		if err = readCheckCRLF(r); err != nil {
			// If we see this, maybe the server is sending trailers
			errl.Println("Chunked data not end with CRLF, try continue")
		}
	}
	return
}

var CRLFbytes = []byte("\r\n")
var chunkEndbytes = []byte("0\r\n\r\n")

// Client can't use closed connection to indicate end of request, so must
// content length or use chunked encoding. Thus contBuf is not necessary for
// sendBodySplitIntoChunk.
func sendBodySplitIntoChunk(c *clientConn, r io.Reader, w io.Writer) (err error) {
	var n int
	bufLen := len(c.buf)
	for {
		n, err = r.Read(c.buf[:bufLen-2]) // make sure we have space to add CRLF
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

		chunkSize := []byte(fmt.Sprintf("%x\r\n", n))
		if _, err = w.Write(chunkSize); err != nil {
			debug.Printf("Writing chunk size %v\n", err)
			return
		}
		copy(c.buf[n:], CRLFbytes)
		if _, err = w.Write(c.buf[:n+len(CRLFbytes)]); err != nil {
			debug.Println("Writing chunk data:", err)
			return
		}
	}
	return
}

// Send message body. If req is not nil, read from client, send to server. If
// rp is not nil, the direction is the oppisite.
func sendBody(c *clientConn, sv *serverConn, req *Request, rp *Response) (err error) {
	var contLen int
	var chunk bool
	var bufRd *bufio.Reader
	var w, contBuf io.Writer

	if req == nil { // read from server, write to client
		w = c
		bufRd = sv.bufRd
		contLen = int(rp.ContLen)
		chunk = rp.Chunking
	} else {
		w = sv
		bufRd = c.bufRd
		contLen = int(req.ContLen)
		chunk = req.Chunking
		contBuf = req.contBuf
	}

	if chunk {
		err = sendBodyChunked(c, bufRd, w, contBuf)
	} else if contLen > 0 {
		err = sendBodyWithContLen(c, bufRd, w, contBuf, contLen)
	} else {
		if req != nil {
			errl.Println("Should not happen! Client request with body but no length specified.")
		}
		err = sendBodySplitIntoChunk(c, bufRd, w)
	}
	return
}
