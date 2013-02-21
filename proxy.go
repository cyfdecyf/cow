package main

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/cyfdecyf/bufio"
	"io"
	"net"
	// "reflect"
	"strings"
	"time"
)

// var _ = reflect.TypeOf

// Explicitly specify buffer size to avoid unnecessary copy using
// bufio.Reader's Read. As I'm using ReadSlice to read line, it's possible to
// get bufio.ErrBufferFull while reading headers, so set it to a large default
// value to prevent such problems.
const bufSize = 8192

var freeList = make(chan []byte, 80)

func getBuf() (b []byte) {
	select {
	case b = <-freeList:
	default:
		b = make([]byte, bufSize)
	}
	return
}

func freeBuf(b []byte) {
	select {
	case freeList <- b:
	default:
	}
	return
}

// Close client connection it no new request received in 1 minute.
const clientConnTimeout = 60 * time.Second

// If client closed connection for HTTP CONNECT method in less then 1 second,
// consider it as an ssl error. This is only effective for Chrome which will
// drop connection immediately upon SSL error.
const sslLeastDuration = time.Second

// Some code are learnt from the http package

type Proxy struct {
	addr      string // listen address, contains port
	port      string
	addrInPAC string // proxy server address to use in PAC
}

type connType byte

const (
	ctNilConn connType = iota
	ctDirectConn
	ctSocksConn
	ctShadowctSocksConn
	ctHttpProxyConn
)

var ctName = [...]string{
	ctNilConn:           "nil",
	ctDirectConn:        "direct",
	ctSocksConn:         "socks5",
	ctShadowctSocksConn: "shadowsocks",
	ctHttpProxyConn:     "http parent",
}

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

var zeroConn conn
var zeroTime time.Time

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
	bufRd       *bufio.Reader
	url         *URL
	state       serverConnState
	willCloseOn time.Time
	siteInfo    *VisitCnt
	visited     bool
	timeoutSet  bool
}

func newServerConn(c conn, url *URL, siteInfo *VisitCnt) *serverConn {
	return &serverConn{
		conn:     c,
		url:      url,
		bufRd:    bufio.NewReaderSize(c, bufSize),
		siteInfo: siteInfo,
	}
}

type clientConn struct {
	net.Conn   // connection to the proxy client
	bufRd      *bufio.Reader
	serverConn map[string]*serverConn // request serverConn, host:port as key
	buf        []byte                 // buffer for reading request, avoids repeatedly allocating buffer
	proxy      *Proxy
}

var (
	errRetry             = errors.New("Retry")
	errPageSent          = errors.New("Error page has sent")
	errShouldClose       = errors.New("Error can only be handled by close connection")
	errInternal          = errors.New("Internal error")
	errNoParentProxy     = errors.New("No parent proxy")
	errFailedParentProxy = errors.New("Failed connecting to parent proxy")

	errChunkedEncode   = errors.New("Invalid chunked encoding")
	errMalformHeader   = errors.New("Malformed HTTP header")
	errMalformResponse = errors.New("Malformed HTTP response")
	errNotSupported    = errors.New("Not supported")
	errBadRequest      = errors.New("Bad request")
	errAuthRequired    = errors.New("Authentication requried")
)

func NewProxy(addr, addrInPAC string) *Proxy {
	_, port := splitHostPort(addr)
	return &Proxy{addr: addr, port: port, addrInPAC: addrInPAC}
}

func (py *Proxy) Serve(done chan byte) {
	defer func() {
		done <- 1
	}()
	ln, err := net.Listen("tcp", py.addr)
	if err != nil {
		fmt.Println("Server creation failed:", err)
		return
	}
	host, port := splitHostPort(py.addr)
	if host == "" || host == "0.0.0.0" {
		info.Printf("COW proxy address %s, PAC url http://<hostip>:%s/pac\n", py.addr, port)
	} else if py.addrInPAC == "" {
		info.Printf("COW proxy address %s, PAC url http://%s/pac\n", py.addr, py.addr)
	} else {
		info.Printf("COW proxy address %s, PAC url http://%s/pac\n", py.addr, py.addrInPAC)
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			debug.Println("Client connection:", err)
			continue
		}
		if debug {
			debug.Println("New Client:", conn.RemoteAddr())
		}
		c := newClientConn(conn, py)
		go c.serve()
	}
}

