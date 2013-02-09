package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

type Header struct {
	ContLen             int64
	KeepAlive           time.Duration
	Referer             string
	ProxyAuthorization  string
	Chunking            bool
	ConnectionKeepAlive bool
}

type rqState byte

const (
	rsCreated  rqState = iota
	rsSent             // request has been sent to server
	rsRecvBody         // response header received, receiving response body
	rsDone
)

type Request struct {
	Method      string
	URL         *URL
	contBuf     *bytes.Buffer // will be non nil when retrying request
	raw         bytes.Buffer
	origReqLine []byte // original request line from client, used for http parent proxy
	headerStart int    // start of header in raw
	Header
	isConnect bool
	state     rqState
	tryCnt    byte
}

func (r *Request) String() (s string) {
	s = fmt.Sprintf("%s %s%s", r.Method,
		r.URL.HostPort, r.URL.Path)
	if verbose {
		s += fmt.Sprintf("\n%v", r.raw.String())
	}
	return
}

func (r *Request) isRetry() bool {
	return r.tryCnt > 1
}

func (r *Request) tryOnce() {
	r.tryCnt++
}

func (r *Request) tooMuchRetry() bool {
	return r.tryCnt > 3
}

type Response struct {
	Status int
	Reason []byte

	Header

	raw bytes.Buffer
}

func (rp *Response) genStatusLine() (res string) {
	if len(rp.Reason) == 0 {
		res = strings.Join([]string{"HTTP/1.1", strconv.Itoa(rp.Status)}, " ")
	} else {
		res = strings.Join([]string{"HTTP/1.1", strconv.Itoa(rp.Status), string(rp.Reason)}, " ")
	}
	return
}

func (rp *Response) String() string {
	var r string
	if verbose {
		r = rp.raw.String()
	} else {
		r = fmt.Sprintf("%s %s", rp.Status, rp.Reason)
	}
	return r
}

type URL struct {
	HostPort string // must contain port
	Host     string // no port
	Port     string
	Domain   string
	Path     string
	Scheme   string
}

func (url *URL) String() string {
	return url.HostPort + url.Path
}

func (url *URL) toURI() string {
	return url.Scheme + "://" + url.String()
}

func (url *URL) HostIsIP() bool {
	return hostIsIP(url.Host)
}

// For port, return empty string if no port specified.
func splitHostPort(s string) (host, port string) {
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
	if rawurl[0] == '/' {
		return &URL{Path: rawurl}, nil
	}

	var f []string
	var rest, scheme string
	f = strings.SplitN(rawurl, "://", 2)
	if len(f) == 1 {
		rest = f[0]
		scheme = "http" // default to http
	} else {
		scheme = strings.ToLower(f[0])
		if scheme != "http" && scheme != "https" {
			msg := scheme + " protocol not supported"
			errl.Println(msg)
			return nil, errors.New(msg)
		}
		rest = f[1]
	}

	var hostport, host, port, path string
	id := strings.Index(rest, "/")
	if id == -1 {
		hostport = rest
	} else {
		hostport = rest[:id]
		path = rest[id:]
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

	return &URL{hostport, host, port, host2Domain(host), path, scheme}, nil
}

// headers of interest to a proxy
// Define them as constant and use editor's completion to avoid typos.
// Note RFC2616 only says about "Connection", no "Proxy-Connection", but firefox
// send this header.
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

	fullHeaderConnection       = "Connection: keep-alive\r\n"
	fullHeaderTransferEncoding = "Transfer-Encoding: chunked\r\n"
)

