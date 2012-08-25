package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/url"
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

type Request struct {
	Method string
	URL    *url.URL
	Proto  string

	keepAlive bool
	header    []string
	body      []byte
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

// Almost same with net/textproto/reader.go ReadLine
func ReadLine(r *bufio.Reader) (string, error) {
	var line []byte
	for {
		l, more, err := r.ReadLine()
		if err != nil {
			return "", err
		}

		if line == nil && !more {
			return string(l), nil
		}
		line = append(line, l...)
		if !more {
			break
		}
	}
	return string(line), nil
}

func isDigit(b byte) bool {
	return '0' <= b && b <= '9'
}

var hostPortRe *regexp.Regexp = regexp.MustCompile("^[^:]*:\\d+$")

func hostHasPort(s string) bool {
	// Common case should has no port, so check the last char
	if !isDigit(s[len(s)-1]) {
		return false
	}
	return hostPortRe.MatchString(s)
}

func parseHeader(s string) (key, val string, err error) {
	var f []string
	if f = strings.SplitN(s, ":", 2); len(f) < 2 {
		return "", "", nil
	}
	key, val = strings.ToLower(strings.TrimSpace(f[0])),
		strings.ToLower(strings.TrimSpace(f[1]))
	return
}

func (c *conn) readRequest() (req *Request, err error) {
	req = new(Request)
	var s string

	// parse initial request line
	if s, err = ReadLine(c.buf.Reader); err != nil {
		return nil, err
	}

	var f []string
	if f = strings.SplitN(s, " ", 3); len(f) < 3 {
		return nil, &ProxyError{"malformed HTTP request"}
	}
	var requestURI string
	req.Method, requestURI, req.Proto = f[0], f[1], f[2]

	// Parse URI into host and path
	if req.URL, err = url.ParseRequestURI(requestURI); err != nil {
		return nil, newProxyError("Parsing request URI", err)
	}
	if !hostHasPort(req.URL.Host) {
		req.URL.Host += ":80"
	}

	// Read request header and body
	for {
		if s, err = ReadLine(c.buf.Reader); err != nil {
			return nil, newProxyError("Reading client request", err)
		}
		key, val, err := parseHeader(s)
		if err != nil {
			return nil, newProxyError("Parsing request header:", err)
		}
		if key == "proxy-connection" && val == "keep-alive" {
			// This is proxy related, don't return
			debug.Printf("proxy-connection keep alive\n")
			c.keepAlive = true
			continue
		}
		// debug.Printf("len %d %s", len(s), s)
		if s == "" {
			// read body and then break, do this only if method is post
			if req.Method == "POST" {
				if req.body, err = ioutil.ReadAll(c.buf); err != nil {
					return nil, newProxyError("Reading request body", err)
				}
			}
			break
		}
		req.header = append(req.header, s)
	}

	return req, nil
}

func (c *conn) serve() {
	defer c.close()
	var req *Request
	var err error
	for {
		if req, err = c.readRequest(); err != nil {
			log.Println("Reading client request", err)
			return
		}
		debug.Printf("%v", req)

		if err = c.doReq(req); err != nil {
			log.Println("Doing http request", err)
			// TODO what's possible error? how to handle?
		}

		break
	}
}

// noLimit is an effective infinite upper bound for io.LimitedReader
const noLimit int64 = (1 << 63) - 1

func (c *conn) doReq(r *Request) (err error) {
	var buf bytes.Buffer

	initial := []string{r.Method, r.URL.Path, "HTTP/1.1\r\n"}
	buf.WriteString(strings.Join(initial, " "))
	buf.WriteString(strings.Join(r.header, "\r\n"))
	buf.WriteString("\r\n\r\n")

	debug.Printf("Connecting to %s\n", r.URL.Host)
	srvconn, err := net.Dial("tcp", r.URL.Host)
	if err != nil {
		return newProxyError("Connecting to %s:", err)
	}
	defer srvconn.Close()

	// Send request to the server
	debug.Printf("Sending HTTP request\n%v\n", buf.String())
	if _, err := srvconn.Write(buf.Bytes()); err != nil {
		return err
	}

	// Read server reply
	var s string
	var f []string
	hasBody := true

	// parse status line
	srvReader := bufio.NewReader(srvconn)
	if s, err = ReadLine(srvReader); err != nil {
		return newProxyError("Reading Response status line:", err)
	}
	if f = strings.SplitN(s, " ", 3); len(f) < 3 {
		return &ProxyError{fmt.Sprintln("malformed HTTP response status line:", s)}
	}
	// f[1] contains status code
	debug.Printf("%v response status %v %v", r.URL, f[1], f[2])
	if f[1] == "304" {
		hasBody = false
	}
	// Send back to client
	c.buf.WriteString(s)
	c.buf.WriteString("\r\n")

	contLen := noLimit
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
		debug.Printf("[Response] %v: %v\n", r.URL, s)

		var key, val string
		if key, val, err = parseHeader(s); err != nil {
			return newProxyError("Parsing response header:", err)
		}
		if key == "content-length" {
			if contLen, err = strconv.ParseInt(val, 10, 64); err != nil {
				return newProxyError("Response content-length:", err)
			}
			if contLen == 0 {
				contLen = noLimit
			}
		}
	}
	if hasBody {
		debug.Printf("Sending server response to client\n")
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
		c.cliconn.Close()
		c.cliconn = nil
	}
}

func (r *Request) String() string {
	return fmt.Sprintf("[Request] Method: %s Host: %s Path: %s, Header:\n\t%v\n",
		r.Method, r.URL.Host, r.URL.Path, strings.Join(r.header, "\n\t"))
}
