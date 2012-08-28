package main

import (
	"bufio"
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

type Request struct {
	Method string
	URL    *URL
	Proto  string
	Header Header

	raw bytes.Buffer
}

func (r *Request) String() (s string) {
	s = fmt.Sprintf("[Request] %s Host: %s Path: %s\n", r.Method,
		r.URL.Host, r.URL.Path)
	if true {
		s += fmt.Sprintf("%v", r.raw.String())
	}
	return
}

type Response struct {
	Status  string
	Reason  string
	HasBody bool

	ContLen  int64
	Chunking bool

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

type Header map[string]string

// TODO Rename to protocol error just as the http pkg
type HttpError struct {
	msg string
}

// headers of interest to a proxy
// Define them as constant and use editor's completion to avoid typos
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
	if !hostHasPort(host) {
		host += ":80"
	}

	return &URL{Host: host, Path: path}, nil
}

// Note header may span more then 1 line, current implementation does not
// support this
func splitHeader(s string) []string {
	f := strings.SplitN(s, ":", 2)
	for i, _ := range f {
		f[i] = strings.TrimSpace(f[i])
	}
	return f
}

// Only add headers that are of interest for a proxy into request's header map
func (r *Request) parseHeader(reader *bufio.Reader) (err error) {
	// Read request header and body
	var s string
	for {
		if s, err = ReadLine(reader); err != nil {
			return newHttpError("Reading client request", err)
		}
		if lower := strings.ToLower(s); strings.HasPrefix(lower, headerProxyConnection) {
			f := splitHeader(s)
			if len(f) == 2 {
				r.Header[headerProxyConnection] = f[1]
			} else {
				// TODO For headers like proxy-connection, I guess not client would
				// make it spread multiple line. But better to support this.
				info.Println("Multi-line header not supported")
			}
			continue
		}
		r.raw.WriteString(s)
		r.raw.WriteString("\r\n")
		// debug.Printf("len %d %s", len(s), s)
		if s == "" {
			break
		}
	}
	return nil
}

// Parse the initial line and header, does not touch body
func parseRequest(reader *bufio.Reader) (r *Request, err error) {
	r = new(Request)
	r.Header = make(Header)
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
		return nil, err
	}
	r.genInitialLine()

	// Read request header
	r.parseHeader(reader)
	return r, nil
}

func (r *Request) genInitialLine() {
	r.raw.WriteString(r.Method)
	r.raw.WriteString(" ")
	r.raw.WriteString(r.URL.Path)
	r.raw.WriteString(" ")
	r.raw.WriteString("HTTP/1.1\r\n")
}

// Only put headers of interest for an proxy into header map
func (rp *Response) parseHeader(reader *bufio.Reader) (err error) {
	var s string
	for {
		// Parse header
		if s, err = ReadLine(reader); err != nil {
			return newHttpError("Reading Response header:", err)
		}
		if s == "" {
			// TODO remove this when implementing persistent connection
			rp.raw.WriteString("Proxy-Connection: close\r\n")
			rp.raw.WriteString("\r\n")
			break
		}

		f := splitHeader(s)
		fieldname := strings.ToLower(f[0])
		// Don't pass connection header to client
		if fieldname != headerConnection {
			rp.raw.WriteString(s)
			rp.raw.WriteString("\r\n")
		} else {
			continue
		}

		// Only parse header for Content-Length and Transfer-Encoding
		if rp.HasBody {
			if fieldname == headerContentLength {
				if len(f) != 2 {
					return &HttpError{"Multi-line header not supported: " + s}
				}
				if rp.ContLen, err = strconv.ParseInt(f[1], 10, 64); err != nil {
					return newProxyError("Response content-length:", err)
				}
				if rp.ContLen == 0 {
					rp.HasBody = false
				}
			} else if fieldname == headerTransferEncoding {
				fieldval := strings.ToLower(f[1])
				if fieldval == "chunked" {
					debug.Printf("chunked transfer detected\n")
					rp.Chunking = true
				} else {
					debug.Printf("transfer-encoding: %s not supported", fieldval)
				}
			}
		}
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

	var s string
	if s, err = ReadLine(reader); err != nil {
		return nil, newHttpError("Reading Response status line:", err)
	}
	var f []string
	if f = strings.SplitN(s, " ", 3); len(f) < 3 {
		return nil, &HttpError{fmt.Sprintln("malformed HTTP response status line:", s)}
	}
	rp.Status = f[1]
	rp.Reason = f[2]
	rp.HasBody = responseMayHaveBody(method, rp.Status)

	rp.raw.WriteString(s)
	rp.raw.WriteString("\r\n")

	if err = rp.parseHeader(reader); err != nil {
		return nil, err
	}

	return rp, nil
}
