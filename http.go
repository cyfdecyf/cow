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
)

type Header struct {
	ContLen            int64
	Chunking           bool
	KeepAlive          bool
	Referer            string
	ProxyAuthorization string
}

type rqState byte

const (
	rsCreated  rqState = iota
	rsSent             // request has been sent to server
	rsRecvBody         // response header received, receiving response body
	rsDone
)

type Request struct {
	Method string
	URL    *URL
	Proto  string

	Header
	isConnect bool

	raw     bytes.Buffer
	contBuf *bytes.Buffer // will be non nil when retrying request
	state   rqState
	tryCnt  int
}

func (r *Request) String() (s string) {
	s = fmt.Sprintf("%s %s%s", r.Method,
		r.URL.Host, r.URL.Path)
	if verbose {
		s += fmt.Sprintf("\n%v", r.raw.String())
	}
	return
}

type Response struct {
	Status string
	Reason string

	Header

	raw bytes.Buffer
}

func (rp *Response) genStatusLine() (res string) {
	if rp.Reason == "" {
		res = fmt.Sprintf("HTTP/1.1 %s", rp.Status)
	} else {
		res = fmt.Sprintf("HTTP/1.1 %s %s", rp.Status, rp.Reason)
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
	Host   string // must contain port
	Path   string
	Scheme string
}

func (url *URL) String() string {
	return url.Host + url.Path
}

func (url *URL) toURI() string {
	return url.Scheme + "://" + url.String()
}

// headers of interest to a proxy
// Define them as constant and use editor's completion to avoid typos.
// Note RFC2616 only says about "Connection", no "Proxy-Connection", but firefox
// send this header.
// See more at http://homepage.ntlworld.com/jonathan.deboynepollard/FGA/web-proxy-connection-header.html
// TODO: parse Keep-Alive header so we know when the server will close connection
const (
	headerContentLength      = "content-length"
	headerTransferEncoding   = "transfer-encoding"
	headerConnection         = "connection"
	headerProxyConnection    = "proxy-connection"
	headerReferer            = "referer"
	headerProxyAuthorization = "proxy-authorization"
)

// For port, return empty string if no port specified.
func splitHostPort(s string) (host, port string) {
	// Common case should has no port, check the last char first
	// Update: as port is always added, this is no longer common case in COW
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

// net.ParseRequestURI will unescape encoded path, but the proxy don't need
// Assumes the input rawurl valid. Even if rawurl is not valid, net.Dial
// will check the correctness of the host.
func ParseRequestURI(rawurl string) (*URL, error) {
	if rawurl[0] == '/' {
		return &URL{Host: "", Path: rawurl}, nil
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

	var host, path string
	f = strings.SplitN(rest, "/", 2)
	host = f[0]
	if len(f) == 1 || f[1] == "" {
		path = "/"
	} else {
		path = "/" + f[1]
	}
	// Must add port in host so it can be used as key to find the correct
	// server connection.
	// e.g. google.com:80 and google.com:443 should use different connections.
	_, port := splitHostPort(host)
	if port == "" {
		if len(scheme) == 4 {
			host = net.JoinHostPort(host, "80")
		} else {
			host = net.JoinHostPort(host, "443")
		}
	}

	return &URL{Host: host, Path: path, Scheme: scheme}, nil
}

func splitHeader(s string) (name, val string, err error) {
	var f []string
	if f = strings.SplitN(strings.ToLower(s), ":", 2); len(f) != 2 {
		return "", "", errMalformHeader
	}
	return f[0], f[1], nil
}

func (h *Header) parseContentLength(s string) (err error) {
	h.ContLen, err = strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return err
}

func (h *Header) parseConnection(s string) error {
	h.KeepAlive = strings.Contains(s, "keep-alive")
	return nil
}

func (h *Header) parseTransferEncoding(s string) error {
	h.Chunking = strings.Contains(s, "chunked")
	return nil
}

func (h *Header) parseReferer(s string) error {
	h.Referer = strings.TrimSpace(s)
	return nil
}

func (h *Header) parseProxyAuthorization(s string) error {
	h.ProxyAuthorization = strings.TrimSpace(s)
	return nil
}

type HeaderParserFunc func(*Header, string) error

// Using Go's method expression
var headerParser = map[string]HeaderParserFunc{
	headerConnection:         (*Header).parseConnection,
	headerProxyConnection:    (*Header).parseConnection,
	headerContentLength:      (*Header).parseContentLength,
	headerTransferEncoding:   (*Header).parseTransferEncoding,
	headerReferer:            (*Header).parseReferer,
	headerProxyAuthorization: (*Header).parseProxyAuthorization,
}

// Only add headers that are of interest for a proxy into request's header map.
func (h *Header) parseHeader(reader *bufio.Reader, raw *bytes.Buffer, url *URL) (err error) {
	// Read request header and body
	var s, name, val, lastLine string
	for {
		if s, err = ReadLine(reader); err != nil {
			return
		}
		if s == "" { // end of headers
			return
		}
		if (s[0] == ' ' || s[0] == '\t') && lastLine != "" { // multi-line header
			s = lastLine + " " + s // combine previous line with current line
			errl.Printf("Encounter multi-line header: %v %s", url, s)
		}
		if name, val, err = splitHeader(s); err != nil {
			return
		}
		if parseFunc, ok := headerParser[name]; ok {
			lastLine = s
			parseFunc(h, val)
			if name == headerConnection ||
				name == headerProxyConnection ||
				name == headerProxyAuthorization {
				continue
			}
		} else {
			// mark this header as not of interest to proxy
			lastLine = ""
		}
		raw.WriteString(s)
		raw.Write(CRLFbytes)
		// debug.Printf("len %d %s", len(s), s)
	}
	return
}

// Parse the initial line and header, does not touch body
func parseRequest(c *clientConn) (r *Request, err error) {
	var s string
	reader := c.bufRd
	setConnReadTimeout(c, clientConnTimeout, "BEFORE receiving client request")
	// parse initial request line
	if s, err = ReadLine(reader); err != nil {
		return nil, err
	}
	unsetConnReadTimeout(c, "AFTER receiving client request")
	// debug.Printf("Request initial line %s", s)

	r = new(Request)
	r.ContLen = -1

	var f []string
	if f = strings.SplitN(s, " ", 3); len(f) < 3 { // request line are separated by SP
		return nil, errors.New(fmt.Sprintf("malformed HTTP request: %s", s))
	}
	var requestURI string
	r.Method, requestURI, r.Proto = strings.ToUpper(f[0]), f[1], f[2]

	// Parse URI into host and path
	r.URL, err = ParseRequestURI(requestURI)
	if err != nil {
		return
	}
	if r.Method == "CONNECT" {
		// Consume remaining header and just return. Headers are not used for
		// CONNECT method.
		r.isConnect = true
	}

	r.genRequestLine()

	// Read request header
	if err = r.parseHeader(reader, &r.raw, r.URL); err != nil {
		errl.Printf("Parsing request header: %v\n", err)
		return nil, err
	}
	r.raw.Write(headerKeepAliveByte)
	r.raw.Write(CRLFbytes)
	return
}

func (r *Request) genRequestLine() {
	r.raw.WriteString(r.Method + " " + r.URL.Path)
	r.raw.WriteString(" HTTP/1.1\r\n")
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
	// when we have tenary search tree, can optimize this a little
	return !(method == "HEAD" ||
		rp.Status == "304" ||
		rp.Status == "204" ||
		strings.HasPrefix(rp.Status, "1"))
}

var malformedHTTPResponseErr = errors.New("malformed HTTP response")

var headerKeepAliveByte = []byte("Connection: Keep-Alive\r\n")
var headerConnCloseByte = []byte("Connection: close\r\n")

// Parse response status and headers.
func parseResponse(sv *serverConn, r *Request) (rp *Response, err error) {
	rp = new(Response)
	rp.ContLen = -1

	var s string
	reader := sv.bufRd
START:
	if sv.state == svConnected && sv.maybeFake() {
		setConnReadTimeout(sv, readTimeout, "BEFORE receiving the first response")
	}
	if s, err = ReadLine(reader); err != nil {
		if err != io.EOF {
			// err maybe timeout caused by explicity setting deadline
			debug.Printf("Reading Response status line: %v %v\n", err, r)
		}
		return nil, err
	}
	if sv.state == svConnected && sv.maybeFake() {
		unsetConnReadTimeout(sv, "AFTER receiving the first response")
	}
	var f []string
	if f = strings.SplitN(s, " ", 3); len(f) < 2 { // status line are separated by SP
		errl.Printf("Malformed HTTP response status line: %s %v\n", s, r)
		return nil, malformedHTTPResponseErr
	}
	// Handle 1xx response
	if f[1] == "100" {
		if err = readCheckCRLF(reader); err != nil {
			errl.Printf("Reading CRLF after 1xx response: %v %v\n", err, r)
			return nil, err
		}
		goto START
	}
	rp.Status = f[1]
	if len(f) == 3 {
		rp.Reason = f[2]
	}

	if f[0] == "HTTP/1.0" {
		// Should return HTTP version as 1.1 to client since closed connection
		// will be converted to chunked encoding
		rp.raw.WriteString(rp.genStatusLine())
	} else {
		rp.raw.WriteString(s)
	}
	rp.raw.Write(CRLFbytes)

	if err = rp.parseHeader(reader, &rp.raw, r.URL); err != nil {
		errl.Printf("Reading response header: %v %v\n", err, r)
		return nil, err
	}
	// Connection close, no content length specification
	// Use chunked encoding to pass content back to client
	if !rp.KeepAlive && !rp.Chunking && rp.ContLen == -1 {
		rp.raw.WriteString("Transfer-Encoding: chunked\r\n")
	}
	if r.KeepAlive {
		rp.raw.Write(headerKeepAliveByte)
	} else {
		rp.raw.Write(headerConnCloseByte)
	}
	rp.raw.Write(CRLFbytes)

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
