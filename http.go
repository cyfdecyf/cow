package main

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/cyfdecyf/bufio"
	"net"
	"strconv"
	"strings"
	"time"
)

const CRLF = "\r\n"

const (
	statusCodeContinue = 100
)

const (
	statusBadReq         = "400 Bad Request"
	statusForbidden      = "403 Forbidden"
	statusExpectFailed   = "417 Expectation Failed"
	statusRequestTimeout = "408 Request Timeout"
)

var CustomHttpErr = errors.New("CustomHttpErr")

type Header struct {
	ContLen             int64
	KeepAlive           time.Duration
	ProxyAuthorization  string
	Chunking            bool
	Trailer             bool
	ConnectionKeepAlive bool
	ExpectContinue      bool
	Host                string
}

type rqState byte

const (
	rsCreated  rqState = iota
	rsSent             // request has been sent to server
	rsRecvBody         // response header received, receiving response body
	rsDone
)

type Request struct {
	Method  string
	URL     *URL
	raw     *bytes.Buffer // stores the raw content of request header
	rawByte []byte        // underlying buffer for raw

	// request line from client starts at 0, cow generates request line that
	// can be sent directly to web server
	reqLnStart int // start of generated request line in raw
	headStart  int // start of header in raw
	bodyStart  int // start of body in raw

	Header
	isConnect bool
	partial   bool // whether contains only partial request data
	state     rqState
	tryCnt    byte
}

// Assume keep-alive request by default.
var zeroRequest = Request{Header: Header{ConnectionKeepAlive: true}}

func (r *Request) reset() {
	b := r.rawByte
	raw := r.raw
	*r = zeroRequest // reset to zero value

	if raw != nil {
		raw.Reset()
		r.rawByte = b
		r.raw = raw
	} else {
		r.rawByte = httpBuf.Get()
		r.raw = bytes.NewBuffer(r.rawByte[:0]) // must use 0 length slice
	}
}

func (r *Request) String() (s string) {
	return fmt.Sprintf("%s %s%s", r.Method, r.URL.HostPort, r.URL.Path)
}

func (r *Request) Verbose() []byte {
	var rqbyte []byte
	if r.isConnect {
		rqbyte = r.rawBeforeBody()
	} else {
		// This includes client request line if has http parent proxy
		rqbyte = r.raw.Bytes()
	}
	return rqbyte
}

// Message body in request is signaled by the inclusion of a Content-Length
// or Transfer-Encoding header.
// Refer to http://stackoverflow.com/a/299696/306935
func (r *Request) hasBody() bool {
	return r.Chunking || r.ContLen > 0
}

func (r *Request) isRetry() bool {
	return r.tryCnt > 1
}

func (r *Request) tryOnce() {
	r.tryCnt++
}

func (r *Request) tooManyRetry() bool {
	return r.tryCnt > 3
}

func (r *Request) responseNotSent() bool {
	return r.state <= rsSent
}

func (r *Request) hasSent() bool {
	return r.state >= rsSent
}

func (r *Request) releaseBuf() {
	if r.raw != nil {
		httpBuf.Put(r.rawByte)
		r.rawByte = nil
		r.raw = nil
	}
}

// rawRequest returns the raw request that can be sent directly to HTTP/1.1 server.
func (r *Request) rawRequest() []byte {
	return r.raw.Bytes()[r.reqLnStart:]
}

func (r *Request) rawBeforeBody() []byte {
	return r.raw.Bytes()[:r.bodyStart]
}

func (r *Request) rawHeaderBody() []byte {
	return r.raw.Bytes()[r.headStart:]
}

func (r *Request) rawBody() []byte {
	return r.raw.Bytes()[r.bodyStart:]
}

func (r *Request) proxyRequestLine() []byte {
	return r.raw.Bytes()[0:r.reqLnStart]
}

