package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"os"
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
	rp, err := parseResponse(srvReader, r.Method)
	if err != nil {
		return
	}
	c.buf.WriteString(rp.raw.String())

	// Wrap inside if to avoid function argument evaluation. Would this work?
	if debug {
		debug.Printf("[Response] %s %v\n%v", r.Method, r.URL, rp)
	}

	if err = c.sendResponseBody(srvReader, rp); err != nil {
		return
	}
	c.buf.Flush()
	return nil
}

// Send response body if header specifies content length
func (c *conn) sendResponseBodyWithContLen(srvReader *bufio.Reader, contLen int64) (err error) {
	debug.Printf("Sending response to client, content length %d\n", contLen)
	lr := io.LimitReader(srvReader, contLen)
	_, err = io.Copy(c.buf.Writer, lr)
	return
}

// Send response body if header specifies chunked encoding
func (c *conn) sendResponseBodyChunked(srvReader *bufio.Reader) (err error) {
	debug.Printf("Sending chunked response to client\n")

	for {
		var s string
		// Read chunk size line, ignore chunk extension if any
		if s, err = ReadLine(srvReader); err != nil {
			return newProxyError("Reading chunk size", err)
		}
		// debug.Printf("chunk size line %s", s)
		f := strings.SplitN(s, ";", 2)
		var size int64
		if size, err = strconv.ParseInt(f[0], 16, 64); err != nil {
			return newProxyError("Chunk size not valid", err)
		}
		c.buf.WriteString(s)
		c.buf.WriteString("\r\n")

		if size == 0 { // end of chunked data, ignore any trailers
			break
		}

		// Read chunk data and send to client
		b := make([]byte, size+2) // include the ending \r\n
		var readcnt int
		if readcnt, err = io.ReadFull(srvReader, b); err != nil {
			return newProxyError("Reading chunked data from server", err)
		}
		if int64(readcnt) != size+2 {
			debug.Printf("read cnt %d not equal to chunk size %d\n", readcnt, size)
		}
		// debug.Printf("chunk data\n%s", string(b))
		// XXX maybe this kind of error handling should be passed to the
		// client? But if the proxy doesn't know when to stop reading from the
		// server, the only way to avoid blocked reading is to set read time
		// out on server connection. Would that be easier?
		if b[len(b)-2] != '\r' || b[len(b)-1] != '\n' {
			return newProxyError("Malformed chunked data: "+string(b), err)
		}
		c.buf.WriteString(string(b))
	}
	return nil
}

// Send response body to client.
func (c *conn) sendResponseBody(srvReader *bufio.Reader, rp *Response) (err error) {
	if !rp.HasBody {
		return
	}

	if rp.Chunking {
		err = c.sendResponseBodyChunked(srvReader)
	} else if rp.ContLen != 0 {
		err = c.sendResponseBodyWithContLen(srvReader, rp.ContLen)
	} else {
		return &ProxyError{"Can't determine response length and not chunked encoding"}
	}

	c.buf.Flush()
	return
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
