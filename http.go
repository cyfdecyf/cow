package main

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/cyfdecyf/bufio"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

type Header struct {
	ContLen             int64
	KeepAlive           time.Duration
	ProxyAuthorization  string
	Chunking            bool
	ConnectionKeepAlive bool
	ExpectContinue      bool
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

const (
	statusCodeContinue = 100
)

const (
	statusBadReq       = "400 Bad Request"
	statusExpectFailed = "417 Expectation Failed"
)

type Response struct {
	Status int
	Reason []byte

	Header

	raw     *bytes.Buffer
	rawByte []byte
}

var zeroResponse = Response{}

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

func (rp *Response) genStatusLine() (res string) {
	if len(rp.Reason) == 0 {
		res = strings.Join([]string{"HTTP/1.1", strconv.Itoa(rp.Status)}, " ")
	} else {
		res = strings.Join([]string{"HTTP/1.1", strconv.Itoa(rp.Status), string(rp.Reason)}, " ")
	}
	res += CRLF
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

// For port, return empty string if no port specified.
// This also works for IPv6 address.
func splitHostPort(s string) (host, port string) {
	if len(s) == 0 {
		return "", ""
	}
	// Common case should has no port, check the last char first
	if !IsDigit(s[len(s)-1]) {
		return s, ""
	}
	// Scan back, make sure we find ':'
	for i := len(s) - 2; i >= 0; i-- {
		c := s[i]
		switch {
		case c == ':':
			return s[:i], s[i+1:]
		case !IsDigit(c):
			return s, ""
		}
	}
	return s, ""
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

	var f [][]byte
	var rest, scheme []byte
	f = bytes.SplitN(rawurl, []byte("://"), 2)
	if len(f) == 1 {
		rest = f[0]
		scheme = []byte("http") // default to http
	} else {
		ASCIIToLowerInplace(f[0]) // it's ok to lower case scheme
		scheme = f[0]
		if !bytes.Equal(scheme, []byte("http")) && !bytes.Equal(scheme, []byte("https")) {
			errl.Printf("%s protocol not supported\n", scheme)
			return nil, errors.New("protocol not supported")
		}
		rest = f[1]
	}

	var hostport, host, port, path string
	id := bytes.IndexByte(rest, '/')
	if id == -1 {
		hostport = string(rest)
	} else {
		hostport = string(rest[:id])
		path = string(rest[id:])
	}

	// Must add port in host so it can be used as key to find the correct
	// server connection.
	// e.g. google.com:80 and google.com:443 should use different connections.
	host, port = splitHostPort(hostport)
	if port == "" {
		if len(scheme) == 4 {
			hostport = net.JoinHostPort(host, "80")
			port = "80"
		} else {
			hostport = net.JoinHostPort(host, "443")
			port = "443"
		}
	}

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
	headerKeepAlive          = "keep-alive"
	headerProxyAuthenticate  = "proxy-authenticate"
	headerProxyAuthorization = "proxy-authorization"
	headerProxyConnection    = "proxy-connection"
	headerReferer            = "referer"
	headerTE                 = "te"
	headerTrailer            = "trailer"
	headerTransferEncoding   = "transfer-encoding"
	headerUpgrade            = "upgrade"
	headerExpect             = "expect"

	fullHeaderConnectionKeepAlive = "Connection: keep-alive\r\n"
	fullHeaderConnectionClose     = "Connection: close\r\n"
	fullHeaderTransferEncoding    = "Transfer-Encoding: chunked\r\n"
)

// Using Go's method expression
var headerParser = map[string]HeaderParserFunc{
	headerConnection:         (*Header).parseConnection,
	headerContentLength:      (*Header).parseContentLength,
	headerKeepAlive:          (*Header).parseKeepAlive,
	headerProxyAuthorization: (*Header).parseProxyAuthorization,
	headerProxyConnection:    (*Header).parseConnection,
	headerTransferEncoding:   (*Header).parseTransferEncoding,
	headerExpect:             (*Header).parseExpect,
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

type HeaderParserFunc func(*Header, []byte, *bytes.Buffer) error

// Used by both "Connection" and "Proxy-Connection" header. COW always adds
// connection header at the end of a request/response (in parseRequest and
// parseResponse), no matter whether the original one has this header or not.
// This will change the order of headers, but should be OK as RFC2616 4.2 says
// header order is not significant. (Though general-header first is "good-
// practice".)
func (h *Header) parseConnection(s []byte, raw *bytes.Buffer) error {
	ASCIIToLowerInplace(s)
	h.ConnectionKeepAlive = !bytes.Contains(s, []byte("close"))
	return nil
}

func (h *Header) parseContentLength(s []byte, raw *bytes.Buffer) (err error) {
	h.ContLen, err = ParseIntFromBytes(s, 10)
	return err
}

func (h *Header) parseKeepAlive(s []byte, raw *bytes.Buffer) (err error) {
	ASCIIToLowerInplace(s)
	id := bytes.Index(s, []byte("timeout="))
	if id != -1 {
		id += len("timeout=")
		end := id
		for ; end < len(s) && IsDigit(s[end]); end++ {
		}
		delta, _ := ParseIntFromBytes(s[id:end], 10)
		h.KeepAlive = time.Second * time.Duration(delta)
	}
	return nil
}

func (h *Header) parseProxyAuthorization(s []byte, raw *bytes.Buffer) error {
	h.ProxyAuthorization = string(s)
	return nil
}

func (h *Header) parseTransferEncoding(s []byte, raw *bytes.Buffer) error {
	ASCIIToLowerInplace(s)
	// For transfer-encoding: identify, it's the same as specifying neither
	// content-length nor transfer-encoding.
	h.Chunking = bytes.Contains(s, []byte("chunked"))
	if h.Chunking {
		raw.WriteString(fullHeaderTransferEncoding)
	} else if !bytes.Contains(s, []byte("identity")) {
		errl.Printf("invalid transfer encoding: %s\n", s)
		return errNotSupported
	}
	return nil
}

// For now, cow does not fully support 100-continue. It will return "417
// expectation failed" if a request contains expect header. This is one of the
// strategies supported by polipo, which is easiest to implement in cow.
// TODO If we see lots of expect 100-continue usage, provide full support.

func (h *Header) parseExpect(s []byte, raw *bytes.Buffer) error {
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
	var f [][]byte
	if f = bytes.SplitN(s, []byte{':'}, 2); len(f) != 2 {
		errl.Printf("malformed header: %s\n", s)
		return nil, nil, errMalformHeader
	}
	// Do not lower case field value, as it maybe case sensitive
	return ASCIIToLower(f[0]), f[1], nil
}

// Only add headers that are of interest for a proxy into request/response's header map.
func (h *Header) parseHeader(reader *bufio.Reader, raw *bytes.Buffer, url *URL) (err error) {
	h.ContLen = -1
	dummyLastLine := []byte{}
	// Read request header and body
	var s, name, val, lastLine []byte
	for {
		if s, err = reader.ReadSlice('\n'); err != nil {
			return
		}
		// There are servers that use \n for line ending, so trim first before check ending.
		// For example, the 404 page for http://plan9.bell-labs.com/magic/man2html/1/2l
		trimmed := TrimSpace(s)
		if len(trimmed) == 0 { // end of headers
			return
		}
		if (s[0] == ' ' || s[0] == '\t') && lastLine != nil { // multi-line header
			// I've never seen multi-line header used in headers that's of interest.
			// Disable multi-line support to avoid copy for now.
			errl.Printf("Multi-line support disabled: %v %s", url, s)
			return errNotSupported
			// combine previous line with current line
			// trimmed = bytes.Join([][]byte{lastLine, []byte{' '}, trimmed}, nil)
		}
		if name, val, err = splitHeader(trimmed); err != nil {
			return
		}
		// Wait Go to solve/provide the string<->[]byte optimization
		kn := string(name)
		if parseFunc, ok := headerParser[kn]; ok {
			// lastLine = append([]byte(nil), trimmed...) // copy to avoid next read invalidating the trimmed line
			lastLine = dummyLastLine
			val = TrimSpace(val)
			if len(val) == 0 {
				continue
			}
			parseFunc(h, val, raw)
		} else {
			// mark this header as not of interest to proxy
			lastLine = nil
		}
		if hopByHopHeader[kn] {
			continue
		}
		raw.Write(s)
		// debug.Printf("len %d %s", len(s), s)
	}
}

// Parse the request line and header, does not touch body
func parseRequest(c *clientConn, r *Request) (err error) {
	var s []byte
	reader := c.bufRd
	setConnReadTimeout(c, clientConnTimeout, "parseRequest")
	// parse request line
	if s, err = reader.ReadSlice('\n'); err != nil {
		if isErrTimeout(err) {
			return errClientTimeout
		}
		return err
	}
	unsetConnReadTimeout(c, "parseRequest")
	// debug.Printf("Request line %s", s)

	r.reset()
	// for http parent proxy, store the original request line
	if config.hasHttpParent {
		r.raw.Write(s)
		r.reqLnStart = len(s)
	}

	var f [][]byte
	// Tolerate with multiple spaces and '\t' is achieved by FieldsN.
	if f = FieldsN(s, 3); len(f) != 3 {
		return errors.New(fmt.Sprintf("malformed HTTP request: %s", s))
	}
	ASCIIToUpperInplace(f[0])
	r.Method = string(f[0])

	// Parse URI into host and path
	r.URL, err = ParseRequestURIBytes(f[1])
	if err != nil {
		return
	}
	if r.Method == "CONNECT" {
		r.isConnect = true
		if bool(dbgRq) && verbose && !config.hasHttpParent {
			r.raw.Write(s)
		}
	} else {
		// Generate normal HTTP request line
		r.raw.WriteString(r.Method + " ")
		if len(r.URL.Path) == 0 {
			r.raw.WriteString("/")
		} else {
			r.raw.WriteString(r.URL.Path)
		}
		r.raw.WriteString(" HTTP/1.1\r\n")
	}
	r.headStart = r.raw.Len()

	// Read request header
	if err = r.parseHeader(reader, r.raw, r.URL); err != nil {
		errl.Printf("Parsing request header: %v\n", err)
		return err
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

func skipCRLF(r *bufio.Reader) error {
	// There maybe servers using single '\n' for line ending
	if _, err := r.ReadSlice('\n'); err != nil {
		errl.Println("Error reading CRLF:", err)
		return err
	}
	return nil
}

// If an http response may have message body
func (rp *Response) hasBody(method string) bool {
	if method == "HEAD" || rp.Status == 304 || rp.Status == 204 ||
		(100 <= rp.Status && rp.Status < 200) {
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
		if err != io.EOF {
			// err maybe timeout caused by explicity setting deadline
			debug.Printf("Reading Response status line: %v %v\n", err, r)
		}
		// For timeout, the connection will not be used, so no need to unset timeout
		return err
	}
	if sv.maybeFake() {
		sv.unsetReadTimeout("parseResponse")
	}
	// debug.Printf("Response line %s", s)

	// response status line parsing
	var f [][]byte
	if f = FieldsN(s, 3); len(f) < 2 { // status line are separated by SP
		errl.Printf("Malformed HTTP response status line: %s %v\n", s, r)
		return errMalformResponse
	}
	status, err := ParseIntFromBytes(f[1], 10)

	rp.reset()
	rp.Status = int(status)
	if err != nil {
		errl.Printf("response status not valid: %s len=%d %v\n", f[1], len(f[1]), err)
		return
	}
	if len(f) == 3 {
		rp.Reason = f[2]
	}

	proto := f[0]
	if !bytes.Equal(proto[0:7], []byte("HTTP/1.")) {
		errl.Printf("Invalid response status line: %s\n", string(f[0]))
		return errMalformResponse
	}
	if proto[7] == '1' {
		rp.raw.Write(s)
	} else if proto[7] == '0' {
		// Should return HTTP version as 1.1 to client since closed connection
		// will be converted to chunked encoding
		rp.raw.WriteString(rp.genStatusLine())
	} else {
		errl.Printf("Response protocol not supported: %s\n", f[0])
		return errNotSupported
	}

	if err = rp.parseHeader(reader, rp.raw, r.URL); err != nil {
		errl.Printf("Reading response header: %v %v\n", err, r)
		return err
	}

	if rp.Status == statusCodeContinue && !r.ExpectContinue {
		// not expecting 100-continue, just ignore it and read final response
		errl.Println("Ignore server 100 response for", r)
		return parseResponse(sv, r, rp)
	}

	// Connection close, no content length specification
	// Use chunked encoding to pass content back to client
	if !rp.ConnectionKeepAlive && !rp.Chunking && rp.ContLen == -1 {
		rp.raw.WriteString("Transfer-Encoding: chunked\r\n")
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
