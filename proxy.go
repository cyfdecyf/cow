package main

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/cyfdecyf/bufio"
	"github.com/cyfdecyf/leakybuf"
	"io"
	"math/rand"
	"net"
	"reflect"
	"strings"
	"time"
)

// As I'm using ReadSlice to read line, it's possible to get
// bufio.ErrBufferFull while reading line, so set it to a large value to
// prevent such problems.
//
// For limits about URL and HTTP header size, refer to:
// http://stackoverflow.com/questions/417142/what-is-the-maximum-length-of-a-url
// (URL usually are less than 2100 bytes.)
// http://www.mnot.net/blog/2011/07/11/what_proxies_must_do
// (This says "URIs should be allowed at least 8000 octets, and HTTP headers
// (should have 4000 as an absolute minimum".)
const httpBufSize = 8192

// Hold at most 4MB memory as buffer for parsing http request/response and
// holding post data.
var httpBuf = leakybuf.NewLeakyBuf(512, httpBufSize)

// Close client connection if no new request received in some time. Keep it
// small to avoid keeping too many client connections (which associates with
// server connections) and causing too much open file error. (On OS X, the
// default soft limit of open file descriptor is 256, which is really
// conservative.) On the other hand, making this too small may cause reading
// normal client request timeout.
const clientConnTimeout = 10 * time.Second
const keepAliveHeader = "Keep-Alive: timeout=10\r\n"

// If client closed connection for HTTP CONNECT method in less then 1 second,
// consider it as an ssl error. This is only effective for Chrome which will
// drop connection immediately upon SSL error.
const sslLeastDuration = time.Second

// Some code are learnt from the http package

// encapulate actual error for an retry error
type RetryError struct {
	error
}

func isErrRetry(err error) bool {
	if err == nil {
		return false
	}
	_, ok := err.(RetryError)
	return ok
}

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

type serverConn struct {
	conn
	bufRd       *bufio.Reader
	buf         []byte // buffer for the buffered reader
	url         *URL
	state       serverConnState
	willCloseOn time.Time
	siteInfo    *VisitCnt
	visited     bool
}

type clientConn struct {
	net.Conn   // connection to the proxy client
	bufRd      *bufio.Reader
	buf        []byte                 // buffer for the buffered reader
	serverConn map[string]*serverConn // request serverConn, host:port as key
	proxy      *Proxy
}

var (
	errTooManyRetry  = errors.New("Too many retry")
	errPageSent      = errors.New("Error page has sent")
	errShouldClose   = errors.New("Error can only be handled by close connection")
	errInternal      = errors.New("Internal error")
	errNoParentProxy = errors.New("No parent proxy")

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
			debug.Println("client connection:", err)
			continue
		}
		if debug {
			debug.Println("new client:", conn.RemoteAddr())
		}
		c := newClientConn(conn, py)
		go c.serve()
	}
}

func newClientConn(rwc net.Conn, proxy *Proxy) *clientConn {
	buf := httpBuf.Get()
	c := &clientConn{
		Conn:       rwc,
		serverConn: map[string]*serverConn{},
		buf:        buf,
		bufRd:      bufio.NewReaderFromBuf(rwc, buf),
		proxy:      proxy,
	}
	return c
}

func (c *clientConn) releaseBuf() {
	c.bufRd = nil
	if c.buf != nil {
		// debug.Println("release client buffer")
		httpBuf.Put(c.buf)
		c.buf = nil
	}
}

func (c *clientConn) Close() error {
	c.releaseBuf()
	for _, sv := range c.serverConn {
		sv.Close()
	}
	if debug {
		debug.Printf("Client %v connection closed\n", c.RemoteAddr())
	}
	return c.Conn.Close()
}

func isSelfURL(url string) bool {
	return url == ""
}

