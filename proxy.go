package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	// "reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Lots of the code here are learnt from the http package

type Proxy struct {
	addr string // listen address
}

// Number of the simultaneous requests in the pipeline
const requestNum = 5

type connectionType int

const (
	directConn connectionType = iota
	socksConn
	nilConn // Error creating connection
)

type Handler struct {
	net.Conn
	connType connectionType
	stop     chan bool     // Used to notify the handler to stop execution
	request  chan *Request // Pass HTTP method from request reader to response reader

	// GFW may return wrong DNS record, which we can connect to but block
	// forever on read. (e.g. twitter.com) If we have never received any
	// response yet, then we should set a timeout for read/write.
	hasReceivedResponse bool
}

type clientConn struct {
	buf        *bufio.ReadWriter
	net.Conn                       // connection to the proxy client
	handler    map[string]*Handler // request handler, host:port as key
	handlerGrp sync.WaitGroup      // Wait all handler to finish before close
}

var (
	errPIPE = errors.New("Error: broken pipe")
)

func NewProxy(addr string) *Proxy {
	return &Proxy{addr: addr}
}

func (py *Proxy) Serve() {
	ln, err := net.Listen("tcp", py.addr)
	if err != nil {
		info.Println("Server creation failed:", err)
		os.Exit(1)
	}
	info.Println("COW proxy listening", py.addr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			info.Println("Client connection:", err)
			continue
		}
		debug.Println("New Client:", conn.RemoteAddr())
		c := newClientConn(conn)
		go c.serve()
	}
}

// Explicitly specify buffer size to avoid unnecessary copy using
// bufio.Reader's Read
const bufSize = 4096

func newClientConn(rwc net.Conn) (c *clientConn) {
	c = &clientConn{Conn: rwc, handler: map[string]*Handler{}}
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
	if c != nil {
		debug.Printf("Client %v connection closed\n", c.RemoteAddr())
		c.Close()
		c = nil
	}
	runtime.GC()
}

func isSelfURL(h string) bool {
	return h == "" || h == selfURL127 || h == selfURLLH
}

