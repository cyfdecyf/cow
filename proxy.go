package main

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strings"
)

// Lots of the code here are learnt from the http package

type Proxy struct {
	addr string // listen address
}

type conn struct {
	buf *bufio.ReadWriter
	rwc net.Conn
}

type Request struct {
	Method     string
	RequestURI string
	Proto      string

	keepAlive bool
	header    []string
	body      []byte
}

type ProxyError struct {
	msg string
}

func (pe *ProxyError) Error() string { return pe.msg }

func NewProxy(addr string) (proxy *Proxy) {
	proxy = &Proxy{addr: addr}
	return
}

func (py *Proxy) Serve() {
	ln, err := net.Listen("tcp", py.addr)
	if err != nil {
		info.Println("Server create failed:", err)
		os.Exit(1)
	}

	for {
		rwc, err := ln.Accept()
		if err != nil {
			info.Println("Client connection:", err)
			continue
		}

		c := newConn(rwc)
		go c.serve()
	}
}

func newConn(rwc net.Conn) (c *conn) {
	c = &conn{rwc: rwc}
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

func (c *conn) readRequest() (req *Request, err error) {
	req = new(Request)
	var s string
	if s, err = ReadLine(c.buf.Reader); err != nil {
		return nil, err
	}

	var f []string
	if f = strings.SplitN(s, " ", 3); len(f) < 3 {
		return nil, &ProxyError{"malformed HTTP request"}
	}
	req.Method, req.RequestURI, req.Proto = f[0], f[1], f[2]

	// Test if HTTP version is well formed
	var ok bool
	if _, _, ok = http.ParseHTTPVersion(req.Proto); !ok {
		return nil, &ProxyError{"malformed HTTP version"}
	}

	for {
		s, err = ReadLine(c.buf.Reader)
		// debug.Printf("len %d %s", len(s), s)
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		if s == "" {
			// read body and then break, do this only if method is post
			if req.Method == "POST" {
				if req.body, err = ioutil.ReadAll(c.buf); err != nil {
					return nil, err
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
			return
		}

		debug.Printf("%v", req)

		fmt.Fprintln(c.buf.Writer, "COW Proxy not finished\n")

		break
	}
	c.close()
}

func (c *conn) close() {
	if c.buf != nil {
		c.buf.Flush()
		c.buf = nil
	}
	if c.rwc != nil {
		c.rwc.Close()
		c.rwc = nil
	}
}

func (r *Request) String() string {
	return fmt.Sprintf("[Request] Method: %s RequestURI: %s header:\n\t%v\n",
		r.Method, r.RequestURI, strings.Join(r.header, "\n\t"))
}
