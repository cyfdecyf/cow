package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type Header struct {
	ContLen   int64
	Chunking  bool
	KeepAlive bool
}

type Request struct {
	Method string
	URL    *URL
	Proto  string

	Header
	isConnect bool

	raw bytes.Buffer
}

func (r *Request) String() (s string) {
	s = fmt.Sprintf("[Request] %s %s%s", r.Method,
		r.URL.Host, r.URL.Path)
	if debug {
		s += fmt.Sprintf("\n%v", r.raw.String())
	}
	return
}

type Response struct {
	Status  string
	Reason  string
	HasBody bool

	Header

	raw bytes.Buffer
}

func (rp *Response) String() string {
	return rp.raw.String()
}

type URL struct {
	Host string
	Path string
}

func (url *URL) String() string {
	return fmt.Sprintf("%s%s", url.Host, url.Path)
}

// TODO Rename to protocol error just as the http pkg
type HttpError struct {
	msg string
}

// headers of interest to a proxy
// Define them as constant and use editor's completion to avoid typos.
// Note RFC2616 only says about "Connection", no "Proxy-Connection", but firefox
// send this header.
// See more at http://homepage.ntlworld.com/jonathan.deboynepollard/FGA/web-proxy-connection-header.html
const (
	headerContentLength    = "content-length"
	headerTransferEncoding = "transfer-encoding"
	headerConnection       = "connection"
	headerProxyConnection  = "proxy-connection"
)

func (he *HttpError) Error() string { return he.msg }

func newHttpError(msg string, err error) *HttpError {
	return &HttpError{fmt.Sprintln(msg, err)}
}

func hostHasPort(s string) bool {
	// Common case should has no port, check the last char first
	if !IsDigit(s[len(s)-1]) {
		return false
	}
	// Scan back, make sure we find ':'
	for i := len(s) - 2; i > 0; i-- {
		c := s[i]
		switch {
		case c == ':':
			return true
		case !IsDigit(c):
			return false
		}
	}
	return false
}

// net.ParseRequestURI will unescape encoded path, but the proxy don't need
// Assumes the input rawurl valid. Even if rawurl is not valid, net.Dial
// will check the correctness of the host.
func ParseRequestURI(rawurl string) (*URL, error) {
	if rawurl[0] == '/' {
		return nil, &HttpError{"Invalid proxy request URI: " + rawurl}
	}

	var f []string
	var rest string
	f = strings.SplitN(rawurl, "://", 2)
	if len(f) == 1 {
		rest = f[0]
	} else {
		scheme := f[0]
		if scheme != "http" && scheme != "https" {
			return nil, &HttpError{scheme + " protocol not supported"}
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

	return &URL{Host: host, Path: path}, nil
}

func splitHeader(s string) (name, val string, err error) {
	var f []string
	if f = strings.SplitN(strings.ToLower(s), ":", 2); len(f) != 2 {
		// TODO Fix this when encounter such web servers
		return "", "", &HttpError{"Multi-line header not supported"}
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

type HeaderParserFunc func(*Header, string) error

// Using Go's method expression
var headerParser = map[string]HeaderParserFunc{
	headerConnection:       (*Header).parseConnection,
	headerProxyConnection:  (*Header).parseConnection,
	headerContentLength:    (*Header).parseContentLength,
	headerTransferEncoding: (*Header).parseTransferEncoding,
}

// Only add headers that are of interest for a proxy into request's header map.
func (h *Header) parseHeader(reader *bufio.Reader, raw *bytes.Buffer, addHeader string) (err error) {
	// Read request header and body
	var s, name, val string
	for {
		if s, err = ReadLine(reader); err != nil {
			return
		}
		if s == "" {
			raw.WriteString(addHeader)
			raw.WriteString("\r\n")
			break
		}
		if name, val, err = splitHeader(s); err != nil {
			return
		}
		if parseFunc, ok := headerParser[name]; ok {
			parseFunc(h, val)
			if name == headerConnection || name == headerProxyConnection {
				// Don't pass connection header to server or client
				continue
			}
		}
		raw.WriteString(s)
		raw.WriteString("\r\n")
		// debug.Printf("len %d %s", len(s), s)
	}
	return
}

// Consume all http header. Used for CONNECT method.
func drainHeader(reader *bufio.Reader) (err error) {
	// Read request header and body
	var s string
	for {
		s, err = ReadLine(reader)
		if s == "" || err != nil {
			return
		}
	}
	return
}

// Parse the initial line and header, does not touch body
func parseRequest(reader *bufio.Reader) (r *Request, err error) {
	r = new(Request)
	r.ContLen = -1
	var s string

	// parse initial request line
	if s, err = ReadLine(reader); err != nil {
		return nil, err
	}
	// debug.Printf("Request initial line %s", s)

	var f []string
	if f = strings.SplitN(s, " ", 3); len(f) < 3 {
		return nil, &HttpError{"malformed HTTP request"}
	}
	var requestURI string
	r.Method, requestURI, r.Proto = f[0], f[1], f[2]

	// Parse URI into host and path
	r.URL, err = ParseRequestURI(requestURI)
	if err != nil {
		return
	}
	if r.Method == "CONNECT" {
		// Consume remaining header and just return. Headers are not used for
		// CONNECT method.
		r.isConnect = true
		r.KeepAlive = false
		err = drainHeader(reader)
		return
	}

	r.genRequestLine()

	// Read request header
	if err = r.parseHeader(reader, &r.raw, ""); err != nil {
		return nil, newHttpError("Parsing request header", err)
	}
	return
}

func (r *Request) genRequestLine() {
	r.raw.WriteString(r.Method)
	r.raw.WriteString(" ")
	r.raw.WriteString(r.URL.Path)
	r.raw.WriteString(" ")
	r.raw.WriteString("HTTP/1.1\r\n")
	// TODO Set this to Keep-Alive after supporting HTTP/1.1 persistent connection
	r.raw.WriteString("Connection: close\r\n")
}

var crlfBuf = make([]byte, 2)

func readCheckCRLF(reader *bufio.Reader) error {
	if _, err := io.ReadFull(reader, crlfBuf); err != nil {
		return err
	}
	if crlfBuf[0] != '\r' || crlfBuf[1] != '\n' {
		return &HttpError{"Not CRLF"}
	}
	return nil
}

// If an http response may have message body
func responseMayHaveBody(method, status string) bool {
	// when we have tenary search tree, can optimize this a little
	return !(method == "HEAD" || status == "304" || status == "204" || strings.HasPrefix(status, "1"))
}

// Parse response status and headers. The request method is needed to
// determine if response may have body, also for debugging
func parseResponse(reader *bufio.Reader, method string) (rp *Response, err error) {
	rp = new(Response)
	rp.ContLen = -1

	var s string
START:
	if s, err = ReadLine(reader); err != nil {
		return nil, newHttpError("Reading Response status line:", err)
	}
	var f []string
	if f = strings.SplitN(s, " ", 3); len(f) < 3 {
		return nil, &HttpError{fmt.Sprintln("malformed HTTP response status line:", s)}
	}
	// Handle 1xx response
	if f[1] == "100" {
		if err = readCheckCRLF(reader); err != nil {
			return nil, newHttpError("Reading CRLF after 1xx response", err)
		}
		goto START
	}
	rp.Status = f[1]
	rp.Reason = f[2]
	rp.HasBody = responseMayHaveBody(method, rp.Status)

	rp.raw.WriteString(s)
	rp.raw.WriteString("\r\n")

	if err = rp.parseHeader(reader, &rp.raw, "Connection: Keep-Alive\r\n"); err != nil {
		return nil, err
	}

	return rp, nil
}