func (r *Request) genRequestLine() {
	// Generate normal HTTP request line
	r.raw.WriteString(r.Method + " ")
	if len(r.URL.Path) == 0 {
		r.raw.WriteString("/")
	} else {
		r.raw.WriteString(r.URL.Path)
	}
	r.raw.WriteString(" HTTP/1.1\r\n")
}

type Response struct {
	Status int
	Reason []byte

	Header

	raw     *bytes.Buffer
	rawByte []byte
}

var zeroResponse = Response{Header: Header{ConnectionKeepAlive: true}}

func (rp *Response) reset() {
	b := rp.rawByte
	raw := rp.raw
	*rp = zeroResponse

	if raw != nil {
		raw.Reset()
		rp.rawByte = b
		rp.raw = raw
	} else {
		rp.rawByte = httpBuf.Get()
		rp.raw = bytes.NewBuffer(rp.rawByte[:0])
	}
}

func (rp *Response) releaseBuf() {
	if rp.raw != nil {
		httpBuf.Put(rp.rawByte)
		rp.rawByte = nil
		rp.raw = nil
	}
}

func (rp *Response) rawResponse() []byte {
	return rp.raw.Bytes()
}

func (rp *Response) genStatusLine() {
	rp.raw.Write([]byte("HTTP/1.1 "))
	rp.raw.WriteString(strconv.Itoa(rp.Status))
	if len(rp.Reason) != 0 {
		rp.raw.WriteByte(' ')
		rp.raw.Write(rp.Reason)
	}
	rp.raw.Write([]byte(CRLF))
	return
}

func (rp *Response) String() string {
	return fmt.Sprintf("%d %s", rp.Status, rp.Reason)
}

func (rp *Response) Verbose() []byte {
	return rp.raw.Bytes()
}

type URL struct {
	HostPort string // must contain port
	Host     string // no port
	Port     string
	Domain   string
	Path     string
}

func (url *URL) String() string {
	return url.HostPort + url.Path
}

// Set all fields according to hostPort except Path.
func (url *URL) ParseHostPort(hostPort string) {
	if hostPort == "" {
		return
	}
	host, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		// Add default 80 and split again. If there's still error this time,
		// it's not because lack of port number.
		host = hostPort
		port = "80"
		hostPort = net.JoinHostPort(hostPort, port)
	}

	url.Host = host
	url.Port = port
	url.HostPort = hostPort
	url.Domain = host2Domain(host)
}

// net.ParseRequestURI will unescape encoded path, but the proxy doesn't need
// that. Assumes the input rawurl is valid. Even if rawurl is not valid, net.Dial
// will check the correctness of the host.

func ParseRequestURI(rawurl string) (*URL, error) {
	return ParseRequestURIBytes([]byte(rawurl))
}

func ParseRequestURIBytes(rawurl []byte) (*URL, error) {
	if rawurl[0] == '/' {
		return &URL{Path: string(rawurl)}, nil
	}

	var rest, scheme []byte
	id := bytes.Index(rawurl, []byte("://"))
	if id == -1 {
		rest = rawurl
		scheme = []byte("http") // default to http
	} else {
		scheme = rawurl[:id]
		ASCIIToLowerInplace(scheme) // it's ok to lower case scheme
		if !bytes.Equal(scheme, []byte("http")) && !bytes.Equal(scheme, []byte("https")) {
			errl.Printf("%s protocol not supported\n", scheme)
			return nil, errors.New("protocol not supported")
		}
		rest = rawurl[id+3:]
	}

	var hostport, host, port, path string
	id = bytes.IndexByte(rest, '/')
	if id == -1 {
		hostport = string(rest)
	} else {
		hostport = string(rest[:id])
		path = string(rest[id:])
	}

	// Must add port in host so it can be used as key to find the correct
	// server connection.
	// e.g. google.com:80 and google.com:443 should use different connections.
	host, port, err := net.SplitHostPort(hostport)
	if err != nil { // missing port
		host = hostport
		if len(scheme) == 4 {
			hostport = net.JoinHostPort(host, "80")
			port = "80"
		} else {
			hostport = net.JoinHostPort(host, "443")
			port = "443"
		}
	}
	// Fixed wechat image url bug, url like http://[::ffff:183.192.196.102]/mmsns/lVxxxxxx
	host = strings.TrimSuffix(strings.TrimPrefix(host, "[::ffff:"), "]")
	hostport = net.JoinHostPort(host, port)
	return &URL{hostport, host, port, host2Domain(host), path}, nil
}