func (c *clientConn) getRequest(r *Request) (err error) {
	if err = parseRequest(c, r); err != nil {
		c.handleClientReadError(r, err, "parse client request")
		return err
	}
	return nil
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

func (c *clientConn) shouldHandleRetry(r *Request, sv *serverConn, re error) bool {
	if !isErrRetry(re) {
		return false
	}
	err, _ := re.(RetryError)
	if !r.responseNotSent() {
		debug.Printf("%v has sent some response, can't retry\n", r)
		return false
	}
	if r.partial {
		debug.Printf("%v partial request, can't retry\n", r)
		sendErrorPage(c, "502 partial request", err.Error(),
			genErrMsg(r, sv, "Request is too large to hold in buffer, can't retry. "+
				"Refresh to retry may work."))
		return false
	} else if r.raw == nil {
		errl.Println("Please report an issue: Non partial request with buffer released:", r)
		panic("Please report an issue: Non partial request with buffer released")
	}
	if r.tooManyRetry() {
		if sv.maybeFake() {
			// Sometimes GFW reset will got EOF error leading to retry too many times.
			// In that case, consider the url as temp blocked and try parent proxy.
			siteStat.TempBlocked(r.URL)
			r.tryCnt = 0
			return true
		}
		debug.Printf("Can't retry %v tryCnt=%d\n", r, r.tryCnt)
		sendErrorPage(c, "502 retry failed", "Can't finish HTTP request",
			genErrMsg(r, sv, "Has tried several times."))
		return false
	}
	return true
}

func (c *clientConn) serve() {
	var r Request
	var rp Response
	var sv *serverConn
	var err error

	var authed bool
	var authCnt int

	defer func() {
		r.releaseBuf()
		c.Close()
	}()

	// Refer to implementation.md for the design choices on parsing the request
	// and response.
	cnt := 0
	for {
		if c.bufRd == nil || c.buf == nil {
			errl.Printf("%s client read buffer nil, served %d requests",
				c.RemoteAddr(), cnt)
			if r.URL != nil {
				errl.Println("previous request:", &r)
			}
			panic("client read buffer nil")
		}
		cnt++
		if err = c.getRequest(&r); err != nil {
			sendErrorPage(c, "404 Bad request", "Bad request", err.Error())
			return
		}
		if dbgRq {
			if verbose {
				dbgRq.Printf("request from client %s: %s\n%s", c.RemoteAddr(), &r, r.Verbose())
			} else {
				dbgRq.Printf("request from client %s: %s\n", c.RemoteAddr(), &r)
			}
		}

		if isSelfURL(r.URL.HostPort) {
			if err = c.serveSelfURL(&r); err != nil {
				return
			}
			continue
		}

		if auth.required && !authed {
			if authCnt > 5 {
				return
			}
			if err = Authenticate(c, &r); err != nil {
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
			errl.Printf("%s retry request tryCnt=%d %v\n", c.RemoteAddr(), r.tryCnt, &r)
		}
		if sv, err = c.getServerConn(&r); err != nil {
			// debug.Printf("Failed to get serverConn for %s %v\n", c.RemoteAddr(), r)
			// Failed connection will send error page back to the client.
			// For CONNECT, the client read buffer is released in copyClient2Server,
			// so can't go back to getRequest.
			if err == errPageSent && !r.isConnect {
				continue
			}
			return
		}

		if r.isConnect {
			err = sv.doConnect(&r, c)
			sv.Close()
			if c.shouldHandleRetry(&r, sv, err) {
				// connection for CONNECT is not reused, no need to remove
				goto retry
			}
			// debug.Printf("doConnect %s to %s done\n", c.RemoteAddr(), r.URL.HostPort)
			return
		}

		if err = sv.doRequest(c, &r, &rp); err != nil {
			c.removeServerConn(sv)
			if c.shouldHandleRetry(&r, sv, err) {
				goto retry
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

func (c *clientConn) handleBlockedRequest(r *Request, err error) error {
	siteStat.TempBlocked(r.URL)
	return RetryError{err}
}

func (c *clientConn) handleServerReadError(r *Request, sv *serverConn, err error, msg string) error {
	var errMsg string
	if err == io.EOF {
		if debug {
			debug.Printf("client %s; %s read from server EOF\n", c.RemoteAddr(), msg)
		}
		return RetryError{err}
	}
	if sv.maybeFake() && maybeBlocked(err) {
		return c.handleBlockedRequest(r, err)
	}
	if r.responseNotSent() {
		errMsg = genErrMsg(r, sv, msg)
		sendErrorPage(c, "502 read error", err.Error(), errMsg)
		return errPageSent
	}
	errl.Println(msg+" unhandled server read error:", err, reflect.TypeOf(err), r)
	return errShouldClose
}

func (c *clientConn) handleServerWriteError(r *Request, sv *serverConn, err error, msg string) error {
	// This function is only called in doRequest, no response is sent to client.
	// So if visiting blocked site, can always retry request.
	if sv.maybeFake() && isErrConnReset(err) {
		siteStat.TempBlocked(r.URL)
	}
	return RetryError{err}
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
			// may reach here when header is larger than buffer size
			debug.Printf("handleClientReadError: %s %v %v\n", msg, err, r)
		}
	}
	return err
}

func (c *clientConn) handleClientWriteError(r *Request, err error, msg string) error {
	// debug.Printf("handleClientWriteError: %s %v %v\n", msg, err, r)
	return err
}

func (c *clientConn) readResponse(sv *serverConn, r *Request, rp *Response) (err error) {
	sv.initBuf()
	defer func() {
		if rp != nil {
			rp.releaseBuf()
		}
	}()

	/*
		if r.partial {
			return RetryError{errors.New("debug retry for partial request")}
		}
	*/

	/*
		// force retry for debugging
		if r.tryCnt == 1 {
			return RetryError{errors.New("debug retry in readResponse")}
		}
	*/

	if err = parseResponse(sv, r, rp); err != nil {
		return c.handleServerReadError(r, sv, err, "Parse response from server.")
	}
	// After have received the first reponses from the server, we consider
	// ther server as real instead of fake one caused by wrong DNS reply. So
	// don't time out later.
	sv.state = svSendRecvResponse
	r.state = rsRecvBody
	r.releaseBuf()

	if _, err = c.Write(rp.rawResponse()); err != nil {
		return c.handleClientWriteError(r, err, "Write response header back to client")
	}
	if dbgRep {
		if verbose {
			// extra space after resposne to align with request debug message
			dbgRep.Printf("response  to client %v: %s %s\n%s", c.RemoteAddr(), r, rp, rp.Verbose())
		} else {
			dbgRep.Printf("response  to client %v: %s %s\n", c.RemoteAddr(), r, rp)
		}
	}
	rp.releaseBuf()

	if rp.hasBody(r.Method) {
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
				errl.Println("unexpected EOF reading body from server", r)
			} else if isErrOpRead(err) {
				err = c.handleServerReadError(r, sv, err, "Read response body from server.")
			} else if isErrOpWrite(err) {
				err = c.handleClientWriteError(r, err, "Write response body to client.")
			} else {
				errl.Println("sendBody unknown network op error", reflect.TypeOf(err), r)
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
		to = minDialTimeout
	}
	c, err := net.DialTimeout("tcp", url.HostPort, to)
	if err != nil {
		// Time out is very likely to be caused by GFW
		debug.Printf("error direct connect to: %s %v\n", url.HostPort, err)
		return zeroConn, err
	}
	debug.Println("connected to", url.HostPort)
	return conn{c, ctDirectConn}, nil
}

func isErrTimeout(err error) bool {
	if ne, ok := err.(net.Error); ok {
		return ne.Timeout()
	}
	return false
}

func maybeBlocked(err error) bool {
	return isErrTimeout(err) || isErrConnReset(err)
}

func createHttpProxyConnection(url *URL) (cn conn, err error) {
	c, err := net.Dial("tcp", config.HttpParent)
	if err != nil {
		errl.Printf("can't connect to http parent proxy for %s: %v\n", url.HostPort, err)
		return zeroConn, err
	}
	debug.Println("connected to:", url.HostPort, "via http parent proxy")
	return conn{c, ctHttpProxyConn}, nil
}

type parentProxyConnectionFunc func(*URL) (conn, error)

var parentProxyCreator []parentProxyConnectionFunc
var parentProxyFailCnt []int // initialized in checkConfig

func callParentProxyCreateFunc(i int, url *URL) (srvconn conn, err error) {
	const maxFailCnt = 30
	srvconn, err = parentProxyCreator[i](url)
	if err != nil {
		if parentProxyFailCnt[i] < maxFailCnt && !networkBad() {
			parentProxyFailCnt[i]++
		}
		return
	}
	parentProxyFailCnt[i] = 0
	return
}

func createParentProxyConnection(url *URL) (srvconn conn, err error) {
	const baseFailCnt = 9
	var skipped []int
	nproxy := len(parentProxyCreator)

	proxyId := 0
	if config.LoadBalance == loadBalanceHash {
		proxyId = int(stringHash(url.Host) % uint64(nproxy))
	}

	for i := 0; i < nproxy; i++ {
		proxyId = (proxyId + i) % nproxy
		// skip failed server, but try it with some probability
		failcnt := parentProxyFailCnt[proxyId]
		if failcnt > 0 && rand.Intn(failcnt+baseFailCnt) != 0 {
			skipped = append(skipped, proxyId)
			continue
		}
		if srvconn, err = callParentProxyCreateFunc(proxyId, url); err == nil {
			return
		}
	}
	// last resort, try skipped one, not likely to succeed
	for _, skippedId := range skipped {
		if srvconn, err = callParentProxyCreateFunc(skippedId, url); err == nil {
			return
		}
	}
	if len(parentProxyCreator) != 0 {
		return
	}
	return zeroConn, errNoParentProxy
}

func (c *clientConn) createConnection(r *Request, siteInfo *VisitCnt) (srvconn conn, err error) {
	var errMsg string
	if config.AlwaysProxy {
		if srvconn, err = createParentProxyConnection(r.URL); err == nil {
			return
		}
		errMsg = genErrMsg(r, nil, "Parent proxy connection failed, always using parent proxy.")
		goto fail
	}
	if siteInfo.AsBlocked() && hasParentProxy {
		// In case of connection error to socks server, fallback to direct connection
		if srvconn, err = createParentProxyConnection(r.URL); err == nil {
			return
		}
		if siteInfo.AlwaysBlocked() {
			errMsg = genErrMsg(r, nil, "Parent proxy connection failed, always blocked site.")
			goto fail
		}
		if siteInfo.AsTempBlocked() {
			errMsg = genErrMsg(r, nil, "Parent proxy connection failed, temporarily blocked site.")
			goto fail
		}
		if srvconn, err = createctDirectConnection(r.URL, siteInfo); err == nil {
			return
		}
		errMsg = genErrMsg(r, nil, "Parent proxy and direct connection failed, maybe blocked site.")
	} else {
		// In case of error on direction connection, try parent server
		if srvconn, err = createctDirectConnection(r.URL, siteInfo); err == nil {
			return
		}
		if !hasParentProxy {
			errMsg = genErrMsg(r, nil, "Direct connection failed, no parent proxy.")
			goto fail
		}
		if siteInfo.AlwaysDirect() {
			errMsg = genErrMsg(r, nil, "Direct connection failed, always direct site.")
			goto fail
		}
		// debug.Printf("type of err %v\n", reflect.TypeOf(err))
		// GFW may cause dns lookup fail (net.DNSError),
		// may also cause connection time out or reset (net.OpError)
		if isDNSError(err) || maybeBlocked(err) {
			// Try to create connection by parent proxy
			var socksErr error
			if srvconn, socksErr = createParentProxyConnection(r.URL); socksErr == nil {
				c.handleBlockedRequest(r, err)
				debug.Println("direct connection failed, use parent proxy for", r)
				return srvconn, nil
			}
			errMsg = genErrMsg(r, nil, "Direct and parent proxy connection failed, maybe blocked site.")
		} else {
			errl.Printf("direct connection for %s failed, unhandled error: %v\n", r, err)
			errMsg = genErrMsg(r, nil, "Direct connection failed, unhandled error.")
		}
	}

fail:
	sendErrorPage(c, "504 Connection failed", err.Error(), errMsg)
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

// Should call initBuf before reading http response from server. This allows
// us not init buf for connect method which does not need to parse http
// respnose.
func newServerConn(c conn, url *URL, siteInfo *VisitCnt) *serverConn {
	sv := &serverConn{
		conn:     c,
		url:      url,
		siteInfo: siteInfo,
	}
	return sv
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

func (sv *serverConn) initBuf() {
	if sv.bufRd == nil {
		sv.buf = httpBuf.Get()
		sv.bufRd = bufio.NewReaderFromBuf(sv, sv.buf)
	}
}

func (sv *serverConn) Close() error {
	debug.Println("Closing server conn:", sv.url.HostPort)
	sv.bufRd = nil
	if sv.buf != nil {
		// debug.Println("release server buffer")
		httpBuf.Put(sv.buf)
		sv.buf = nil
	}
	return sv.Conn.Close()
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
	to := readTimeout
	if sv.siteInfo.OnceBlocked() && to > defaultReadTimeout {
		to = minReadTimeout
	}
	setConnReadTimeout(sv, to, msg)
}

func (sv *serverConn) unsetReadTimeout(msg string) {
	unsetConnReadTimeout(sv, msg)
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

// Use smaller buffer for connection method as the buffer will be hold for a
// very long time.
const connectBufSize = 4096

// Hold at most 2M memory for connection buffer. This can support 256
// concurrent connect method.
var connectBuf = leakybuf.NewLeakyBuf(512, connectBufSize)

func copyServer2Client(sv *serverConn, c *clientConn, r *Request) (err error) {
	buf := connectBuf.Get()
	defer func() {
		connectBuf.Put(buf)
	}()

	/*
		// force retry for debugging
		if r.tryCnt == 1 && sv.maybeFake() {
			time.Sleep(1)
			return RetryError{errors.New("debug retry in copyServer2Client")}
		}
	*/

	total := 0
	const directThreshold = 4096
	readTimeoutSet := false
	for {
		// debug.Println("srv->cli")
		if sv.maybeFake() {
			sv.setReadTimeout("srv->cli")
			readTimeoutSet = true
		} else if readTimeoutSet {
			sv.unsetReadTimeout("srv->cli")
			readTimeoutSet = false
		}
		var n int
		if n, err = sv.Read(buf); err != nil {
			if sv.maybeFake() && maybeBlocked(err) {
				siteStat.TempBlocked(r.URL)
				debug.Printf("srv->cli blocked site %s detected, err: %v retry\n", r.URL.HostPort, err)
				return RetryError{err}
			}
			// Expected error besides EOF: "use of closed network connection",
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
		if total > directThreshold {
			sv.updateVisit()
		}
	}
	return
}

type serverWriter struct {
	rq *Request
	sv *serverConn
}

func newServerWriter(r *Request, sv *serverConn) *serverWriter {
	return &serverWriter{r, sv}
}

// Write to server, store written data in request buffer if necessary.
// FIXME: too tighly coupled with Request.
func (sw *serverWriter) Write(p []byte) (int, error) {
	if sw.rq.raw == nil {
		// buffer released
	} else if sw.rq.raw.Len() >= 2*httpBufSize {
		// Avoid using too much memory to hold request body. If a request is
		// not buffered completely, COW can't retry and can release memory
		// immediately.
		debug.Println("request body too large, not buffering any more")
		sw.rq.releaseBuf()
		sw.rq.partial = true
	} else if sw.rq.responseNotSent() {
		sw.rq.raw.Write(p)
	} else { // has sent response
		sw.rq.releaseBuf()
	}
	return sw.sv.Write(p)
}

func copyClient2Server(c *clientConn, sv *serverConn, r *Request, srvStopped notification, done chan byte) (err error) {
	// sv.maybeFake may change during execution in this function.
	// So need a variable to record the whether timeout is set.
	deadlineIsSet := false
	defer func() {
		if deadlineIsSet {
			// maybe need to retry, should unset timeout here because
			unsetConnReadTimeout(c, "cli->srv after err")
		}
		done <- 1
	}()

	var n int

	if r.isRetry() {
		if debug {
			debug.Printf("cli(%s)->srv(%s) retry request %d bytes of buffered body\n",
				c.RemoteAddr(), r.URL.HostPort, len(r.rawBody()))
		}
		if _, err = sv.Write(r.rawBody()); err != nil {
			debug.Println("cli->srv send to server error")
			return
		}
	}

	w := newServerWriter(r, sv)
	if c.bufRd != nil {
		n = c.bufRd.Buffered()
		if n > 0 {
			buffered, _ := c.bufRd.Peek(n) // should not return error
			if _, err = w.Write(buffered); err != nil {
				// debug.Printf("cli->srv write buffered err: %v\n", err)
				return
			}
		}
		if debug {
			debug.Printf("cli->srv client %s released read buffer\n", c.RemoteAddr())
		}
		c.releaseBuf()
	}

	var start time.Time
	if config.DetectSSLErr {
		start = time.Now()
	}
	buf := connectBuf.Get()
	defer func() {
		connectBuf.Put(buf)
	}()
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
		if _, err = w.Write(buf[:n]); err != nil {
			// XXX is it enough to only do block detection in copyServer2Client?
			/*
				if sv.maybeFake() && isErrConnReset(err) {
					siteStat.TempBlocked(r.URL)
					errl.Printf("copyClient2Server blocked site %d detected, retry\n", r.URL.HostPort)
					return RetryError{err}
				}
			*/
			// debug.Printf("cli->srv write err: %v\n", err)
			return
		}
		// debug.Printf("cli(%s)->srv(%s) sent %d bytes data\n", c.RemoteAddr(), r.URL.HostPort, n)
	}
	return
}

var connEstablished = []byte("HTTP/1.1 200 Tunnel established\r\n\r\n")

// Do HTTP CONNECT
func (sv *serverConn) doConnect(r *Request, c *clientConn) (err error) {
	r.state = rsCreated

	if sv.connType == ctHttpProxyConn {
		// debug.Printf("%s Sending CONNECT request to http proxy server\n", c.RemoteAddr())
		if err = sv.sendHTTPProxyRequest(r, c); err != nil {
			if debug {
				debug.Printf("%s error sending CONNECT request to http proxy server: %v\n",
					c.RemoteAddr(), err)
			}
			return err
		}
	} else if !r.isRetry() {
		// debug.Printf("send connection confirmation to %s->%s\n", c.RemoteAddr(), r.URL.HostPort)
		if _, err = c.Write(connEstablished); err != nil {
			if debug {
				debug.Printf("%v Error sending 200 Connecion established: %v\n", c.RemoteAddr(), err)
			}
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
	if isErrRetry(err) {
		srvStopped.notify()
		<-done
		// debug.Printf("doConnect: cli(%s)->srv(%s) stopped\n", c.RemoteAddr(), r.URL.HostPort)
	} else {
		// close client connection to force read from client in copyClient2Server return
		c.Conn.Close()
	}
	if isErrRetry(cli2srvErr) {
		return cli2srvErr
	}
	return
}

func (sv *serverConn) sendHTTPProxyRequest(r *Request, c *clientConn) (err error) {
	if _, err = sv.Write(r.proxyRequestLine()); err != nil {
		return c.handleServerWriteError(r, sv, err,
			"sending proxy request line to http parent")
	}
	// Add authorization header for parent http proxy
	if config.httpAuthHeader != nil {
		if _, err = sv.Write(config.httpAuthHeader); err != nil {
			return c.handleServerWriteError(r, sv, err,
				"sending proxy authorization header to http parent")
		}
	}
	if _, err = sv.Write(r.rawHeaderBody()); err != nil {
		return c.handleServerWriteError(r, sv, err,
			"sending proxy request header to http parent")
	}
	/*
		if bool(dbgRq) && verbose {
			debug.Printf("request to http proxy:\n%s%s", r.proxyRequestLine(), r.rawHeaderBody())
		}
	*/
	return
}

func (sv *serverConn) sendRequest(r *Request, c *clientConn) (err error) {
	// Send request to the server
	if sv.connType == ctHttpProxyConn {
		return sv.sendHTTPProxyRequest(r, c)
	}
	/*
		if bool(debug) && verbose {
			debug.Printf("request to server\n%s", r.rawRequest())
		}
	*/
	if _, err = sv.Write(r.rawRequest()); err != nil {
		err = c.handleServerWriteError(r, sv, err, "sending request to server")
	}
	return
}

// Do HTTP request other that CONNECT
func (sv *serverConn) doRequest(c *clientConn, r *Request, rp *Response) (err error) {
	r.state = rsCreated
	if err = sv.sendRequest(r, c); err != nil {
		return
	}

	// Send request body. If this is retry, r.raw contains request body and is
	// sent while sending request.
	if !r.isRetry() && (r.Chunking || r.ContLen > 0) {
		// Message body in request is signaled by the inclusion of a Content-
		// Length or Transfer-Encoding header. Refer to http://stackoverflow.com/a/299696/306935
		if err = sendBody(c, sv, r, nil); err != nil {
			if err == io.EOF && isErrOpRead(err) {
				errl.Println("EOF reading request body from client", r)
			} else if isErrOpWrite(err) {
				err = c.handleServerWriteError(r, sv, err, "Sending request body")
			} else {
				errl.Println("reading request body:", err)
			}
			return
		}
		if debug {
			debug.Printf("%s %s body sent\n", c.RemoteAddr(), r)
		}
	}
	r.state = rsSent
	err = c.readResponse(sv, r, rp)
	if err == nil {
		sv.updateVisit()
	}
	return
}

// Send response body if header specifies content length
func sendBodyWithContLen(r *bufio.Reader, w io.Writer, contLen int) (err error) {
	// debug.Println("Sending body with content length", contLen)
	if contLen == 0 {
		return
	}
	if err = copyN(w, r, contLen, httpBufSize); err != nil {
		debug.Println("sendBodyWithContLen error:", err)
	}
	return
}

// Send response body if header specifies chunked encoding. rdSize specifies
// the size of each read on Reader, it should be set to be the buffer size of
// the Reader, this parameter is added for testing.
func sendBodyChunked(r *bufio.Reader, w io.Writer, rdSize int) (err error) {
	// debug.Println("Sending chunked body")
	for {
		var s []byte
		// Read chunk size line, ignore chunk extension if any.
		if s, err = r.PeekSlice('\n'); err != nil {
			errl.Println("peeking chunk size:", err)
			return
		}
		// debug.Printf("Chunk size line %s\n", s)
		smid := bytes.IndexByte(s, ';')
		if smid == -1 {
			smid = len(s)
		}
		var size int64
		if size, err = ParseIntFromBytes(TrimSpace(s[:smid]), 16); err != nil {
			errl.Println("chunk size invalid:", err)
			return
		}
		// end of chunked data. As we remove trailer header in request sending
		// to server, there should be no trailer in response.
		// TODO: Is it possible for client request body to have trailers in it?
		if size == 0 {
			r.Skip(len(s))
			skipCRLF(r)
			if _, err = w.Write([]byte(chunkEnd)); err != nil {
				debug.Println("sending chunk ending:", err)
			}
			return
		}
		// The spec section 19.3 only suggest toleranting single LF for
		// headers, not for chunked encoding. So assume the server will send
		// CRLF. If not, the following parse int may find errors.
		total := len(s) + int(size) + 2 // total data size for this chunk, including ending CRLF
		// PeekSlice will not advance reader, so we can just copy total sized data.
		if err = copyN(w, r, total, rdSize); err != nil {
			debug.Println("copying chunked data:", err)
			return
		}
	}
	return
}

const CRLF = "\r\n"
const chunkEnd = "0\r\n\r\n"

func sendBodySplitIntoChunk(r *bufio.Reader, w io.Writer) (err error) {
	// debug.Printf("sendBodySplitIntoChunk called\n")
	var b []byte
	for {
		b, err = r.ReadNext()
		// debug.Println("split into chunk n =", n, "err =", err)
		if err != nil {
			if err == io.EOF {
				// EOF is expected here as the server is closing connection.
				// debug.Println("end chunked encoding")
				_, err = w.Write([]byte(chunkEnd))
				if err != nil {
					debug.Println("Write chunk end 0")
				}
				return
			}
			debug.Println("read error in sendBodySplitIntoChunk", err)
			return
		}

		chunkSize := []byte(fmt.Sprintf("%x\r\n", len(b)))
		if _, err = w.Write(chunkSize); err != nil {
			debug.Printf("writing chunk size %v\n", err)
			return
		}
		if _, err = w.Write(b); err != nil {
			debug.Println("writing chunk data:", err)
			return
		}
		if _, err = w.Write([]byte(CRLF)); err != nil {
			debug.Println("writing chunk ending CRLF:", err)
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

	if rp != nil { // read responses from server, write to client
		w = c
		bufRd = sv.bufRd
		contLen = int(rp.ContLen)
		chunk = rp.Chunking
	} else if req != nil { // read request body from client, send to server
		// The server connection may have been closed, need to retry request in that case.
		// So always need to save request body.
		w = newServerWriter(req, sv)
		bufRd = c.bufRd
		contLen = int(req.ContLen)
		chunk = req.Chunking
	} else {
		panic("sendBody must have either request or response not nil")
	}

	// chunked encoding has precedence over content length
	// COW does not sanitize response header, but should correctly handle it
	if chunk {
		err = sendBodyChunked(bufRd, w, httpBufSize)
	} else if contLen >= 0 {
		err = sendBodyWithContLen(bufRd, w, contLen)
	} else {
		if req != nil {
			errl.Println("client request with body but no length or chunked encoding specified.")
			return errBadRequest
		}
		err = sendBodySplitIntoChunk(bufRd, w)
	}
	return
}
