package main

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	ss "github.com/shadowsocks/shadowsocks-go/shadowsocks"
	"io"
	"math/rand"
	"net"
	"strconv"
)

func connectByParentProxy(url *URL) (srvconn net.Conn, err error) {
	const baseFailCnt = 9
	var skipped []int
	nproxy := len(parentProxy)

	firstId := 0
	if config.LoadBalance == loadBalanceHash {
		firstId = int(stringHash(url.Host) % uint64(nproxy))
		debug.Println("use proxy", firstId)
	}

	for i := 0; i < nproxy; i++ {
		proxyId := (firstId + i) % nproxy
		pp := &parentProxy[proxyId]
		// skip failed server, but try it with some probability
		if pp.failCnt > 0 && rand.Intn(pp.failCnt+baseFailCnt) != 0 {
			skipped = append(skipped, proxyId)
			continue
		}
		if srvconn, err = pp.connect(url); err == nil {
			return
		}
	}
	// last resort, try skipped one, not likely to succeed
	for _, skippedId := range skipped {
		if srvconn, err = parentProxy[skippedId].connect(url); err == nil {
			return
		}
	}
	if len(parentProxy) != 0 {
		return
	}
	return nil, errors.New("no parent proxy")
}

// proxyConnector is the interface that all parent proxies should support.
type proxyConnector interface {
	connect(*URL) (net.Conn, error)
}

type ParentProxy struct {
	proxyConnector
	failCnt int
}

var parentProxy []ParentProxy

func addParentProxy(pc proxyConnector) {
	parentProxy = append(parentProxy, ParentProxy{pc, 0})
}

func (pp *ParentProxy) connect(url *URL) (srvconn net.Conn, err error) {
	const maxFailCnt = 30
	srvconn, err = pp.proxyConnector.connect(url)
	if err != nil {
		if pp.failCnt < maxFailCnt && !networkBad() {
			pp.failCnt++
		}
		return
	}
	pp.failCnt = 0
	return
}

func printParentProxy() {
	debug.Println("avaiable parent proxies:")
	for _, pp := range parentProxy {
		switch pc := pp.proxyConnector.(type) {
		case *shadowsocksParent:
			debug.Println("\tshadowsocks: ", pc.server)
		case *httpParent:
			debug.Println("\thttp parent: ", pc.server)
		case *socksParent:
			debug.Println("\tsocks parent: ", pc.server)
		}
	}
}

// http parent proxy
type httpParent struct {
	server     string
	authHeader []byte
}

type httpConn struct {
	net.Conn
	parent *httpParent
}

func (hc httpConn) String() string {
	return "http parent proxy " + hc.parent.server
}

func newHttpParent(server string) *httpParent {
	return &httpParent{server: server}
}

func (hp *httpParent) initAuth(userPasswd string) {
	if userPasswd == "" {
		return
	}
	b64 := base64.StdEncoding.EncodeToString([]byte(userPasswd))
	hp.authHeader = []byte(headerProxyAuthorization + ": Basic " + b64 + CRLF)
}

func (hp *httpParent) connect(url *URL) (net.Conn, error) {
	c, err := net.Dial("tcp", hp.server)
	if err != nil {
		errl.Printf("can't connect to http parent proxy %s for %s: %v\n", hp.server, url.HostPort, err)
		return nil, err
	}
	debug.Println("connected to:", url.HostPort, "via http parent proxy:", hp.server)
	return httpConn{c, hp}, nil
}

// shadowsocks parent proxy
type shadowsocksParent struct {
	server string
	cipher *ss.Cipher
}

type shadowsocksConn struct {
	net.Conn
	parent *shadowsocksParent
}

func (s shadowsocksConn) String() string {
	return "shadowsocks proxy " + s.parent.server
}

// In order to use parent proxy in the order specified in the config file, we
// insert an uninitialized proxy into parent proxy list, and initialize it
// when all its config have been parsed.

func newShadowsocksParent(server string) *shadowsocksParent {
	return &shadowsocksParent{server, nil}
}

func (sp *shadowsocksParent) initCipher(method, passwd string) error {
	if method == "table" {
		method = ""
	}
	cipher, err := ss.NewCipher(method, passwd)
	if err != nil {
		return err
	}
	sp.cipher = cipher
	return nil
}

func (sp *shadowsocksParent) connect(url *URL) (net.Conn, error) {
	c, err := ss.Dial(url.HostPort, sp.server, sp.cipher.Copy())
	if err != nil {
		errl.Printf("create shadowsocks connection to %s through server %s failed %v\n",
			url.HostPort, sp.server, err)
		return nil, err
	}
	debug.Println("connected to:", url.HostPort, "via shadowsocks:", sp.server)
	return shadowsocksConn{c, sp}, nil
}