func newClientConn(rwc net.Conn, proxy *Proxy) *clientConn {
	c := &clientConn{
		Conn:       rwc,
		serverConn: map[string]*serverConn{},
		bufRd:      bufio.NewReaderSize(rwc, bufSize),
		proxy:      proxy,
	}
	return c
}

func (c *clientConn) close() {
	if c.buf != nil {
		freeBuf(c.buf)
	}
	for _, sv := range c.serverConn {
		debug.Println("Client close: closing server conn:", sv.url.HostPort)
		sv.Close()
	}
	c.Conn.Close()
	if debug {
		debug.Printf("Client %v connection closed\n", c.RemoteAddr())
	}
}

func isSelfURL(url string) bool {
	return url == ""
}

func (c *clientConn) getRequest() (r *Request, err error) {
	if r, err = parseRequest(c); err != nil {
		c.handleClientReadError(r, err, "parse client request")
		return nil, err
	}
	return r, nil
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
end:
	sendErrorPage(c, "404 not found", "Page not found", "Handling request to proxy itself.")
	return errPageSent
}

func (c *clientConn) handleRetry(r *Request, sv *serverConn) (err error) {
	if !r.responseNotSent() {
		debug.Printf("%v has sent response, can't retry\n", r)
		return errShouldClose
	}
	if !r.tooMuchRetry() {
		return errRetry
	}
	if sv.maybeFake() {
		// Sometimes GFW reset will got EOF error leading to retry too many times
		// In that case, try parent proxy.
		siteStat.TempBlocked(r.URL)
		r.tryCnt = 0
		return errRetry
	}
	debug.Printf("Can't retry %v tryCnt=%d\n", r, r.tryCnt)
	if !r.isConnect {
		sendErrorPage(c, "502 retry failed", "Can't finish HTTP request",
			genErrMsg(r, sv, "Has tried several times."))
		return errPageSent
	}
	return errShouldClose
}

func (c *clientConn) serve() {
	defer c.close()

	var r *Request
	var err error
	var sv *serverConn

	var authed bool
	var authCnt int

	// Refer to implementation.md for the design choices on parsing the request
	// and response.
	for {
		if r, err = c.getRequest(); err != nil {
			sendErrorPage(c, "404 Bad request", "Bad request", err.Error())
			return
		}
		if dbgRq {
			dbgRq.Printf("%v %v\n", c.RemoteAddr(), r)
		}

		if isSelfURL(r.URL.HostPort) {
			if err = c.serveSelfURL(r); err != nil {
				return
			}
			continue
		}

		if auth.required && !authed {
			if authCnt > 5 {
				return
			}
			if err = Authenticate(c, r); err != nil {
				if err == errAuthRequired {
					authCnt++
					continue
				} else {
					return
				}
			}
			authed = true
		}

	retry:
		r.tryOnce()
		if bool(debug) && r.isRetry() {
			errl.Printf("%s retry request tryCnt=%d %v\n", c.RemoteAddr(), r.tryCnt, r)
		}
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
				// connection for CONNECT is not reused, no need to remove
				if err = c.handleRetry(r, sv); err == errRetry {
					goto retry
				}
			}
			// Why return after doConnect:
			// 1. proxy can only know whether the request is finished when either
			// the server or the client close connection
			// 2. if the web server closes connection, the only way to
			// tell the client this is to close client connection (proxy
			// don't know the protocol between the client and server)

			// debug.Printf("doConnect for %s to %s done\n", c.RemoteAddr(), r.URL.HostPort)
			return
		}

		if err = sv.doRequest(r, c); err != nil {
			c.removeServerConn(sv)
			if err == errRetry {
				err = c.handleRetry(r, sv)
				if err == errRetry {
					goto retry
				}
			}
			if err == errPageSent {
				continue
			}
			return
		}

		if !r.ConnectionKeepAlive {
			// debug.Println("close client connection because request has no keep-alive")
			return
		}
	}
}

