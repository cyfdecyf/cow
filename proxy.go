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

type clientConn struct {
	keepAlive bool
	buf       *bufio.ReadWriter
	netconn   net.Conn // connection to the proxy client
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
		conn, err := ln.Accept()
		if err != nil {
			log.Println("Client connection:", err)
			continue
		}
		info.Println("New Client:", conn.RemoteAddr())

		c := newClientConn(conn)
		go c.serve()
	}
}

func newClientConn(rwc net.Conn) (c *clientConn) {
	c = &clientConn{netconn: rwc}
	// http pkg uses io.LimitReader with no limit to create a reader, why?
	br := bufio.NewReader(rwc)
	bw := bufio.NewWriter(rwc)
	c.buf = bufio.NewReadWriter(br, bw)
	return
}

func (c *clientConn) serve() {
	defer c.close()
	var r *Request
	var err error
	for {
		if r, err = parseRequest(c.buf.Reader); err != nil {
			// io.EOF means the client connection is closed
			if err != io.EOF {
				log.Println("Reading client request", err)
			}
			return
		}
		if debug {
			debug.Printf("%v", r)
		} else {
			info.Println(r)
		}

		// TODO Need to do the request in a goroutine to support pipelining?
		// If so, how to maintain the order of finishing request?
		// Consider pipelining later as this is just performance improvement.
		if err = c.doRequest(r); err != nil {
			// Server connection error should not close client connection
			// because this connection is between the proxy and the web
			// server, e.g. EOF. If this kind of error lead to client
			// connection close, the proxy will detect this later.
			log.Println("Doing http request", err)
		}

		// How to detect closed client connection?
		// Reading client connection will encounter EOF and detect that the
		// connection has been closed.

		// Firefox will create 6 persistent connections to the proxy server.
		// If opening many connections is not a problem, then nothing need
		// to be done.
		// Otherwise, set a read time out and close connection upon timeout.
		// This should not cause problem as
		// 1. I didn't see any independent message sent by firefox in order to
		//    close a persistent connection
		// 2. Sending Connection: Keep-Alive but actually closing the
		//    connection cause no problem for firefox. (The client should be
		//    able to detect closed connection and open a new one.)
		if !r.KeepAlive {
			break
		}
	}
}

func (c *clientConn) doRequest(r *Request) (err error) {
	host := r.URL.Host
	if !hostHasPort(host) {
		host += ":80"
	}
	srvconn, err := net.Dial("tcp", host)
	if err != nil {
		return newProxyError("Connecting to %s:", err)
	}
	debug.Printf("Connected to %s\n", r.URL.Host)
	// TODO revisit here when implementing keep-alive
	defer srvconn.Close()

	// Send request to the server
	if _, err := srvconn.Write(r.raw.Bytes()); err != nil {
		return err
	}
	// Send request body
	if r.Method == "POST" {
		// TODO we may need to parse content-length
		if _, err = io.Copy(srvconn, c.buf.Reader); err != nil {
			return newProxyError("Sending request body to server", err)
		}
	}

	// Read server reply
	// parse status line
	srvReader := bufio.NewReader(srvconn)
	rp, err := parseResponse(srvReader, r.Method)
	if err != nil {
		return err
	}
	c.buf.WriteString(rp.raw.String())
	// Flush response header to the client earlier
	if err = c.buf.Flush(); err != nil {
		return newProxyError("Flushing response header to client", err)
	}

	// Wrap inside if to avoid function argument evaluation. Would this work?
	if debug {
		debug.Printf("[Response] %s %v\n%v", r.Method, r.URL, rp)
	}

	if err = c.sendResponseBody(srvReader, rp); err != nil {
		return err
	}
	debug.Printf("Finished request %s %s\n", r.Method, r.URL)
	return nil
}

// Send response body if header specifies content length
func (c *clientConn) sendResponseBodyWithContLen(srvReader *bufio.Reader, contLen int64) (err error) {
	debug.Printf("Sending response body to client, content length %d\n", contLen)
	// CopyN will copy n bytes unless there's error of EOF. For EOF, it means
	// the connection is closed, return will propagate till serv function and
	// close client connection.
	if _, err = io.CopyN(c.buf.Writer, srvReader, contLen); err != nil {
		return newProxyError("Sending response body to client", err)
	}
	return nil
}

// Send response body if header specifies chunked encoding
func (c *clientConn) sendResponseBodyChunked(srvReader *bufio.Reader) (err error) {
	debug.Printf("Sending chunked response to client\n")

	b := make([]byte, 2) // buffer for chunked transfer
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
		if _, err = io.CopyN(c.buf.Writer, srvReader, size); err != nil {
			return newProxyError("Reading chunked data from server", err)
		}
		// XXX maybe this kind of error handling should be passed to the
		// client? But if the proxy doesn't know when to stop reading from the
		// server, the only way to avoid blocked reading is to set read time
		// out on server connection. Would that be easier?
		if _, err = io.ReadFull(srvReader, b); err != nil {
			return newProxyError("Reading chunked data ending CRLF from server", err)
		}
		if b[0] != '\r' || b[1] != '\n' {
			return newProxyError("Chunked data not ending with CRLF", err)
		}
		c.buf.WriteString("\r\n")
	}
	return nil
}

// Send response body to client.
func (c *clientConn) sendResponseBody(srvReader *bufio.Reader, rp *Response) (err error) {
	if !rp.HasBody {
		return
	}

	if rp.Chunking {
		err = c.sendResponseBodyChunked(srvReader)
	} else if rp.ContLen != 0 {
		err = c.sendResponseBodyWithContLen(srvReader, rp.ContLen)
	} else {
		// Maybe because this is an HTTP/1.0 server. Just read and wait connection close
		debug.Printf("Can't determine response length and not chunked encoding\n")
		if _, err = io.Copy(c.buf.Writer, srvReader); err != nil {
			return err
		}
	}

	if err = c.buf.Flush(); err != nil {
		return newProxyError("Flushing response body to client", err)
	}
	return
}

func (c *clientConn) close() {
	if c.buf != nil {
		c.buf.Flush()
		c.buf = nil
	}
	if c.netconn != nil {
		info.Printf("Client %v connection closed\n", c.netconn.RemoteAddr())
		c.netconn.Close()
		c.netconn = nil
	}
}