// For socks documentation, refer to rfc 1928 http://www.ietf.org/rfc/rfc1928.txt

var socksError = [...]string{
	1: "General SOCKS server failure",
	2: "Connection not allowed by ruleset",
	3: "Network unreachable",
	4: "Host unreachable",
	5: "Connection refused",
	6: "TTL expired",
	7: "Command not supported",
	8: "Address type not supported",
	9: "to X'FF' unassigned",
}

var socksProtocolErr = errors.New("socks protocol error")

var socksMsgVerMethodSelection = []byte{
	0x5, // version 5
	1,   // n method
	0,   // no authorization required
}

// socks5 parent proxy
type socksParent struct {
	server string
}

type socksConn struct {
	net.Conn
	parent *socksParent
}

func (s socksConn) String() string {
	return "socks proxy " + s.parent.server
}

func newSocksParent(server string) *socksParent {
	return &socksParent{server}
}

func (sp *socksParent) connect(url *URL) (net.Conn, error) {
	c, err := net.Dial("tcp", sp.server)
	if err != nil {
		errl.Printf("can't connect to socks server %s for %s: %v\n",
			sp.server, url.HostPort, err)
		return nil, err
	}
	hasErr := false
	defer func() {
		if hasErr {
			c.Close()
		}
	}()

	var n int
	if n, err = c.Write(socksMsgVerMethodSelection); n != 3 || err != nil {
		errl.Printf("sending ver/method selection msg %v n = %v\n", err, n)
		hasErr = true
		return nil, err
	}

	// version/method selection
	repBuf := make([]byte, 2)
	_, err = io.ReadFull(c, repBuf)
	if err != nil {
		errl.Printf("read ver/method selection error %v\n", err)
		hasErr = true
		return nil, err
	}
	if repBuf[0] != 5 || repBuf[1] != 0 {
		errl.Printf("socks ver/method selection reply error ver %d method %d",
			repBuf[0], repBuf[1])
		hasErr = true
		return nil, err
	}
	// debug.Println("Socks version selection done")

	// send connect request
	host := url.Host
	port, err := strconv.Atoi(url.Port)
	if err != nil {
		errl.Printf("should not happen, port error %v\n", port)
		hasErr = true
		return nil, err
	}

	hostLen := len(host)
	bufLen := 5 + hostLen + 2 // last 2 is port
	reqBuf := make([]byte, bufLen)
	reqBuf[0] = 5 // version 5
	reqBuf[1] = 1 // cmd: connect
	// reqBuf[2] = 0 // rsv: set to 0 when initializing
	reqBuf[3] = 3 // atyp: domain name
	reqBuf[4] = byte(hostLen)
	copy(reqBuf[5:], host)
	binary.BigEndian.PutUint16(reqBuf[5+hostLen:5+hostLen+2], uint16(port))

	/*
		if debug {
			debug.Println("Send socks connect request", (url.HostPort))
		}
	*/

	if n, err = c.Write(reqBuf); err != nil || n != bufLen {
		errl.Printf("send socks request err %v n %d\n", err, n)
		hasErr = true
		return nil, err
	}

	// I'm not clear why the buffer is fixed at 10. The rfc document does not say this.
	// Polipo set this to 10 and I also observed the reply is always 10.
	replyBuf := make([]byte, 10)
	if n, err = c.Read(replyBuf); err != nil {
		// Seems that socks server will close connection if it can't find host
		if err != io.EOF {
			errl.Printf("read socks reply err %v n %d\n", err, n)
		}
		hasErr = true
		return nil, errors.New("Connection failed (by socks server). No such host?")
	}
	// debug.Printf("Socks reply length %d\n", n)

	if replyBuf[0] != 5 {
		errl.Printf("socks reply connect %s VER %d not supported\n", url.HostPort, replyBuf[0])
		hasErr = true
		return nil, socksProtocolErr
	}
	if replyBuf[1] != 0 {
		errl.Printf("socks reply connect %s error %s\n", url.HostPort, socksError[replyBuf[1]])
		hasErr = true
		return nil, socksProtocolErr
	}
	if replyBuf[3] != 1 {
		errl.Printf("socks reply connect %s ATYP %d\n", url.HostPort, replyBuf[3])
		hasErr = true
		return nil, socksProtocolErr
	}

	debug.Println("connected to:", url.HostPort, "via socks server:", sp.server)
	// Now the socket can be used to pass data.
	return socksConn{c, sp}, nil
}