// headers of interest to a proxy
// Define them as constant and use editor's completion to avoid typos.
// Note RFC2616 only says about "Connection", no "Proxy-Connection", but
// Firefox and Safari send this header along with "Connection" header.
// See more at http://homepage.ntlworld.com/jonathan.deboynepollard/FGA/web-proxy-connection-header.html
const (
	headerConnection         = "connection"
	headerContentLength      = "content-length"
	headerExpect             = "expect"
	headerHost               = "host"
	headerKeepAlive          = "keep-alive"
	headerProxyAuthenticate  = "proxy-authenticate"
	headerProxyAuthorization = "proxy-authorization"
	headerProxyConnection    = "proxy-connection"
	headerReferer            = "referer"
	headerTE                 = "te"
	headerTrailer            = "trailer"
	headerTransferEncoding   = "transfer-encoding"
	headerUpgrade            = "upgrade"

	fullHeaderConnectionKeepAlive = "Connection: keep-alive\r\n"
	fullHeaderConnectionClose     = "Connection: close\r\n"
	fullHeaderTransferEncoding    = "Transfer-Encoding: chunked\r\n"
)

// Using Go's method expression
var headerParser = map[string]HeaderParserFunc{
	headerConnection:         (*Header).parseConnection,
	headerContentLength:      (*Header).parseContentLength,
	headerExpect:             (*Header).parseExpect,
	headerHost:               (*Header).parseHost,
	headerKeepAlive:          (*Header).parseKeepAlive,
	headerProxyAuthorization: (*Header).parseProxyAuthorization,
	headerProxyConnection:    (*Header).parseConnection,
	headerTransferEncoding:   (*Header).parseTransferEncoding,
	headerTrailer:            (*Header).parseTrailer,
}

var hopByHopHeader = map[string]bool{
	headerConnection:         true,
	headerKeepAlive:          true,
	headerProxyAuthorization: true,
	headerProxyConnection:    true,
	headerTE:                 true,
	headerTrailer:            true,
	headerTransferEncoding:   true,
	headerUpgrade:            true,
}

// Note: Value bytes passed to header parse function are in the buffer
// associated with bufio and will be modified. It will also be stored in the
// raw request buffer, so becareful when modifying the value bytes. (Change
// case only when the spec says it is case insensitive.)
//
// If Header needs to hold raw value, make a copy. For example,
// parseProxyAuthorization does this.

type HeaderParserFunc func(*Header, []byte) error

// Used by both "Connection" and "Proxy-Connection" header. COW always adds
// connection header at the end of a request/response (in parseRequest and
// parseResponse), no matter whether the original one has this header or not.
// This will change the order of headers, but should be OK as RFC2616 4.2 says
// header order is not significant. (Though general-header first is "good-
// practice".)
func (h *Header) parseConnection(s []byte) error {
	ASCIIToLowerInplace(s)
	h.ConnectionKeepAlive = !bytes.Contains(s, []byte("close"))
	return nil
}

func (h *Header) parseContentLength(s []byte) (err error) {
	h.ContLen, err = ParseIntFromBytes(s, 10)
	return err
}

func (h *Header) parseHost(s []byte) (err error) {
	if h.Host == "" {
		h.Host = string(s)
	}
	return
}

func (h *Header) parseKeepAlive(s []byte) (err error) {
	ASCIIToLowerInplace(s)
	id := bytes.Index(s, []byte("timeout="))
	if id != -1 {
		id += len("timeout=")
		end := id
		for ; end < len(s) && IsDigit(s[end]); end++ {
		}
		delta, err := ParseIntFromBytes(s[id:end], 10)
		if err != nil {
			return err // possible empty bytes
		}
		h.KeepAlive = time.Second * time.Duration(delta)
	}
	return nil
}