func genErrMsg(r *Request, sv *serverConn, what string) string {
	if sv == nil {
		return fmt.Sprintf("<p>HTTP Request <strong>%v</strong></p> <p>%s</p>", r, what)
	}
	return fmt.Sprintf("<p>HTTP Request <strong>%v</strong></p> <p>%s</p> <p>Using %s connection.</p>",
		r, what, ctName[sv.connType])
}

const (
	errCodeReset   = "502 connection reset"
	errCodeTimeout = "504 time out reading response"
	errCodeBadReq  = "400 bad request"
)

func (c *clientConn) handleBlockedRequest(r *Request) error {
	siteStat.TempBlocked(r.URL)
	return errRetry
}

func (c *clientConn) handleServerReadError(r *Request, sv *serverConn, err error, msg string) error {
	var errMsg string
	if err == io.EOF {
		if debug {
			debug.Printf("client %s; %s read from server EOF\n", c.RemoteAddr(), msg)
		}
		return errRetry
	}
	if sv.maybeFake() && maybeBlocked(err) {
		return c.handleBlockedRequest(r)
	}
	if r.responseNotSent() {
		errMsg = genErrMsg(r, sv, msg)
		sendErrorPage(c, "502 read error", err.Error(), errMsg)
		return errPageSent
	}
	errl.Println("Unhandled server read error:", err, r)
	return errShouldClose
}

func (c *clientConn) handleServerWriteError(r *Request, sv *serverConn, err error, msg string) error {
	// This function is only called in doRequest, no response is sent to client.
	// So if visiting blocked site, can always retry request.
	if sv.maybeFake() && isErrConnReset(err) {
		siteStat.TempBlocked(r.URL)
	}
	return errRetry
}

func (c *clientConn) handleClientReadError(r *Request, err error, msg string) error {
	if err == io.EOF {
		debug.Printf("%s client closed connection", msg)
	} else if debug {
		if isErrConnReset(err) {
			debug.Printf("%s connection reset", msg)
		} else if isErrTimeout(err) {
			debug.Printf("%s client read timeout, maybe has closed\n", msg)
		} else {
			// may reach here when header is larger than bufSize
			debug.Printf("handleClientReadError: %s %v %v\n", msg, err, r)
		}
	}
	return err
}

func (c *clientConn) handleClientWriteError(r *Request, err error, msg string) error {
	// debug.Printf("handleClientWriteError: %s %v %v\n", msg, err, r)
	return err
}

func isErrOpWrite(err error) bool {
	if ne, ok := err.(*net.OpError); ok && ne.Op == "write" {
		return true
	}
	return false
}

func isErrOpRead(err error) bool {
	if ne, ok := err.(*net.OpError); ok && ne.Op == "read" {
		return true
	}
	return false
}

func (c *clientConn) readResponse(sv *serverConn, r *Request) (err error) {
	var rp *Response

	if rp, err = parseResponse(sv, r); err != nil {
		return c.handleServerReadError(r, sv, err, "Parse response from server.")
	}
	// After have received the first reponses from the server, we consider
	// ther server as real instead of fake one caused by wrong DNS reply. So
	// don't time out later.
	sv.state = svSendRecvResponse

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
	if rp.ConnectionKeepAlive {
		if rp.KeepAlive == time.Duration(0) {
			// Apache 2.2 timeout defaults to 5 seconds.
			const serverConnTimeout = 5 * time.Second
			sv.willCloseOn = time.Now().Add(serverConnTimeout)
		} else {
			sv.willCloseOn = time.Now().Add(rp.KeepAlive - time.Second)
		}
	} else {
		c.removeServerConn(sv)
	}
	return
}

