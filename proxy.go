package main

import (
	"bufio"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// Lots of the code here are learnt from the http package

type Proxy struct {
	addr string // listen address
}

type clientConn struct {
	keepAlive bool
	buf       *bufio.ReadWriter
	netconn   net.Conn            // connection to the proxy client
	srvconn   map[string]net.Conn // connection to the server, host:port as key
}

func NewProxy(addr string) (proxy *Proxy) {
	proxy = &Proxy{addr: addr}
	return
}

func (py *Proxy) Serve() {
	ln, err := net.Listen("tcp", py.addr)
	if err != nil {
		log.Println("Server creation failed:", err)
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

// Explicitly specify buffer size to avoid unnecessary copy using
// bufio.Reader's Read
const bufSize = 4096

func newClientConn(rwc net.Conn) (c *clientConn) {
	c = &clientConn{netconn: rwc, srvconn: map[string]net.Conn{}}
	// http pkg uses io.LimitReader with no limit to create a reader, why?
	br := bufio.NewReaderSize(rwc, bufSize)
	bw := bufio.NewWriter(rwc)
	c.buf = bufio.NewReadWriter(br, bw)
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

func (c *clientConn) serve() {
	defer c.close()
	var r *Request
	var err error

	// Refer to implementation.md for the design choices on parsing the request
	// and response.
	for {
		if r, err = parseRequest(c.buf.Reader); err != nil {
			// io.EOF means the client connection is closed
			if err != io.EOF {
				errl.Printf("Reading client request: %v\n", err)
			}
			return
		}
		if debug {
			debug.Printf("%v", r)
		} else {
			info.Println(r)
		}

		if r.isConnect {
			if err = c.doConnect(r); err != nil {
				errl.Printf("Doing connect: %v\n", err)
			}
			return
		}

		if err = c.doRequest(r); err != nil {
			errl.Printf("Doing request: %s %s %v\n", r.Method, r.URL, err)
			// TODO Should server connection error close client connection?
			// Possible error here:
			// 1. the proxy can't find the host
			// 2. broken pipe to the client
			break
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

func copyData(dst net.Conn, src *bufio.Reader, dbgmsg string) (err error) {
	buf := make([]byte, bufSize)
	var n int
	for {
		n, err = src.Read(buf)
		if err != nil {
			if err != io.EOF {
				errl.Printf("%s reading data: %v\n", dbgmsg, err)
			}
			return
		}
		_, err = dst.Write(buf[0:n])
		if err != nil {
			errl.Printf("%s writing data: %v\n", dbgmsg, err)
			return
		}
	}
	return
}

func (c *clientConn) doConnect(r *Request) (err error) {
	host := r.URL.Host
	if !hostHasPort(host) {
		host += ":80"
	}
	srvconn, err := net.Dial("tcp", host)
	if err != nil {
		// TODO how to respond error connection?
		errl.Printf("doConnect Connecting to: %s %v\n", host, err)
		return err
	}
	// defer must come after error checking because srvconn maybe null in case of error
	defer srvconn.Close()
	debug.Printf("Connected to %s Sending 200 Connection established to client\n", host)
	// TODO Send response to client
	c.buf.WriteString("HTTP/1.0 200 Connection established\r\nProxy-agent: cow-proxy/0.1\r\n\r\n")
	c.buf.Writer.Flush()

	errchan := make(chan error)
	// Must wait this goroutine finish before returning from this function.
	// Otherwise, the server/client may have been closed and thus cause nil
	// pointer deference
	go func() {
		err := copyData(c.netconn, bufio.NewReaderSize(srvconn, bufSize), "server->client")
		errchan <- err
	}()

	err = copyData(srvconn, c.buf.Reader, "client->server")

	// wait goroutine finish
	err2 := <-errchan
	if err2 != io.EOF {
		return err2
	}
	if err == io.EOF {
		err = nil
	}
	return
}

func (c *clientConn) getConnection(host string) (srvconn net.Conn, err error) {
	// Must declare ok outside of if statement. Using short variable declarations
	// will create local variable to the if statement.
	var ok bool
	if srvconn, ok = c.srvconn[host]; !ok {
		srvconn, err = net.Dial("tcp", host)
		if err != nil {
			// TODO Find a way report no host error to client. Send back web page?
			// It's weird here, sometimes nslookup can finding host, but net.Dial
			// can't
			errl.Printf("Connecting to: %s %v\n", host, err)
			return nil, err
		}
		debug.Printf("Connected to %s\n", host)
		c.srvconn[host] = srvconn

		// start goroutine to send response to client
		go func() {
			// TODO this is a place to detect blocked sites
			err := copyData(c.netconn, bufio.NewReader(srvconn), "doRequest server->client")
			if err != nil && err != io.EOF {
				errl.Printf("getConnection goroutine sending response to client: %v\n", err)
			}
			c.removeConnection(host)
		}()
	}
	return
}

func (c *clientConn) removeConnection(host string) (err error) {
	err = c.srvconn[host].Close()
	delete(c.srvconn, host)
	return
}

func (c *clientConn) doRequest(r *Request) (err error) {
	// TODO should reuse connection to implement keep-alive
	host := r.URL.Host
	if !hostHasPort(host) {
		host += ":80"
	}

RETRY:
	srvconn, err := c.getConnection(host)
	if err != nil {
		return
	}
	// Send request to the server
	if _, err = srvconn.Write(r.raw.Bytes()); err != nil {
		// The srv connection maybe already closed.
		// Need to delete the connection and reconnect in that case.
		errl.Printf("writing to connection error: %v\n", err)
		if err == syscall.EPIPE {
			c.removeConnection(host)
			goto RETRY
		} else {
			return err
		}
	}

	// Send request body
	if r.Method == "POST" {
		if err = sendBody(bufio.NewWriter(srvconn), c.buf.Reader, r.Chunking, r.ContLen); err != nil {
			errl.Printf("Sending request body: %v\n", err)
			return err
		}
	}

	// Read server reply is handled in the goroutine started when creating the
	// server connection

	// The original response parsing code.
	/*
		rp, err := parseResponse(srvReader, r.Method)
		if err != nil {
			return
		}
		c.buf.WriteString(rp.raw.String())
		// Flush response header to the client ASAP
		if err = c.buf.Flush(); err != nil {
			errl.Printf("Flushing response header to client: %v\n", err)
			return err
		}

		// Wrap inside if to avoid function argument evaluation. Would this work?
		if debug {
			debug.Printf("[Response] %s %v\n%v", r.Method, r.URL, rp)
		}

		if rp.HasBody {
			if err = sendBody(c.buf.Writer, srvReader, rp.Chunking, rp.ContLen); err != nil {
				return
			}
		}
		debug.Printf("Finished request %s %s\n", r.Method, r.URL)
	*/
	return
}

// Send response body if header specifies content length
func sendBodyWithContLen(w *bufio.Writer, r *bufio.Reader, contLen int64) (err error) {
	debug.Printf("Sending body with content length %d\n", contLen)
	if contLen == 0 {
		return
	}
	// CopyN will copy n bytes unless there's error of EOF. For EOF, it means
	// the connection is closed, return will propagate till serv function and
	// close client connection.
	if _, err = io.CopyN(w, r, contLen); err != nil {
		errl.Printf("Sending response body to client %v\n", err)
		return err
	}
	return
}

// Send response body if header specifies chunked encoding
func sendBodyChunked(w *bufio.Writer, r *bufio.Reader) (err error) {
	debug.Printf("Sending chunked body\n")

	for {
		var s string
		// Read chunk size line, ignore chunk extension if any
		if s, err = ReadLine(r); err != nil {
			errl.Printf("Reading chunk size: %v\n", err)
			return err
		}
		// debug.Printf("chunk size line %s", s)
		f := strings.SplitN(s, ";", 2)
		var size int64
		if size, err = strconv.ParseInt(f[0], 16, 64); err != nil {
			errl.Printf("Chunk size not valid: %v\n", err)
			return err
		}
		w.WriteString(s)
		w.WriteString("\r\n")

		if size == 0 { // end of chunked data, ignore any trailers
			goto END
		}

		// Read chunk data and send to client
		if _, err = io.CopyN(w, r, size); err != nil {
			errl.Printf("Reading chunked data from server: %v\n", err)
			return err
		}
	END:
		// XXX maybe this kind of error handling should be passed to the
		// client? But if the proxy doesn't know when to stop reading from the
		// server, the only way to avoid blocked reading is to set read time
		// out on server connection. Would that be easier?
		if err = readCheckCRLF(r); err != nil {
			errl.Printf("Reading chunked data CRLF: %v\n", err)
			return err
		}
		w.WriteString("\r\n")
	}
	return
}

// Send message body
func sendBody(w *bufio.Writer, r *bufio.Reader, chunk bool, contLen int64) (err error) {
	if chunk {
		err = sendBodyChunked(w, r)
	} else if contLen >= 0 {
		err = sendBodyWithContLen(w, r, contLen)
	} else {
		// Maybe because this is an HTTP/1.0 server. Just read and wait connection close
		info.Printf("Can't determine body length and not chunked encoding\n")
		if _, err = io.Copy(w, r); err != nil {
			return
		}
	}

	if err = w.Flush(); err != nil {
		errl.Printf("Flushing body to client %v\n", err)
		return err
	}
	return
}