// Using Go's method expression
var headerParser = map[string]HeaderParserFunc{
	headerConnection:         (*Header).parseConnection,
	headerContentLength:      (*Header).parseContentLength,
	headerKeepAlive:          (*Header).parseKeepAlive,
	headerProxyAuthorization: (*Header).parseProxyAuthorization,
	headerProxyConnection:    (*Header).parseConnection,
	headerTransferEncoding:   (*Header).parseTransferEncoding,
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

type HeaderParserFunc func(*Header, []byte, *bytes.Buffer) error

func (h *Header) parseConnection(s []byte, raw *bytes.Buffer) error {
	h.ConnectionKeepAlive = bytes.Contains(s, []byte("keep-alive"))
	raw.WriteString(fullHeaderConnection)
	return nil
}

func (h *Header) parseContentLength(s []byte, raw *bytes.Buffer) (err error) {
	h.ContLen, err = strconv.ParseInt(string(TrimSpace(s)), 10, 64)
	return err
}

func (h *Header) parseKeepAlive(s []byte, raw *bytes.Buffer) (err error) {
	id := bytes.Index(s, []byte("timeout="))
	if id != -1 {
		id += len("timeout=")
		end := id
		for ; end < len(s) && IsDigit(s[end]); end++ {
		}
		delta, _ := strconv.Atoi(string(s[id:end]))
		h.KeepAlive = time.Second * time.Duration(delta)
	}
	return nil
}

func (h *Header) parseProxyAuthorization(s []byte, raw *bytes.Buffer) error {
	h.ProxyAuthorization = string(TrimSpace(s))
	return nil
}

func (h *Header) parseTransferEncoding(s []byte, raw *bytes.Buffer) error {
	h.Chunking = bytes.Contains(s, []byte("chunked"))
	if h.Chunking {
		raw.WriteString(fullHeaderTransferEncoding)
	} else {
		errl.Printf("unsupported transfer encoding %s\n", string(s))
		return errNotSupported
	}
	return nil
}

func splitHeader(s []byte) (name, val []byte, err error) {
	var f [][]byte
	if f = bytes.SplitN(s, []byte{':'}, 2); len(f) != 2 {
		errl.Println("malformed header:", s)
		return nil, nil, errMalformHeader
	}
	// Do not lower case field value, as it maybe case sensitive
	return ASCIIToLower(f[0]), f[1], nil
}

// Only add headers that are of interest for a proxy into request's header map.
func (h *Header) parseHeader(reader *bufio.Reader, raw *bytes.Buffer, url *URL) (err error) {
	// Read request header and body
	var s, name, val, lastLine []byte
	for {
		if s, err = ReadLineBytes(reader); err != nil {
			return
		}
		if len(s) == 0 { // end of headers
			return
		}
		if (s[0] == ' ' || s[0] == '\t') && lastLine != nil { // multi-line header
			info.Printf("Encounter multi-line header: %v %s", url, string(s))
			// combine previous line with current line
			s = bytes.Join([][]byte{lastLine, []byte{' '}, s}, nil)
		}
		if name, val, err = splitHeader(s); err != nil {
			return
		}
		// Wait Go to solve/provide the string<->[]byte optimization
		kn := string(name)
		if parseFunc, ok := headerParser[kn]; ok {
			lastLine = s
			parseFunc(h, ASCIIToLower(val), raw)
		} else {
			// mark this header as not of interest to proxy
			lastLine = nil
		}
		if hopByHopHeader[kn] {
			continue
		}
		raw.Write(s)
		raw.WriteString(CRLF)
		// debug.Printf("len %d %s", len(s), s)
	}
	return
}

// Parse the initial line and header, does not touch body
func parseRequest(c *clientConn) (r *Request, err error) {
	var s []byte
	reader := c.bufRd
	setConnReadTimeout(c, clientConnTimeout, "parseRequest")
	// parse initial request line
	if s, err = ReadLineBytes(reader); err != nil {
		return nil, err
	}
	unsetConnReadTimeout(c, "parseRequest")
	// debug.Printf("Request initial line %s", s)

	r = new(Request)
	r.ContLen = -1

	var f [][]byte
	if f = bytes.SplitN(s, []byte{' '}, 3); len(f) < 3 { // request line are separated by SP
		return nil, errors.New(fmt.Sprintf("malformed HTTP request: %s", string(s)))
	}
	var requestURI string
	ASCIIToUpperInplace(f[0])
	r.Method, requestURI = string(f[0]), string(f[1])

	// Parse URI into host and path
	r.URL, err = ParseRequestURI(requestURI)
	if err != nil {
		return
	}
	if r.Method == "CONNECT" {
		r.isConnect = true
	} else {
		// Generate normal HTTP request line
		r.raw.WriteString(r.Method + " ")
		r.raw.WriteString(r.URL.Path)
		r.raw.WriteString(" HTTP/1.1\r\n")
	}

	// for http parent proxy, always need the initial request line
	if hasHttpParentProxy {
		r.origReqLine = make([]byte, len(s))
		copy(r.origReqLine, s)
		r.headerStart = r.raw.Len()
	}

	// Read request header
	if err = r.parseHeader(reader, &r.raw, r.URL); err != nil {
		errl.Printf("Parsing request header: %v\n", err)
		return nil, err
	}
	if !r.ConnectionKeepAlive {
		// Always add one connection header for request
		r.raw.WriteString(fullHeaderConnection)
	}
	r.raw.WriteString(CRLF)
	return
}

func (r *Request) responseNotSent() bool {
	return r.state <= rsSent
}

func readCheckCRLF(reader *bufio.Reader) error {
	crlfBuf := make([]byte, 2)
	if _, err := io.ReadFull(reader, crlfBuf); err != nil {
		return err
	}
	if crlfBuf[0] != '\r' || crlfBuf[1] != '\n' {
		return errChunkedEncode
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
func parseResponse(sv *serverConn, r *Request) (rp *Response, err error) {
	rp = new(Response)
	rp.ContLen = -1

	var s []byte
	reader := sv.bufRd
START:
	sv.setReadTimeout("parseResponse")
	if s, err = ReadLineBytes(reader); err != nil {
		if err != io.EOF {
			// err maybe timeout caused by explicity setting deadline
			debug.Printf("Reading Response status line: %v %v\n", err, r)
		}
		// For timeout, the connection will not be used, so no need to unset timeout
		return nil, err
	}
	sv.unsetReadTimeout("parseResponse")
	var f [][]byte
	if f = bytes.SplitN(s, []byte{' '}, 3); len(f) < 2 { // status line are separated by SP
		errl.Printf("Malformed HTTP response status line: %s %v\n", s, r)
		return nil, errMalformResponse
	}
	// Handle 1xx response
	if bytes.Equal(f[1], []byte("100")) {
		if err = readCheckCRLF(reader); err != nil {
			errl.Printf("Reading CRLF after 1xx response: %v %v\n", err, r)
			return nil, err
		}
		goto START
	}
	// Currently, one have to convert []byte to string to use strconv
	// Refer to: http://code.google.com/p/go/issues/detail?id=2632
	rp.Status, err = strconv.Atoi(string(f[1]))
	if err != nil {
		errl.Printf("response status not valid: %s %v\n", f[1], err)
		return
	}
	if len(f) == 3 {
		rp.Reason = f[2]
	}

	proto := f[0]
	if !bytes.Equal(proto[0:7], []byte("HTTP/1.")) {
		errl.Printf("Invalid response status line: %s\n", string(f[0]))
		return nil, errMalformResponse
	}
	if proto[7] == '1' {
		rp.raw.Write(s)
	} else if proto[7] == '0' {
		// Should return HTTP version as 1.1 to client since closed connection
		// will be converted to chunked encoding
		rp.raw.WriteString(rp.genStatusLine())
	} else {
		errl.Printf("Response protocol not supported: %s\n", string(f[0]))
		return nil, errNotSupported
	}
	rp.raw.WriteString(CRLF)

	if err = rp.parseHeader(reader, &rp.raw, r.URL); err != nil {
		errl.Printf("Reading response header: %v %v\n", err, r)
		return nil, err
	}
	// Connection close, no content length specification
	// Use chunked encoding to pass content back to client
	if !rp.ConnectionKeepAlive && !rp.Chunking && rp.ContLen == -1 {
		rp.raw.WriteString("Transfer-Encoding: chunked\r\n")
	}
	if !rp.ConnectionKeepAlive {
		rp.raw.WriteString("Connection: keep-alive\r\n")
	}
	rp.raw.WriteString(CRLF)

	return rp, nil
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