func (h *Header) parseProxyAuthorization(s []byte) error {
	h.ProxyAuthorization = string(s)
	return nil
}

func (h *Header) parseTransferEncoding(s []byte) error {
	ASCIIToLowerInplace(s)
	// For transfer-encoding: identify, it's the same as specifying neither
	// content-length nor transfer-encoding.
	h.Chunking = bytes.Contains(s, []byte("chunked"))
	if !h.Chunking && !bytes.Contains(s, []byte("identity")) {
		return fmt.Errorf("invalid transfer encoding: %s", s)
	}
	return nil
}

// RFC 2616 3.6.1 states when trailers are allowed:
//
// a) request includes TE header
// b) server is the original server
//
// Even though COW removes TE header, the original server can still respond
// with Trailer header.
// As Trailer is general header, it's possible to appear in request. But is
// there any client does this?
func (h *Header) parseTrailer(s []byte) error {
	// use errl to test if this header is common to see
	errl.Printf("got Trailer header: %s\n", s)
	if len(s) != 0 {
		h.Trailer = true
	}
	return nil
}

// For now, COW does not fully support 100-continue. It will return "417
// expectation failed" if a request contains expect header. This is one of the
// strategies supported by polipo, which is easiest to implement in cow.
// TODO If we see lots of expect 100-continue usage, provide full support.

func (h *Header) parseExpect(s []byte) error {
	ASCIIToLowerInplace(s)
	errl.Printf("Expect header: %s\n", s) // put here to see if expect header is widely used
	h.ExpectContinue = true
	/*
		if bytes.Contains(s, []byte("100-continue")) {
			h.ExpectContinue = true
		}
	*/
	return nil
}

func splitHeader(s []byte) (name, val []byte, err error) {
	i := bytes.IndexByte(s, ':')
	if i < 0 {
		return nil, nil, fmt.Errorf("malformed header: %#v", string(s))
	}
	// Do not lower case field value, as it maybe case sensitive
	return ASCIIToLower(s[:i]), TrimSpace(s[i+1:]), nil
}

// Learned from net.textproto. One difference is that this one keeps the
// ending '\n' in the returned line. Buf if there's only CRLF in the line,
// return nil for the line.
func readContinuedLineSlice(r *bufio.Reader) ([]byte, error) {
	// feedly.com request headers contains things like:
	// "$Authorization.feedly: $FeedlyAuth\r\n", so we must test for only
	// continuation spaces.
	isspace := func(b byte) bool {
		return b == ' ' || b == '\t'
	}

	// Read the first line.
	line, err := r.ReadSlice('\n')
	if err != nil {
		return nil, err
	}

	// There are servers that use \n for line ending, so trim first before check ending.
	// For example, the 404 page for http://plan9.bell-labs.com/magic/man2html/1/2l
	trimmed := TrimSpace(line)
	if len(trimmed) == 0 {
		if len(line) > 2 {
			return nil, fmt.Errorf("malformed end of headers, len: %d, %#v", len(line), string(line))
		}
		return nil, nil
	}

	if isspace(line[0]) {
		return nil, fmt.Errorf("malformed header, start with space: %#v", string(line))
	}

	// Optimistically assume that we have started to buffer the next line
	// and it starts with an ASCII letter (the next header key), so we can
	// avoid copying that buffered data around in memory and skipping over
	// non-existent whitespace.
	if r.Buffered() > 0 {
		peek, err := r.Peek(1)
		if err == nil && !isspace(peek[0]) {
			return line, nil
		}
	}

	var buf []byte
	buf = append(buf, trimmed...)

	// Read continuation lines.
	for skipSpace(r) > 0 {
		line, err := r.ReadSlice('\n')
		if err != nil {
			break
		}
		buf = append(buf, ' ')
		buf = append(buf, TrimTrailingSpace(line)...)
	}
	buf = append(buf, '\r', '\n')
	return buf, nil
}

