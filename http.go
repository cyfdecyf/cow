package main

import (
	"bufio"
	"bytes"
	"errors"
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
	s = fmt.Sprintf("%s %s%s", r.Method,
		r.URL.Host, r.URL.Path)
	if verbose {
		s += fmt.Sprintf("\n%v", r.raw.String())
	}
	return
}

func (r *Request) toURI() string {
	return r.Proto + "://" + r.URL.String()
}

type Response struct {
	Status string
	Reason string

	Header

	raw bytes.Buffer
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
	Host string // must contain port
	Path string
}

func (url *URL) String() string {
	return fmt.Sprintf("%s%s", url.Host, url.Path)
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

// For port, return empty string if no port specified.
func splitHostPort(s string) (host, port string) {
	// Common case should has no port, check the last char first
	if !IsDigit(s[len(s)-1]) {
		return s, ""
	}
	// Scan back, make sure we find ':'
	for i := len(s) - 2; i > 0; i-- {
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
		// OS X seems to send only path to the server if the url is 127.0.0.1
		return &URL{Host: "", Path: rawurl}, nil
		// return nil, errors.New("Invalid proxy request URI: " + rawurl)
	}

	var f []string
	var rest string
	f = strings.SplitN(rawurl, "://", 2)
	if len(f) == 1 {
		rest = f[0]
	} else {
		scheme := strings.ToLower(f[0])
		if scheme != "http" && scheme != "https" {
			return nil, errors.New(scheme + " protocol not supported")
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
	_, port := splitHostPort(host)
	if port == "" {
		host += ":80"
	}

	return &URL{Host: host, Path: path}, nil
}

func splitHeader(s string) (name, val string, err error) {
	var f []string
	if f = strings.SplitN(strings.ToLower(s), ":", 2); len(f) != 2 {
		// TODO Fix this when encounter such web servers
		return "", "", errors.New("Multi-line header not supported")
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
			// Connection close, no content length specification
			// Use chunked encoding to pass content back to client
			if !h.KeepAlive && !h.Chunking && h.ContLen == -1 {
				raw.WriteString("Transfer-Encoding: chunked\r\n")
			}

			raw.WriteString(addHeader)
			raw.WriteString("\r\n")
			return
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
		return nil, errors.New(fmt.Sprintf("malformed HTTP request: %s", s))
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
		err = drainHeader(reader)
		return
	}

	r.genRequestLine()

	// Read request header
	if err = r.parseHeader(reader, &r.raw, ""); err != nil {
		errl.Printf("Parsing request header: %v\n", err)
		return nil, err
	}
	return
}

func (r *Request) genRequestLine() {
	r.raw.WriteString(r.Method + " " + r.URL.Path)
	r.raw.WriteString(" HTTP/1.1\r\nConnection: Keep-Alive\r\n")
}

var crlfBuf = make([]byte, 2)

func readCheckCRLF(reader *bufio.Reader) error {
	if _, err := io.ReadFull(reader, crlfBuf); err != nil {
		return err
	}
	if crlfBuf[0] != '\r' || crlfBuf[1] != '\n' {
		return errors.New("Not CRLF")
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

// Parse response status and headers.
func parseResponse(reader *bufio.Reader) (rp *Response, err error) {
	rp = new(Response)
	rp.ContLen = -1

	var s string
START:
	if s, err = ReadLine(reader); err != nil {
		if err != io.EOF {
			// err maybe timeout caused by explicity setting deadline
			debug.Printf("Reading Response status line: %v\n", err)
		}
		return nil, err
	}
	var f []string
	if f = strings.SplitN(s, " ", 3); len(f) < 2 {
		errl.Println("malformed HTTP response status line:", s)
		return nil, malformedHTTPResponseErr
	}
	// Handle 1xx response
	if f[1] == "100" {
		if err = readCheckCRLF(reader); err != nil {
			errl.Printf("Reading CRLF after 1xx response: %v\n", err)
			return nil, err
		}
		goto START
	}
	rp.Status = f[1]
	if len(f) == 3 {
		rp.Reason = f[2]
	}

	rp.raw.WriteString(s)
	rp.raw.WriteString("\r\n")

	if err = rp.parseHeader(reader, &rp.raw, "Connection: Keep-Alive\r\n"); err != nil {
		errl.Println("Reading response header:", err)
		return nil, err
	}

	return rp, nil
}
