package main

import (
	"bufio"
	"io"
	"log"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// Lots of the code here are learnt from the http package

type Proxy struct {
	addr string // listen address
}

type Handler interface {
	ServeRequest(*Request, *clientConn) error
	Close() error
	NotifyStop()
}

// Number of the simultaneous requests in the pipeline
const requestNum = 5

type directHandler struct {
	net.Conn
	stop    chan bool     // Used to notify the handler to stop execution
	request chan *Request // Pass HTTP method from request reader to response reader
}

type clientConn struct {
	buf        *bufio.ReadWriter
	conn       net.Conn           // connection to the proxy client
	handler    map[string]Handler // request handler, host:port as key
	handlerGrp sync.WaitGroup     // Wait all handler to finish before close
}

type proxyError string

func (e proxyError) Error() string {
	return string(e)
}

var (
	errPIPE = proxyError("Error: broken pipe")
)

func NewProxy(addr string) *Proxy {
	return &Proxy{addr: addr}
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
	c = &clientConn{conn: rwc, handler: map[string]Handler{}}
	// http pkg uses io.LimitReader with no limit to create a reader, why?
	br := bufio.NewReaderSize(rwc, bufSize)
	bw := bufio.NewWriter(rwc)
	c.buf = bufio.NewReadWriter(br, bw)
	return
}

func (c *clientConn) close() {
	for _, h := range c.handler {
		h.NotifyStop()
	}
	c.handlerGrp.Wait()
	if c.buf != nil {
		c.buf.Flush()
		c.buf = nil
	}
	if c.conn != nil {
		info.Printf("Client %v connection closed\n", c.conn.RemoteAddr())
		c.conn.Close()
		c.conn = nil
	}
	runtime.GC()
}

func (c *clientConn) serve() {
	defer c.close()
	var r *Request
	var err error
	var handler Handler

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
			debug.Printf("%v: %v\n", c.conn.RemoteAddr(), r)
		}

	RETRY:
		if handler, err = c.getHandler(r); err == nil {
			err = handler.ServeRequest(r, c)
		}

		if err != nil {
			if err == errPIPE {
				c.removeHandler(r)
				debug.Printf("Retrying request %v\n", r)
				goto RETRY
			}
			// TODO not all error should end the client connection
			// Possible error here:
			// 1. the proxy can't find the host
			// 2. broken pipe to the client
			return
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
			if err == io.EOF {
				return
			}
			if ne, ok := err.(*net.OpError); ok {
				if ne.Err == syscall.ECONNRESET {
					return
				}
			}
			errl.Printf("%s read data: %v\n", dbgmsg, err)
			return
		}

		_, err = dst.Write(buf[0:n])
		if err != nil {
			if ne, ok := err.(*net.OpError); ok {
				if ne.Err == syscall.EPIPE {
					return
				}
			}
			errl.Printf("%s write data: %v\n", dbgmsg, err)
			return
		}
	}
	return
}

func hasMessage(c chan bool) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
	return false
}

func (c *clientConn) readResponse(srvReader *bufio.Reader, rCh chan *Request, stop chan bool) (err error) {
	var rp *Response
	var r *Request
	for {
		if hasMessage(stop) {
			break
		}
		r = <-rCh
		rp, err = parseResponse(srvReader, r.Method)
		if err != nil {
			errl.Printf("%v parseResponse for %v return %v\n", c.conn.RemoteAddr(), r, err)
			break
		}

		c.buf.WriteString(rp.raw.String())
		// Flush response header to the client ASAP
		if err = c.buf.Flush(); err != nil {
			errl.Printf("Flushing response header to client: %v\n", err)
			break
		}

		// Wrap inside if to avoid function argument evaluation.
		if debug {
			debug.Printf("%v [Response] %s %v\n%v", c.conn.RemoteAddr(), r.Method, r.URL, rp)
		}

		if rp.HasBody {
			if err = sendBody(c.buf.Writer, srvReader, rp.Chunking, rp.ContLen); err != nil {
				break
			}
		}
		debug.Printf("Finished request %s %s\n", r.Method, r.URL)

		if !rp.KeepAlive {
			return
		}
	}
	return
}

func (c *clientConn) getHandler(r *Request) (handler Handler, err error) {
	handler, ok := c.handler[r.URL.Host]
	// create different handler based on whether the request is blocked by [GFW] 
	if !ok {
		return c.createDirectHandler(r)
	}
	return
}