func (c *clientConn) getServerConn(r *Request) (sv *serverConn, err error) {
	sv, ok := c.serverConn[r.URL.HostPort]
	if ok && sv.mayBeClosed() {
		// debug.Printf("Connection to %s maybe closed\n", sv.url.HostPort)
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
	delete(c.serverConn, sv.url.HostPort)
}

func createctDirectConnection(url *URL, siteInfo *VisitCnt) (conn, error) {
	to := dialTimeout
	if siteInfo.OnceBlocked() && to >= defaultDialTimeout {
		to /= 2
	}
	c, err := net.DialTimeout("tcp", url.HostPort, to)
	if err != nil {
		// Time out is very likely to be caused by GFW
		debug.Printf("Error connecting to: %s %v\n", url.HostPort, err)
		return zeroConn, err
	}
	debug.Println("Connected to", url.HostPort)
	return conn{c, ctDirectConn}, nil
}

func maybeBlocked(err error) bool {
	return isErrTimeout(err) || isErrConnReset(err)
}

type parentProxyConnectionFunc func(*URL) (conn, error)

var parentProxyCreator = []parentProxyConnectionFunc{}

func createHttpProxyConnection(url *URL) (cn conn, err error) {
	c, err := net.Dial("tcp", config.HttpParent)
	if err != nil {
		debug.Printf("Error connecting to: %s %v\n", url.HostPort, err)
		return zeroConn, err
	}
	debug.Println("Connected to http parent proxy")
	return conn{c, ctHttpProxyConn}, nil
}

func createParentProxyConnection(url *URL) (srvconn conn, err error) {
	// Try parent proxy in the order specified in the config
	for _, f := range parentProxyCreator {
		if srvconn, err = f(url); err == nil {
			return
		}
	}
	if len(parentProxyCreator) != 0 {
		return zeroConn, errFailedParentProxy
	}
	return zeroConn, errNoParentProxy
}

func (c *clientConn) createConnection(r *Request, siteInfo *VisitCnt) (srvconn conn, err error) {
	if config.AlwaysProxy {
		if srvconn, err = createParentProxyConnection(r.URL); err == nil {
			return
		}
		goto fail
	}
	if siteInfo.AsBlocked() && hasParentProxy {
		// In case of connection error to socks server, fallback to direct connection
		if srvconn, err = createParentProxyConnection(r.URL); err == nil {
			return
		}
		if siteInfo.AlwaysBlocked() || siteInfo.AsTempBlocked() {
			goto fail
		}
		if srvconn, err = createctDirectConnection(r.URL, siteInfo); err == nil {
			return
		}
	} else {
		// In case of error on direction connection, try parent server
		if srvconn, err = createctDirectConnection(r.URL, siteInfo); err == nil {
			return
		}
		if !hasParentProxy || siteInfo.AlwaysDirect() {
			goto fail
		}
		// debug.Printf("type of err %v\n", reflect.TypeOf(err))
		// GFW may cause dns lookup fail (net.DNSError),
		// may also cause connection time out or reset (net.OpError)
		if isDNSError(err) || maybeBlocked(err) {
			// Try to create socks connection
			var socksErr error
			if srvconn, socksErr = createParentProxyConnection(r.URL); socksErr == nil {
				c.handleBlockedRequest(r)
				debug.Println("direct connection failed, use socks connection for", r)
				return srvconn, nil
			}
		}
	}

fail:
	debug.Printf("Failed connect to %s %s", r.URL.HostPort, r)
	if r.Method == "CONNECT" {
		return zeroConn, errShouldClose
	}
	sendErrorPage(c, "504 Connection failed", err.Error(),
		genErrMsg(r, nil, "Connection failed."))
	return zeroConn, errPageSent
}

func (c *clientConn) createServerConn(r *Request) (*serverConn, error) {
	siteInfo := siteStat.GetVisitCnt(r.URL)
	srvconn, err := c.createConnection(r, siteInfo)
	if err != nil {
		return nil, err
	}
	sv := newServerConn(srvconn, r.URL, siteInfo)
	if r.isConnect {
		// Don't put connection for CONNECT method for reuse
		return sv, nil
	}
	c.serverConn[sv.url.HostPort] = sv
	// client will connect to differnet servers in a single proxy connection
	// debug.Printf("serverConn to for client %v %v\n", c.RemoteAddr(), c.serverConn)
	return sv, nil
}

func (sv *serverConn) directConnection() bool {
	return sv.connType == ctDirectConn
}

func (sv *serverConn) updateVisit() {
	if sv.visited {
		return
	}
	sv.visited = true
	if sv.directConnection() {
		sv.siteInfo.DirectVisit()
	} else {
		sv.siteInfo.BlockedVisit()
	}
}

func (sv *serverConn) maybeFake() bool {
	return sv.state == svConnected && sv.directConnection() && !sv.siteInfo.AlwaysDirect()
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

// setReadTimeout will only set timeout if the server connection maybe fake.
// In case it's not fake, this will unset timeout.
func (sv *serverConn) setReadTimeout(msg string) {
	if sv.maybeFake() {
		to := readTimeout
		if sv.siteInfo.OnceBlocked() && to >= defaultReadTimeout {
			// use shorter readTimeout for potential blocked sites
			to /= 2
		}
		setConnReadTimeout(sv, to, msg)
		sv.timeoutSet = true
	} else {
		sv.unsetReadTimeout(msg)
	}
}

func (sv *serverConn) unsetReadTimeout(msg string) {
	if sv.timeoutSet {
		sv.timeoutSet = false
		unsetConnReadTimeout(sv, msg)
	}
}

func (sv *serverConn) maybeSSLErr(cliStart time.Time) bool {
	// If client closes connection very soon, maybe there's SSL error, maybe
	// not (e.g. user stopped request).
	// COW can't tell which is the case, so this detection is not reliable.
	return sv.state > svConnected && time.Now().Sub(cliStart) < sslLeastDuration
}

func (sv *serverConn) mayBeClosed() bool {
	return time.Now().After(sv.willCloseOn)
}

func copyServer2Client(sv *serverConn, c *clientConn, r *Request) (err error) {
	// For connect, the data read each time is usually less than 4096. Little
	// benefit to increase this to 8192.
	buf := getBuf()
	defer func() {
		freeBuf(buf)
	}()
	sv.bufRd = nil
	var n int

	/*
		// XXX This is to debug CONNECT retry
		if r.tryCnt == 1 {
			time.Sleep(1)
			return errRetry
		}
	*/

	total := 0
	const directThreshold = 4096
	for {
		// debug.Println("srv->cli")
		sv.setReadTimeout("srv->cli")
		if n, err = sv.Read(buf); err != nil {
			if err == io.EOF {
				return errRetry
			}
			if sv.maybeFake() && maybeBlocked(err) {
				siteStat.TempBlocked(r.URL)
				debug.Printf("srv->cli blocked site %s detected, err: %v retry\n", r.URL.HostPort, err)
				return errRetry
			}
			// Expected error: "use of closed network connection",
			// this is to make blocking read return.
			// debug.Printf("copyServer2Client read data: %v\n", err)
			return
		}
		total += n
		if _, err = c.Write(buf[0:n]); err != nil {
			// debug.Printf("copyServer2Client write data: %v\n", err)
			return
		}
		// debug.Printf("srv(%s)->cli(%s) sent %d bytes data\n", r.URL.HostPort, c.RemoteAddr(), total)
		// set state to rsRecvBody to indicate the request has partial response sent to client
		r.state = rsRecvBody
		sv.state = svSendRecvResponse
		sv.unsetReadTimeout("srv->cli")
		if total > directThreshold {
			sv.updateVisit()
		}
	}
	return
}

func copy2Server(sv *serverConn, r *Request, p []byte) (err error) {
	if r.responseNotSent() {
		if r.contBuf == nil {
			r.contBuf = new(bytes.Buffer)
		}
		r.contBuf.Write(p)
	} else if r.contBuf != nil {
		r.contBuf = nil
	}
	_, err = sv.Write(p)
	return
}

func copyClient2Server(c *clientConn, sv *serverConn, r *Request, srvStopped notification, done chan byte) (err error) {
	// sv.maybeFake may change during execution in this function.
	// So need a variable to record the whether timeout is set.
	deadlineIsSet := false
	buf := getBuf()
	defer func() {
		freeBuf(buf)
		if deadlineIsSet {
			// maybe need to retry, should unset timeout here because
			unsetConnReadTimeout(c, "cli->srv after err")
		}
		done <- 1
	}()

	var n int

	if r.contBuf != nil {
		debug.Printf("cli(%s)->srv(%s) retry request send %d bytes of stored data\n",
			c.RemoteAddr(), r.URL.HostPort, r.contBuf.Len())
		if _, err = sv.Write(r.contBuf.Bytes()); err != nil {
			debug.Println("cli->srv send to server error")
			return
		}
	}

	if c.bufRd != nil {
		n = c.bufRd.Buffered()
		if n > 0 {
			buffered, _ := c.bufRd.Peek(n) // should not return error
			if err = copy2Server(sv, r, buffered); err != nil {
				// debug.Printf("cli->srv write buffered err: %v\n", err)
				return
			}
		}
		c.bufRd = nil
	}

	var start time.Time
	if config.DetectSSLErr {
		start = time.Now()
	}
	for {
		// debug.Println("cli->srv")
		if sv.maybeFake() {
			setConnReadTimeout(c, time.Second, "cli->srv")
			deadlineIsSet = true
		} else if deadlineIsSet {
			// maybeFake may trun to false after timeout, but timeout should be unset
			unsetConnReadTimeout(c, "cli->srv before read")
			deadlineIsSet = false
		}
		if n, err = c.Read(buf); err != nil {
			if config.DetectSSLErr && (isErrConnReset(err) || err == io.EOF) && sv.maybeSSLErr(start) {
				debug.Println("client connection closed very soon, taken as SSL error:", r)
				siteStat.TempBlocked(r.URL)
			} else if isErrTimeout(err) && !srvStopped.hasNotified() {
				// debug.Printf("cli(%s)->srv(%s) timeout\n", c.RemoteAddr(), r.URL.HostPort)
				continue
			}
			// debug.Printf("cli->srv read err: %v\n", err)
			return
		}

		// copyServer2Client will detect write to closed server. Just store client content for retry.
		if err = copy2Server(sv, r, buf[:n]); err != nil {
			// XXX is it enough to only do block detection in copyServer2Client?
			/*
				if sv.maybeFake() && isErrConnReset(err) {
					siteStat.TempBlocked(r.URL)
					errl.Printf("copyClient2Server blocked site %d detected, retry\n", r.URL.HostPort)
					return errRetry
				}
			*/
			// debug.Printf("cli->srv write err: %v\n", err)
			return
		}
		// debug.Printf("cli(%s)->srv(%s) sent %d bytes data\n", c.RemoteAddr(), r.URL.HostPort, n)
	}
	return
}

var connEstablished = []byte("HTTP/1.0 200 Connection established\r\nProxy-agent: cow-proxy\r\n\r\n")

// Do HTTP CONNECT
func (sv *serverConn) doConnect(r *Request, c *clientConn) (err error) {
	r.state = rsCreated

	if sv.connType == ctHttpProxyConn {
		// debug.Printf("%s Sending CONNECT request to http proxy server\n", c.RemoteAddr())
		if err = sv.sendHTTPProxyRequest(r, c); err != nil {
			if debug {
				debug.Printf("%s Error sending CONNECT request to http proxy server: %v\n",
					c.RemoteAddr(), err)
			}
			sv.Close()
			return err
		}
	} else if !r.isRetry() {
		// debug.Printf("send connection confirmation to %s->%s\n", c.RemoteAddr(), r.URL.HostPort)
		if _, err = c.Write(connEstablished); err != nil {
			if debug {
				debug.Printf("%v Error sending 200 Connecion established: %v\n", c.RemoteAddr(), err)
			}
			sv.Close()
			return err
		}
	}

	var cli2srvErr error
	done := make(chan byte, 1)
	srvStopped := newNotification()
	go func() {
		// debug.Printf("doConnect: cli(%s)->srv(%s)\n", c.RemoteAddr(), r.URL.HostPort)
		cli2srvErr = copyClient2Server(c, sv, r, srvStopped, done)
		sv.Close() // close sv to force read from server in copyServer2Client return
	}()

	// debug.Printf("doConnect: srv(%s)->cli(%s)\n", r.URL.HostPort, c.RemoteAddr())
	err = copyServer2Client(sv, c, r)
	if err == errRetry {
		srvStopped.notify()
		<-done
		// debug.Printf("doConnect: cli(%s)->srv(%s) stopped\n", c.RemoteAddr(), r.URL.HostPort)
	} else {
		// close client connection to force read from client in copyClient2Server return
		c.Conn.Close()
	}
	if cli2srvErr == errRetry || err == errRetry {
		return errRetry
	}
	return
}

func (sv *serverConn) sendHTTPProxyRequest(r *Request, c *clientConn) (err error) {
	if _, err = sv.Write(r.origReqLine); err != nil {
		return c.handleServerWriteError(r, sv, err, "sending proxy request line to server")
	}
	if _, err = sv.Write(r.raw.Bytes()[r.headerStart:]); err != nil {
		return c.handleServerWriteError(r, sv, err, "sending proxy request header to server")
	}
	return
}

// Do HTTP request other that CONNECT
func (sv *serverConn) doRequest(r *Request, c *clientConn) (err error) {
	if c.buf == nil {
		// It's common for response to have content-length larger than 4096
		c.buf = getBuf()
	}
	r.state = rsCreated
	// Send request to the server
	if sv.connType != ctHttpProxyConn {
		if _, err = sv.Write(r.raw.Bytes()); err != nil {
			return c.handleServerWriteError(r, sv, err, "sending request to server")
		}
	} else {
		if err = sv.sendHTTPProxyRequest(r, c); err != nil {
			return
		}
	}

	// Send request body
	if r.contBuf != nil {
		debug.Println("Retry request send buffered body:", r)
		if _, err = sv.Write(r.contBuf.Bytes()); err != nil {
			return c.handleServerWriteError(r, sv, err, "Sending retry request body")
		}
	} else if r.Chunking || r.ContLen > 0 {
		// Message body in request is signaled by the inclusion of a Content-
		// Length or Transfer-Encoding header. Refer to http://stackoverflow.com/a/299696/306935
		// The server connection may have been closed, need to retry request in that case.
		r.contBuf = new(bytes.Buffer)
		if err = sendBody(c, sv, r, nil); err != nil {
			if err == io.EOF && isErrOpRead(err) {
				info.Println("EOF reading request body from client", r)
			} else if isErrOpWrite(err) {
				err = c.handleServerWriteError(r, sv, err, "Sending request body")
			} else {
				errl.Println("Reading request body:", err)
			}
			return
		}
	}
	r.state = rsSent
	err = c.readResponse(sv, r)
	if err == nil {
		sv.updateVisit()
	}
	return
}

// Send response body if header specifies content length
func sendBodyWithContLen(buf []byte, r io.Reader, w io.Writer, contLen int) (err error) {
	// debug.Println("Sending body with content length", contLen)
	if contLen == 0 {
		return
	}
	if err = copyN(r, w, contLen, buf, nil, nil); err != nil {
		debug.Println("sendBodyWithContLen error:", err)
	}
	return
}

// Send response body if header specifies chunked encoding
func sendBodyChunked(buf []byte, r *bufio.Reader, w io.Writer) (err error) {
	// debug.Println("Sending chunked body")
	for {
		var s []byte
		// Read chunk size line, ignore chunk extension if any.
		if s, err = r.PeekSlice('\n'); err != nil {
			errl.Println("Peeking chunk size:", err)
			return
		}
		// debug.Printf("Chunk size line %s\n", s)
		smid := bytes.IndexByte(s, ';')
		if smid == -1 {
			smid = len(s)
		}
		var size int64
		if size, err = ParseIntFromBytes(TrimSpace(s[:smid]), 16); err != nil {
			errl.Println("Chunk size invalid:", err)
			return
		}
		// end of chunked data. As we remove trailer header in request sending
		// to server, there should be no trailer in response.
		// TODO: Is it possible for client request body to have trailers in it?
		if size == 0 {
			r.Skip(len(s))
			skipCRLF(r)
			if _, err = w.Write([]byte(chunkEnd)); err != nil {
				debug.Println("Sending chunk ending:", err)
			}
			return
		}
		// The spec section 19.3 only suggest toleranting single LF for
		// headers, so assume the server will send CRLF. If not, the following
		// parse int may find errors.
		total := len(s) + int(size) + 2 // total data size for this chunk, including ending CRLF
		bn := r.Buffered()
		left := total - bn
		if left < 0 { // buffered content has more data than the current chunk
			debug.Println("buffered content has more than current chunk")
			left = 0
			bn = total
		}
		// First write out buffered content, so we can avoid unnecessary copy
		b, _ := r.ReadN(bn) // should not return error
		if _, err = w.Write(b); err != nil {
			debug.Println("Writing chunked data:", err)
			return
		}
		if left > 0 {
			if err = copyN(r, w, left, buf, nil, nil); err != nil {
				debug.Println("Copying chunked data:", err)
				return
			}
		}
	}
	return
}

const CRLF = "\r\n"
const chunkEnd = "0\r\n\r\n"

// Client can't use closed connection to indicate end of request, so must
// content length or use chunked encoding. Thus contBuf is not necessary for
// sendBodySplitIntoChunk.
func sendBodySplitIntoChunk(buf []byte, r io.Reader, w io.Writer) (err error) {
	// debug.Printf("sendBodySplitIntoChunk called\n")
	var n int
	bufLen := len(buf)
	for {
		n, err = r.Read(buf[:bufLen-2]) // make sure we have space to add CRLF
		// debug.Println("split into chunk n =", n, "err =", err)
		if err != nil {
			if err == io.EOF {
				// EOF is expected here as the server is closing connection.
				// debug.Println("end chunked encoding")
				if _, err = w.Write([]byte(chunkEnd)); err != nil {
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
		copy(buf[n:], CRLF)
		if _, err = w.Write(buf[:n+len(CRLF)]); err != nil {
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
	var w io.Writer

	if req == nil { // read from server, write to client
		w = c
		bufRd = sv.bufRd
		contLen = int(rp.ContLen)
		chunk = rp.Chunking
	} else {
		w = io.MultiWriter(sv, req.contBuf)
		bufRd = c.bufRd
		contLen = int(req.ContLen)
		chunk = req.Chunking
	}

	// chunked encoding has precedence over content length
	// COW does not sanitize response header, but should correctly handle it
	if chunk {
		err = sendBodyChunked(c.buf, bufRd, w)
	} else if contLen >= 0 {
		err = sendBodyWithContLen(c.buf, bufRd, w, contLen)
	} else {
		if req != nil {
			errl.Println("Should not happen! Client request with body but no length specified.")
		}
		err = sendBodySplitIntoChunk(c.buf, bufRd, w)
	}
	return
}