func skipSpace(r *bufio.Reader) int {
	n := 0
	for {
		c, err := r.ReadByte()
		if err != nil {
			// Bufio will keep err until next read.
			break
		}
		if c != ' ' && c != '\t' {
			r.UnreadByte()
			break
		}
		n++
	}
	return n
}

// Only add headers that are of interest for a proxy into request/response's header map.
func (h *Header) parseHeader(reader *bufio.Reader, raw *bytes.Buffer, url *URL) (err error) {
	h.ContLen = -1
	for {
		var line, name, val []byte
		if line, err = readContinuedLineSlice(reader); err != nil || len(line) == 0 {
			return
		}
		if name, val, err = splitHeader(line); err != nil {
			errl.Printf("split header %v\nline: %s\nraw header:\n%s\n", err, line, raw.Bytes())
			return
		}
		// Wait Go to solve/provide the string<->[]byte optimization
		kn := string(name)
		if parseFunc, ok := headerParser[kn]; ok {
			if len(val) == 0 {
				continue
			}
			if err = parseFunc(h, val); err != nil {
				errl.Printf("parse header %v\nline: %s\nraw header:\n%s\n", err, line, raw.Bytes())
				return
			}
		}
		if hopByHopHeader[kn] {
			continue
		}
		raw.Write(line)
		// debug.Printf("len %d %s", len(s), s)
	}
}

// Parse the request line and header, does not touch body
func parseRequest(c *clientConn, r *Request) (err error) {
	var s []byte
	reader := c.bufRd
	c.setReadTimeout("parseRequest")
	// parse request line
	if s, err = reader.ReadSlice('\n'); err != nil {
		if isErrTimeout(err) {
			return errClientTimeout
		}
		return err
	}
	c.unsetReadTimeout("parseRequest")
	// debug.Printf("Request line %s", s)

	r.reset()
	if config.saveReqLine {
		r.raw.Write(s)
		r.reqLnStart = len(s)
	}

	var f [][]byte
	// Tolerate with multiple spaces and '\t' is achieved by FieldsN.
	if f = FieldsN(s, 3); len(f) != 3 {
		return fmt.Errorf("malformed request line: %#v", string(s))
	}
	ASCIIToUpperInplace(f[0])
	r.Method = string(f[0])

	// Parse URI into host and path
	r.URL, err = ParseRequestURIBytes(f[1])
	if err != nil {
		return
	}
	r.Header.Host = r.URL.HostPort // If Header.Host is set, parseHost will just return.
	if r.Method == "CONNECT" {
		r.isConnect = true
		if bool(dbgRq) && verbose && !config.saveReqLine {
			r.raw.Write(s)
		}
	} else {
		r.genRequestLine()
	}
	r.headStart = r.raw.Len()

	// Read request header.
	if err = r.parseHeader(reader, r.raw, r.URL); err != nil {
		errl.Printf("parse request header: %v %s\n%s", err, r, r.Verbose())
		return err
	}
	if r.Chunking {
		r.raw.WriteString(fullHeaderTransferEncoding)
	}
	if r.ConnectionKeepAlive {
		r.raw.WriteString(fullHeaderConnectionKeepAlive)
	} else {
		r.raw.WriteString(fullHeaderConnectionClose)
	}
	// The spec says proxy must add Via header. polipo disables this by
	// default, and I don't want to let others know the user is using COW, so
	// don't add it.
	r.raw.WriteString(CRLF)
	r.bodyStart = r.raw.Len()
	return
}

// If an http response may have message body
func (rp *Response) hasBody(method string) bool {
	if method == "HEAD" || rp.Status == 304 || rp.Status == 204 ||
		rp.Status < 200 {
		return false
	}
	return true
}

