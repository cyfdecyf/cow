package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"os"
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

		if err = c.doRequest(r); err != nil {
			log.Println("Doing http request", err)
			// TODO what's possible error? how to handle?
		}

		break
	}
}

func (c *conn) doRequest(r *Request) (err error) {
	debug.Printf("Connecting to %s\n", r.URL.Host)
	srvconn, err := net.Dial("tcp", r.URL.Host)
	if err != nil {
		return newProxyError("Connecting to %s:", err)
	}
	// TODO revisit here when implementing keep-alive
	defer srvconn.Close()

	// Send request to the server
	debug.Printf("%v", r)
	if _, err := srvconn.Write(r.raw.Bytes()); err != nil {
		return err
	}
	// Send request body
	if r.Method == "POST" {
		if _, err = io.Copy(srvconn, c.buf.Reader); err != nil {
			return newProxyError("Sending request body to server", err)
		}
	}

	// Read server reply
	// parse status line
	srvReader := bufio.NewReader(srvconn)
	response, err := parseResponse(srvReader, r.Method)
	if err != nil {
		return err
	}
	c.buf.WriteString(response.raw.String())

	// Wrap inside if to avoid function argument evaluation. Would this work?
	if debug {
		debug.Printf("[Response] %s %v\n%v", r.Method, r.URL, response)
	}

	if response.HasBody {
		debug.Printf("Sending server response to client, content length %v\n",
			response.ContLen)
		// Send reply body to client
		var lr io.Reader
		// No content length specified
		if response.ContLen == -1 {
			lr = srvconn // io.Copy read 32k each time, no need to use bufio here
		} else {
			lr = io.LimitReader(srvconn, response.ContLen)
		}
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