func (c *clientConn) createDirectHandler(r *Request) (Handler, error) {
	var err error
	host := r.URL.Host
	if !hostHasPort(r.URL.Host) {
		host += ":80"
	}
	var srvconn net.Conn
	if srvconn, err = net.Dial("tcp", host); err != nil {
		// TODO Find a way report no host error to client. Send back web page?
		// Time out is very likely to be caused by [GFW]
		errl.Printf("Connecting to: %s %v\n", r.URL.Host, err)
		return nil, err
	}
	debug.Printf("Connected to %s\n", r.URL.Host)
	if r.isConnect {
		// Don't put connection for CONNECT method for reuse
		return &directHandler{Conn: srvconn}, err
	}

	handler := &directHandler{Conn: srvconn,
		stop: make(chan bool), request: make(chan *Request, requestNum)}
	c.handler[r.URL.Host] = handler

	// start goroutine to send response to client
	c.handlerGrp.Add(1)
	go func() {
		if err := c.readResponse(bufio.NewReader(srvconn), handler.request, handler.stop); err != nil {
			errl.Printf("readResponse return error %v\n", err)
		}
		// XXX It's possbile that request is being sent through the connection
		// when we try to remove it. Is there possible error here? The sending
		// side will discover closed connection, so not a big problem.
		debug.Printf("Closing srv conn %v\n", srvconn.RemoteAddr())
		c.removeHandler(r)
		c.handlerGrp.Done()
	}()

	return handler, err
}

func (h *directHandler) NotifyStop() {
	h.stop <- true
}

func (c *clientConn) removeHandler(r *Request) (err error) {
	handler, ok := c.handler[r.URL.Host]
	delete(c.handler, r.URL.Host)
	if ok {
		handler.Close()
	}
	return
}

// Serve client request directly (without using any parent proxy)
func (srvconn *directHandler) ServeRequest(r *Request, c *clientConn) (err error) {
	if r.isConnect {
		return srvconn.doConnect(r, c)
	}
	return srvconn.doRequest(r, c)
}

var connEstablished = []byte("HTTP/1.0 200 Connection established\r\nProxy-agent: cow-proxy/0.1\r\n\r\n")

func (srvconn *directHandler) doConnect(r *Request, c *clientConn) (err error) {
	defer srvconn.Close()
	debug.Printf("Sending 200 Connection established to %s\n", r.URL.Host)
	_, err = c.conn.Write(connEstablished)
	if err != nil {
		errl.Printf("Error sending 200 Connecion established\n")
		return err
	}

	errchan := make(chan error)
	// Must wait this goroutine finish before returning from this function.
	// Otherwise, the server/client may have been closed and thus cause nil
	// pointer deference
	go func() {
		err := copyData(c.conn, bufio.NewReaderSize(srvconn, bufSize), "doConnect server->client")
		errchan <- err
	}()

	err = copyData(srvconn, c.buf.Reader, "doConnect client->server")

	// wait goroutine finish
	err2 := <-errchan
	if err2 != io.EOF {
		return err2
	}
	return
}

func (srvconn directHandler) doRequest(r *Request, c *clientConn) (err error) {
	// Send request to the server
	if _, err = srvconn.Write(r.raw.Bytes()); err != nil {
		// The srv connection maybe already closed.
		// Need to delete the connection and reconnect in that case.
		errl.Printf("writing to connection error: %v\n", err)
		if err == syscall.EPIPE {
			return errPIPE
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
	// Send request method to response read goroutine
	srvconn.request <- r

	return
}

// Send response body if header specifies content length
func sendBodyWithContLen(w *bufio.Writer, r *bufio.Reader, contLen int64) (err error) {
	// debug.Printf("Sending body with content length %d\n", contLen)
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

	done := false
	for !done {
		var s string
		// Read chunk size line, ignore chunk extension if any
		if s, err = ReadLine(r); err != nil {
			errl.Printf("Reading chunk size: %v\n", err)
			return err
		}
		// debug.Printf("Chunk size line %s", s)
		f := strings.SplitN(s, ";", 2)
		var size int64
		if size, err = strconv.ParseInt(f[0], 16, 64); err != nil {
			errl.Printf("Chunk size not valid: %v\n", err)
			return err
		}
		w.WriteString(s)
		w.WriteString("\r\n")

		if size == 0 { // end of chunked data, ignore any trailers
			done = true
		} else {
			// Read chunk data and send to client
			if _, err = io.CopyN(w, r, size); err != nil {
				errl.Printf("Reading chunked data from server: %v\n", err)
				return err
			}
		}

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

	if err != nil {
		return
	}

	if err = w.Flush(); err != nil {
		errl.Printf("Flushing body to client %v\n", err)
		return err
	}
	return
}