// Parse response status and headers.
func parseResponse(sv *serverConn, r *Request, rp *Response) (err error) {
	var s []byte
	reader := sv.bufRd
	if sv.maybeFake() {
		sv.setReadTimeout("parseResponse")
	}
	if s, err = reader.ReadSlice('\n'); err != nil {
		// err maybe timeout caused by explicity setting deadline, EOF, or
		// reset caused by GFW.
		debug.Printf("read response status line %v %v\n", err, r)
		// Server connection with error will not be used any more, so no need
		// to unset timeout.
		// For read error, return directly in order to identify whether this
		// is caused by GFW.
		return err
	}
	if sv.maybeFake() {
		sv.unsetReadTimeout("parseResponse")
	}
	// debug.Printf("Response line %s", s)

	// response status line parsing
	var f [][]byte
	if f = FieldsN(s, 3); len(f) < 2 { // status line are separated by SP
		return fmt.Errorf("malformed response status line: %#v %v", string(s), r)
	}
	status, err := ParseIntFromBytes(f[1], 10)

	rp.reset()
	rp.Status = int(status)
	if err != nil {
		return fmt.Errorf("response status not valid: %s len=%d %v", f[1], len(f[1]), err)
	}
	if len(f) == 3 {
		rp.Reason = f[2]
	}

	proto := f[0]
	if !(bytes.Equal(proto[0:7], []byte("HTTP/1.")) || bytes.Equal(proto[0:7], []byte("HTTP/2."))) {
		return fmt.Errorf("invalid response status line: %s request %v", string(f[0]), r)
	}
	if proto[7] == '1' {
		rp.raw.Write(s)
	} else if proto[7] == '0' {
		// Should return HTTP version as 1.1 to client since closed connection
		// will be converted to chunked encoding
		rp.genStatusLine()
	} else {
		return fmt.Errorf("response protocol not supported: %s", f[0])
	}

	if err = rp.parseHeader(reader, rp.raw, r.URL); err != nil {
		errl.Printf("parse response header: %v %s\n%s", err, r, rp.Verbose())
		return err
	}

	//Check for http error code from config file
	if config.HttpErrorCode > 0 && rp.Status == config.HttpErrorCode {
		debug.Println("Requested http code is raised")
		return CustomHttpErr
	}

	if rp.Status == statusCodeContinue && !r.ExpectContinue {
		// not expecting 100-continue, just ignore it and read final response
		errl.Println("Ignore server 100 response for", r)
		return parseResponse(sv, r, rp)
	}

	if rp.Chunking {
		rp.raw.WriteString(fullHeaderTransferEncoding)
	} else if rp.ContLen == -1 {
		// No chunk, no content length, assume close to signal end.
		rp.ConnectionKeepAlive = false
		if rp.hasBody(r.Method) {
			// Connection close, no content length specification.
			// Use chunked encoding to pass content back to client.
			debug.Println("add chunked encoding to close connection response", r, rp)
			rp.raw.WriteString(fullHeaderTransferEncoding)
		} else if !(rp.Status == 304 || rp.Status == 204) {
			debug.Println("add content-length 0 to close connection response", r, rp)
			rp.raw.WriteString("Content-Length: 0\r\n")
		}
	}
	// Whether COW should respond with keep-alive depends on client request,
	// not server response.
	if r.ConnectionKeepAlive {
		rp.raw.WriteString(fullHeaderConnectionKeepAlive)
		rp.raw.WriteString(fullKeepAliveHeader)
	} else {
		rp.raw.WriteString(fullHeaderConnectionClose)
	}
	rp.raw.WriteString(CRLF)

	return nil
}

func unquote(s string) string {
	return strings.Trim(s, "\"")
}

func parseKeyValueList(str string) map[string]string {
	list := strings.Split(str, ",")
	if len(list) == 1 && list[0] == "" {
		return nil
	}
	res := make(map[string]string)
	for _, ele := range list {
		kv := strings.SplitN(strings.TrimSpace(ele), "=", 2)
		if len(kv) != 2 {
			errl.Println("no equal sign in key value list element:", ele)
			return nil
		}
		key, val := kv[0], unquote(kv[1])
		res[key] = val
	}
	return res
}
