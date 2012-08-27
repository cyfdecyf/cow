package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"net/url"
	"strings"
)

type Request struct {
	Method string
	URL    *url.URL
	Proto  string

	rawHeader []string
	header    Header
	body      []byte
}

type Header map[string]string

type HttpError struct {
	msg string
}

func (he *HttpError) Error() string { return he.msg }

func newHttpError(msg string, err error) *HttpError {
	return &HttpError{fmt.Sprintln(msg, err)}
}

// If an http response may have message body
func responseMayHaveBody(method, status string) bool {
	// when we have tenary search tree, can optimize this a little
	return !(method == "HEAD" || status == "304" || status == "204" || strings.HasPrefix(status, "1"))
}

func (r *Request) parseHeader(reader bufio.Reader) {

}

func parseRequest(reader *bufio.Reader) (r *Request, err error) {
	r = new(Request)
	var s string

	// parse initial request line
	if s, err = ReadLine(reader); err != nil {
		return nil, err
	}

	var f []string
	if f = strings.SplitN(s, " ", 3); len(f) < 3 {
		return nil, &HttpError{"malformed HTTP request"}
	}
	var requestURI string
	r.Method, requestURI, r.Proto = f[0], f[1], f[2]

	// Parse URI into host and path
	if r.URL, err = url.ParseRequestURI(requestURI); err != nil {
		return nil, newProxyError("Parsing request URI", err)
	}
	if !hostHasPort(r.URL.Host) {
		r.URL.Host += ":80"
	}

	// Read request header and body
	for {
		if s, err = ReadLine(reader); err != nil {
			return nil, newProxyError("Reading client request", err)
		}

		if lower := strings.ToLower(s); strings.HasPrefix(lower, "proxy-connection") {
			_, val, err := parseHeader(lower)
			if err != nil {
				return nil, newProxyError("Parsing request header:", err)
			}
			if val == "keep-alive" {
				// This is proxy related, don't return
				debug.Printf("proxy-connection keep alive\n")
			}
			continue
		}

		// debug.Printf("len %d %s", len(s), s)
		if s == "" {
			// read body and then break, do this only if method is post
			if r.Method == "POST" {
				if r.body, err = ioutil.ReadAll(reader); err != nil {
					return nil, newProxyError("Reading request body", err)
				}
			}
			break
		}
		r.rawHeader = append(r.rawHeader, s)
	}

	return r, nil
}

func (r *Request) genRawRequest() []byte {
	// First calculate size of the header
	var n int = len("  HTTP/1.1\r\n") + 2 // plus the length of the final \r\n
	n += len(r.Method)
	n += len(r.URL.Path)
	for _, l := range r.rawHeader {
		n += len(l) + 2
	}
	n += len(r.body)

	// generate header
	b := make([]byte, n)
	bp := copy(b, r.Method)
	bp += copy(b[bp:], " ")
	bp += copy(b[bp:], r.URL.Path)
	bp += copy(b[bp:], " ")
	bp += copy(b[bp:], "HTTP/1.1\r\n")
	for _, h := range r.rawHeader {
		bp += copy(b[bp:], h)
		bp += copy(b[bp:], "\r\n")
	}
	bp += copy(b[bp:], "\r\n")
	// TODO check this when testing POST
	copy(b[bp:], r.body)

	return b
}
