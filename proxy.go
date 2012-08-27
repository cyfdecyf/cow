package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Lots of the code here are learnt from the http package

type Proxy struct {
	addr string // listen address
}

type conn struct {
	keepAlive bool
	buf       *bufio.ReadWriter
	cliconn   net.Conn // connection to the proxy client
	// TODO is it possible that one proxy connection is used to server all the client request?
	// Make things simple at this moment and disable http request keep-alive
	// srvconn net.Conn // connection to the server
}

type ProxyError struct {
	msg string
}

func (pe *ProxyError) Error() string { return pe.msg }

func newProxyError(msg string, err error) *ProxyError {
	return &ProxyError{fmt.Sprintln(msg, err)}
}

func NewProxy(addr string) (proxy *Proxy) {
	proxy = &Proxy{addr: addr}
	return
}

func (py *Proxy) Serve() {
	ln, err := net.Listen("tcp", py.addr)
	if err != nil {
		log.Println("Server create failed:", err)
		os.Exit(1)
	}
	info.Println("COW proxy listening", py.addr)

	for {
		rwc, err := ln.Accept()
		if err != nil {
			log.Println("Client connection:", err)
			continue
		}
		info.Println("New Client:", rwc.RemoteAddr())

		c := newConn(rwc)
		go c.serve()
	}
}

func newConn(rwc net.Conn) (c *conn) {
	c = &conn{cliconn: rwc}
	// http pkg uses io.LimitReader with no limit to create a reader, why?
	br := bufio.NewReader(rwc)
	bw := bufio.NewWriter(rwc)
	c.buf = bufio.NewReadWriter(br, bw)
	return
}

var hostPortRe *regexp.Regexp = regexp.MustCompile("^[^:]+:\\d+$")

func hostHasPort(s string) bool {
	// Common case should has no port, so check the last char
	if !IsDigit(s[len(s)-1]) {
		return false
	}
	return hostPortRe.MatchString(s)
}

// Note header may span more then 1 line, current implementation does not
// support this
func parseHeader(s string) (key, val string, err error) {
	var f []string
	if f = strings.SplitN(s, ":", 2); len(f) < 2 {
		return "", "", &ProxyError{"header not supported: " + s}
	}
	key, val = strings.TrimSpace(f[0]), strings.TrimSpace(f[1])
	return
}

func (c *conn) serve() {
	defer c.close()
	var r *Request
	var err error
	for {
		if r, err = parseRequest(c.buf.Reader); err != nil {
			log.Println("Reading client request", err)
			return
		}
		// debug.Printf("%v", req)

		if err = c.doReq(r); err != nil {
			log.Println("Doing http request", err)
			// TODO what's possible error? how to handle?
		}

		break
	}
}

// noLimit is an effective infinite upper bound for io.LimitedReader
const noLimit int64 = (1 << 63) - 1

func (c *conn) doReq(r *Request) (err error) {
	debug.Printf("Connecting to %s\n", r.URL.Host)
	srvconn, err := net.Dial("tcp", r.URL.Host)
	if err != nil {
		return newProxyError("Connecting to %s:", err)
	}
	// TODO revisit here when implementing keep-alive
	defer srvconn.Close()

	// Send request to the server
	rawReq := r.genRawRequest()
	debug.Printf("%v\n", r)
	if _, err := srvconn.Write(rawReq); err != nil {
		return err
	}

	// Read server reply

	// parse status line
	srvReader := bufio.NewReader(srvconn)
	var s string
	if s, err = ReadLine(srvReader); err != nil {
		return newProxyError("Reading Response status line:", err)
	}
	var f []string
	if f = strings.SplitN(s, " ", 3); len(f) < 3 {
		return &ProxyError{fmt.Sprintln("malformed HTTP response status line:", s)}
	}
	status := f[1]
	reason := f[2]
	// Send back to client
	c.buf.WriteString(s)
	c.buf.WriteString("\r\n")

	hasBody := responseMayHaveBody(r.Method, status)
	contLen := noLimit
	lengthParsed := false

	var rawResponse bytes.Buffer // For debugging

	for {
		// Parse header
		if s, err = ReadLine(srvReader); err != nil {
			return newProxyError("Reading Response header:", err)
		}
		c.buf.WriteString(s)
		c.buf.WriteString("\r\n")
		if s == "" {
			break
		}
		if debug {
			rawResponse.WriteString("\n\t" + s)
		}

		// Only parse header for Content-Length and Transfer-Encoding
		if hasBody && !lengthParsed {
			lower := strings.ToLower(s)
			if strings.HasPrefix(lower, "content-length") {
				_, val, err := parseHeader(lower)
				if err != nil {
					return newProxyError("Parsing response header:", err)
				}
				if contLen, err = strconv.ParseInt(val, 10, 64); err != nil {
					return newProxyError("Response content-length:", err)
				}
				if contLen == 0 {
					hasBody = false
				}
				lengthParsed = true
			}
		}
	}
	if debug {
		// Wrap inside if to avoid evaluating function arguments
		debug.Printf("[Response] %s %v %v %v%s", r.Method, r.URL, status, reason,
			rawResponse.String())
	}
	if hasBody {
		debug.Printf("Sending server response to client, content length %v\n",
			contLen)
		// Send reply body to client
		lr := io.LimitReader(srvconn, contLen)
		if _, err := io.Copy(c.buf.Writer, lr); err != nil && err != io.EOF {
			return err
		}
	}
	c.buf.Flush()
	return nil
}

func (c *conn) close() {
	if c.buf != nil {
		c.buf.Flush()
		c.buf = nil
	}
	if c.cliconn != nil {
		debug.Printf("client connection closed\n")
		c.cliconn.Close()
		c.cliconn = nil
	}
}

func (r *Request) String() (s string) {
	s = fmt.Sprintf("[Request] %s %s %s", r.Method,
		r.URL.Host, r.URL.Path)
	if false {
		s += fmt.Sprintf(" Header:\n\t%v\n", strings.Join(r.rawHeader, "\n\t"))
	}
	return
}
