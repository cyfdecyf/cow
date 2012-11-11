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
	"syscall"
	"time"
)

// Lots of the code here are learnt from the http package

type Proxy struct {
	addr string // listen address
}

// Number of the simultaneous requests in the pipeline
const requestNum = 5

type connType int

const (
	nilConn connType = iota
	directConn
	socksConn
)

type conn struct {
	net.Conn
	connType
}

type Handler struct {
	conn
	host    string
	stop    notification // Used to notify the handler to stop execution
	stopped bool
	request chan *Request // Receive HTTP request from request goroutine

	// GFW may return wrong DNS record, which we can connect to but block
	// forever on read. (e.g. twitter.com) If we have never received any
	// response yet, then we should set a timeout for read/write.
	hasReceivedResponse bool
}

func newHandler(c conn, host string) *Handler {
	return &Handler{conn: c, host: host}
}

type clientConn struct {
	buf      *bufio.ReadWriter
	net.Conn                     // connection to the proxy client
	handler  map[string]*Handler // request handler, host:port as key
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
			debug.Println("Client connection:", err)
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
	// There's no need to wait response goroutine finish. They will finish
	// When see the stop notification or detects client connection error.
	for _, h := range c.handler {
		h.stop.notify()
	}
	// Can't set c.buf to nil because maybe used in response goroutine
	c.Close()
	debug.Printf("Client %v connection closed\n", c.RemoteAddr())
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
				delete(c.handler, handler.host)
				debug.Println("Retrying request:", r)
				goto RETRY
			}
			if err != io.EOF {
				debug.Println("Serve request return error:", err)
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

func copyData(dst, src net.Conn, srcReader *bufio.Reader, dstStopped notification, dbgmsg string) (err error) {
	buf := make([]byte, bufSize)
	var n int
	for {
		if dstStopped.hasNotified() {
			debug.Println(dbgmsg, "dst has stopped")
			return
		}
		if err = src.SetReadDeadline(time.Now().Add(rwDeadline)); err != nil {
			debug.Println("Set ReadDeadline in copyData:", err)
		}
		if n, err = srcReader.Read(buf); err != nil {
			if ne, ok := err.(*net.OpError); ok && ne.Timeout() {
				continue
			}
			if err != io.EOF {
				debug.Printf("%s read data: %v\n", dbgmsg, err)
			}
			return
		}

		_, err = dst.Write(buf[0:n])
		if err != nil {
			debug.Printf("%s write data: %v\n", dbgmsg, err)
			return
		}
	}
	return
}

func genErrMsg(r *Request) string {
	return fmt.Sprintf("<p>HTTP Request <strong>%v</strong></p>", r)
}

// What value is appropriate?
var rwDeadline = time.Duration(10) * time.Second

func (c *clientConn) readResponse(handler *Handler) (err error) {
	var rp *Response
	var r *Request
	var srvReader *bufio.Reader = bufio.NewReader(handler)
	for {
		if handler.stop.hasNotified() {
			debug.Println("readResponse stop requested")
			break
		}
		if !handler.hasReceivedResponse && handler.connType == directConn {
			// Wait for the first request to be sent
			r = <-handler.request
			if err = handler.Conn.SetReadDeadline(time.Now().Add(rwDeadline)); err != nil {
				debug.Println("Setting ReadDeadline before receiving the first response")
			}
		}
		rp, err = parseResponse(srvReader)
		if err != nil {
			// TODO check if client has closed connection
			if err != io.EOF {
				if handler.hasReceivedResponse || handler.connType != directConn {
					r = <-handler.request
				}
				detailMsg := genErrMsg(r)
				// debug.Println("Type of error", reflect.TypeOf(err))
				ne, ok := err.(*net.OpError)
				if !ok {
					sendErrorPage(c.buf.Writer, "502", "read error", err.Error(), detailMsg)
					return
				}
				// GFW may connection reset here, may also make it time out Is
				// it normal for connection to a site timeout? If so, it's
				// better not add it to blocked site
				host, _ := splitHostPort(r.URL.Host)
				if !hostIsIP(host) && handler.connType == directConn {
					detailMsg += fmt.Sprintf(
						"<p>Domain <strong>%s</strong> added to blocked list. <strong>Try to refresh.</strong></p>",
						host2Domain(host))
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

		c.buf.WriteString(rp.raw.String())
		// Flush response header to the client ASAP
		if err = c.buf.Flush(); err != nil {
			debug.Println("Flushing response header to client:", err)
			return
		}

		if !handler.hasReceivedResponse && handler.connType == directConn {
			// After have received the first reponses from the server, we
			// consider ther server as real instead of fake one caused by
			// wrong DNS reply. So don't time out later.
			if err = handler.Conn.SetReadDeadline(time.Time{}); err != nil {
				debug.Println("Unset ReadDeadline")
			}
			handler.hasReceivedResponse = false
		} else {
			// Must come after parseResponse, so closed server
			// connection can be detected ASAP
			r = <-handler.request
		}

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
	if ok && handler.stopped {
		delete(c.handler, handler.host)
		ok = false
	}

	if !ok {
		handler, err = c.createHandler(r)
	}
	return
}

var dialTimeout = time.Duration(5) * time.Second

func createDirectConnection(host string) (conn, error) {
	c, err := net.DialTimeout("tcp", host, dialTimeout)
	if err != nil {
		// Time out is very likely to be caused by [GFW]
		debug.Printf("Connecting to: %s %v\n", host, err)
		return conn{nil, nilConn}, err
	}
	debug.Println("Connected to", host)
	return conn{c, directConn}, nil
}

func (c *clientConn) createHandler(r *Request) (*Handler, error) {
	var err error
	var srvconn conn
	connFailed := false

	if isRequestBlocked(r) {
		// In case of connection error to socks server, fallback to direct connection
		if srvconn, err = createSocksConnection(r.URL.Host); err != nil {
			if srvconn, err = createDirectConnection(r.URL.Host); err != nil {
				connFailed = true
				goto connDone
			}
			addDirectRequest(r)
		}
	} else {
		// In case of error on direction connection, try socks server
		if srvconn, err = createDirectConnection(r.URL.Host); err != nil {
			// debug.Printf("type of err %v\n", reflect.TypeOf(err))
			// GFW may cause dns lookup fail, may also cause connection time out
			if _, ok := err.(*net.DNSError); ok {
			} else if ne, ok := err.(*net.OpError); ok && ne.Timeout() {
			} else {
				connFailed = true
				goto connDone
			}

			// Try to create socks connection
			if srvconn, err = createSocksConnection(r.URL.Host); err != nil {
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
		sendErrorPage(c.buf.Writer, "504", "Connection failed", err.Error(), genErrMsg(r))
		return nil, err
	}

	handler := newHandler(srvconn, r.URL.Host)
	if r.isConnect {
		// Don't put connection for CONNECT method for reuse
		return handler, err
	}

	handler.stop = newNotification()
	handler.request = make(chan *Request, requestNum)

	c.handler[r.URL.Host] = handler
	go func() {
		c.readResponse(handler)
		// It's possbile that request is being sent through the server
		// connection. The sending side will discover closed server connection
		// and try to redo the request.

		// TODO find a way to delete stopped handler in the request goroutine
		// Not removing the handler from client's handler map will stop the
		// handler from being recycled. But as client connection will close at
		// some point, it's not likely to have memory leak.
		debug.Println("Closing srv conn", srvconn.RemoteAddr())
		handler.Close()
		handler.stopped = true
	}()

	return handler, err
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

	// Notify the destination has stopped in copyData is important. If the
	// client has stopped connection, while the server->client is blocked
	// reading data from the server, the server connection will not get chance
	// to stop (unless there's timeout in read). This may result too many open
	// connection error from the socks server.
	srvStopped := newNotification()
	clientStopped := newNotification()

	// Must wait this goroutine finish before returning from this function.
	// Otherwise, the server/client may have been closed and thus cause nil
	// pointer deference
	go func() {
		err := copyData(c, srvconn, bufio.NewReaderSize(srvconn, bufSize),
			clientStopped, "doConnect server->client")
		srvStopped.notify()
		errchan <- err
	}()

	err = copyData(srvconn, c, c.buf.Reader, srvStopped, "doConnect client->server")
	clientStopped.notify()

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

	// TODO all possible error that caused by closed server connection should
	// redo request
	if _, err = srvconn.Write(r.raw.Bytes()); err != nil {
		// The srv connection maybe already closed.
		// Need to delete the connection and reconnect in that case.
		errl.Println("Sending request header:", err)
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
		debug.Println("Sending response body to client", err)
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
		// Maybe the client has closed the connection
		debug.Println("Flushing body to client:", err)
		return err
	}
	return
}

func hostIsIP(host string) bool {
	return net.ParseIP(host) != nil
}