func (c *clientConn) serve() {
	defer c.close()
	var r *Request
	var err error
	var handler *Handler

	// Refer to implementation.md for the design choices on parsing the request
	// and response.
	for {
		if r, err = parseRequest(c.buf.Reader); err != nil {
			// io.EOF means the client connection is closed
			if err != io.EOF {
				errl.Println("Reading client request:", err)
			}
			return
		}
		if dbgRq {
			dbgRq.Printf("%v %v\n", c.RemoteAddr(), r)
		}
		if isSelfURL(r.URL.Host) {
			// Send PAC file if requesting self
			sendPAC(c.buf.Writer)
			return
		}

	RETRY:
		if handler, err = c.getHandler(r); err == nil {
			err = handler.ServeRequest(r, c)
		}

		if err != nil {
			if err == errPIPE {
				c.removeHandler(r.URL.Host)
				debug.Println("Retrying request:", r)
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

// What value is appropriate?
var rwDeadline = time.Duration(10) * time.Second

func (c *clientConn) readResponse(srvReader *bufio.Reader, handler *Handler) (err error) {
	var rp *Response
	var r *Request
	for {
		if hasMessage(handler.stop) {
			debug.Println("readResponse stop requested")
			break
		}
		if !handler.hasReceivedResponse && handler.connType == directConn {
			// Note we will only receive response after request has sent.
			// So the time out here should take request sending time into account.
			handler.Conn.SetDeadline(time.Now().Add(rwDeadline))
		}
		rp, err = parseResponse(srvReader)
		if err != nil {
			if err != io.EOF {
				r = <-handler.request
				// debug.Println("Type of error", reflect.TypeOf(err))
				ne, ok := err.(*net.OpError)
				if !ok {
					return
				}
				// GFW may connection reset here, may also make it time out Is
				// it normal for connection to a site timeout? If so, it's
				// better not add it to blocked site
				host, _ := splitHostPort(r.URL.Host)
				detailMsg := fmt.Sprintf("<p>HTTP Request <strong>%v</strong></p>", r)
				if !hostIsIP(host) && handler.connType == directConn {
					detailMsg += fmt.Sprintf(
						"<p>Domain <strong>%s</strong> added to blocked list. <strong>Try to refresh.</strong></p>",
						host)
				}
				if ne.Err == syscall.ECONNRESET {
					if handler.connType == directConn {
						addBlockedRequest(r)
					}
					sendErrorPage(c.buf.Writer, "503", "Connection reset",
						ne.Error(), detailMsg)
				} else if ne.Timeout() {
					if handler.connType == directConn {
						addBlockedRequest(r)
					}
					sendErrorPage(c.buf.Writer, "504", "Time out reading response",
						ne.Error(), detailMsg)
				}
			}
			return
		}
		if !handler.hasReceivedResponse && handler.connType == directConn {
			// After have received the first reponses from the server, we
			// consider ther server as real instead of fake one caused by
			// wrong DNS reply. So don't time out later.
			handler.Conn.SetReadDeadline(time.Time{})
			handler.hasReceivedResponse = false
		}

		c.buf.WriteString(rp.raw.String())
		// Flush response header to the client ASAP
		if err = c.buf.Flush(); err != nil {
			errl.Println("Flushing response header to client:", err)
			return
		}

		// Must come after parseResponse, so closed server
		// connection can be detected ASAP
		r = <-handler.request
		// Wrap inside if to avoid function argument evaluation.
		if dbgRep {
			dbgRep.Printf("%v %s %v %v", c.RemoteAddr(), r.Method, r.URL, rp)
		}

		if rp.hasBody(r.Method) {
			if err = sendBody(c.buf.Writer, srvReader, rp.Chunking, rp.ContLen); err != nil {
				if err != io.EOF {
					debug.Println("readResponse sendBody:", err)
				}
				return
			}
		}
		/*
			if debug {
				debug.Printf("[Finished] %v request %s %s\n", c.RemoteAddr(), r.Method, r.URL)
			}
		*/

		if !rp.KeepAlive {
			return
		}
	}
	return
}

func (c *clientConn) getHandler(r *Request) (handler *Handler, err error) {
	handler, ok := c.handler[r.URL.Host]

	if !ok {
		handler, err = c.createHandler(r)
	}
	return
}

var dialTimeout = time.Duration(5) * time.Second

func createDirectConnection(host string) (net.Conn, connectionType, error) {
	c, err := net.DialTimeout("tcp", host, dialTimeout)
	if err != nil {
		// TODO Find a way report no host error to client. Send back web page?
		// Time out is very likely to be caused by [GFW]
		errl.Printf("Connecting to: %s %v\n", host, err)
		return nil, nilConn, err
	}
	debug.Println("Connected to", host)
	return c, directConn, nil
}

func (c *clientConn) createHandler(r *Request) (*Handler, error) {
	var err error
	var srvconn net.Conn
	var ct connectionType
	connFailed := false

	if isRequestBlocked(r) {
		// In case of connection error to socks server, fallback to direct connection
		if srvconn, ct, err = createSocksConnection(r.URL.Host); err != nil {
			if srvconn, ct, err = createDirectConnection(r.URL.Host); err != nil {
				connFailed = true
				goto connDone
			}
			addDirectRequest(r)
		}
	} else {
		// In case of error on direction connection, try socks server
		if srvconn, ct, err = createDirectConnection(r.URL.Host); err != nil {
			// debug.Printf("type of err %v\n", reflect.TypeOf(err))
			// GFW may cause dns lookup fail, may also cause connection time out
			if _, ok := err.(*net.DNSError); ok {
			} else if ne, ok := err.(*net.OpError); ok && ne.Timeout() {
			} else {
				connFailed = true
				goto connDone
			}

			// Try to create socks connection
			if srvconn, ct, err = createSocksConnection(r.URL.Host); err != nil {
				connFailed = true
				goto connDone
			}
			addBlockedRequest(r)
		} else {
			addDirectRequest(r)
		}
	}

connDone:
	if connFailed {
		sendErrorPage(c.buf.Writer, "504", "Connection failed", err.Error(),
			fmt.Sprintf("<p>HTTP Request <strong>%v</strong></p>", r))
		return nil, err
	}

	if r.isConnect {
		// Don't put connection for CONNECT method for reuse
		return &Handler{Conn: srvconn, connType: ct}, err
	}

	handler := &Handler{Conn: srvconn, connType: ct,
		stop: make(chan bool), request: make(chan *Request, requestNum)}
	c.handler[r.URL.Host] = handler

	// start goroutine to send response to client
	c.handlerGrp.Add(1)
	go func() {
		c.readResponse(bufio.NewReader(srvconn), handler)
		// XXX It's possbile that request is being sent through the connection
		// when we try to remove it. Is there possible error here? The sending
		// side will discover closed connection, so not a big problem.
		debug.Println("Closing srv conn", srvconn.RemoteAddr())
		c.removeHandler(r.URL.Host)
		c.handlerGrp.Done()
	}()

	return handler, err
}

func (h *Handler) NotifyStop() {
	h.stop <- true
}

func (c *clientConn) removeHandler(host string) (err error) {
	handler, ok := c.handler[host]
	if ok {
		delete(c.handler, host)
		handler.Close()
	}
	return
}

// Serve client request through network connection
func (h *Handler) ServeRequest(r *Request, c *clientConn) (err error) {
	if r.isConnect {
		return h.doConnect(r, c)
	}
	return h.doRequest(r, c)
}

var connEstablished = []byte("HTTP/1.0 200 Connection established\r\nProxy-agent: cow-proxy/0.1\r\n\r\n")

// Do HTTP CONNECT
func (srvconn *Handler) doConnect(r *Request, c *clientConn) (err error) {
	defer srvconn.Close()
	if debug {
		debug.Printf("%v 200 Connection established to %s\n", c.RemoteAddr(), r.URL.Host)
	}
	_, err = c.Write(connEstablished)
	if err != nil {
		errl.Printf("%v Error sending 200 Connecion established\n", c.RemoteAddr())
		return err
	}

	errchan := make(chan error)
	// Must wait this goroutine finish before returning from this function.
	// Otherwise, the server/client may have been closed and thus cause nil
	// pointer deference
	go func() {
		err := copyData(c, bufio.NewReaderSize(srvconn, bufSize), "doConnect server->client")
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

// Do HTTP request other that CONNECT
func (srvconn *Handler) doRequest(r *Request, c *clientConn) (err error) {
	// Send request to the server
	if _, err = srvconn.Write(r.raw.Bytes()); err != nil {
		// The srv connection maybe already closed.
		// Need to delete the connection and reconnect in that case.
		errl.Println("writing to connection error:", err)
		if err == syscall.EPIPE {
			return errPIPE
		} else {
			return err
		}
	}

	// Send request body
	if r.Method == "POST" {
		if err = sendBody(bufio.NewWriter(srvconn), c.buf.Reader, r.Chunking, r.ContLen); err != nil {
			errl.Println("Sending request body:", err)
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
	// debug.Println("Sending body with content length", contLen)
	if contLen == 0 {
		return
	}
	// CopyN will copy n bytes unless there's error of EOF. For EOF, it means
	// the connection is closed, return will propagate till serv function and
	// close client connection.
	if _, err = io.CopyN(w, r, contLen); err != nil {
		errl.Println("Sending response body to client", err)
		return err
	}
	return
}

// Send response body if header specifies chunked encoding
func sendBodyChunked(w *bufio.Writer, r *bufio.Reader) (err error) {
	// debug.Println("Sending chunked body")

	done := false
	for !done {
		var s string
		// Read chunk size line, ignore chunk extension if any
		if s, err = ReadLine(r); err != nil {
			errl.Println("Reading chunk size:", err)
			return err
		}
		// debug.Println("Chunk size line", s)
		f := strings.SplitN(s, ";", 2)
		var size int64
		if size, err = strconv.ParseInt(strings.TrimSpace(f[0]), 16, 64); err != nil {
			errl.Println("Chunk size not valid:", err)
			return err
		}
		w.WriteString(s)
		w.WriteString("\r\n")

		if size == 0 { // end of chunked data, ignore any trailers
			done = true
		} else {
			// Read chunk data and send to client
			if _, err = io.CopyN(w, r, size); err != nil {
				errl.Println("Reading chunked data from server:", err)
				return err
			}
		}

		// XXX maybe this kind of error handling should be passed to the
		// client? But if the proxy doesn't know when to stop reading from the
		// server, the only way to avoid blocked reading is to set read time
		// out on server connection. Would that be easier?
		if err = readCheckCRLF(r); err != nil {
			errl.Println("Reading chunked data CRLF:", err)
			return err
		}
		w.WriteString("\r\n")
	}
	return
}

func sendBodySplitIntoChunk(w *bufio.Writer, r *bufio.Reader) (err error) {
	buf := make([]byte, bufSize)
	var n int
	for {
		n, err = r.Read(buf)
		// debug.Println("split into chunk n =", n, "err =", err)
		if err != nil {
			// err maybe EOF which is expected here as the server is closing connection
			// For other errors, report the error it in readResponse
			w.WriteString("0\r\n\r\n")
			break
		}

		w.WriteString(fmt.Sprintf("%x\r\n", n))
		w.Write(buf[:n])
	}
	w.Flush()
	return
}

// Send message body
func sendBody(w *bufio.Writer, r *bufio.Reader, chunk bool, contLen int64) (err error) {
	if chunk {
		err = sendBodyChunked(w, r)
	} else if contLen >= 0 {
		err = sendBodyWithContLen(w, r, contLen)
	} else {
		// Server use close connection to indicate end of data
		err = sendBodySplitIntoChunk(w, r)
	}

	if err != nil {
		return
	}

	if err = w.Flush(); err != nil {
		errl.Println("Flushing body to client:", err)
		return err
	}
	return
}

func hostIsIP(host string) bool {
	return net.ParseIP(host) != nil
}
