package main

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	ss "github.com/shadowsocks/shadowsocks-go/shadowsocks"
	"hash/crc32"
	"io"
	"math/rand"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Interface that all types of parent proxies should support.
type ParentProxy interface {
	connect(*URL) (net.Conn, error)
	getServer() string // for use in updating server latency
	genConfig() string // for upgrading config
}

// Interface for different proxy selection strategy.
type ParentPool interface {
	add(ParentProxy)
	empty() bool
	// Select a proxy from the pool and connect. May try several proxies until
	// one that succees, return nil and error if all parent proxies fail.
	connect(*URL) (net.Conn, error)
}

// Init parentProxy to be backup pool. So config parsing have a pool to add
// parent proxies.
var parentProxy ParentPool = &backupParentPool{}

func initParentPool() {
	backPool, ok := parentProxy.(*backupParentPool)
	if !ok {
		panic("initial parent pool should be backup pool")
	}
	if debug {
		printParentProxy(backPool.parent)
	}
	if len(backPool.parent) == 0 {
		info.Println("no parent proxy server")
		return
	}
	if len(backPool.parent) == 1 && config.LoadBalance != loadBalanceBackup {
		debug.Println("only 1 parent, no need for load balance")
		config.LoadBalance = loadBalanceBackup
	}

	switch config.LoadBalance {
	case loadBalanceHash:
		debug.Println("hash parent pool", len(backPool.parent))
		parentProxy = &hashParentPool{*backPool}
	case loadBalanceLatency:
		debug.Println("latency parent pool", len(backPool.parent))
		go updateParentProxyLatency()
		parentProxy = newLatencyParentPool(backPool.parent)
	}
}

func printParentProxy(parent []ParentWithFail) {
	debug.Println("avaiable parent proxies:")
	for _, pp := range parent {
		switch pc := pp.ParentProxy.(type) {
		case *shadowsocksParent:
			debug.Println("\tshadowsocks: ", pc.server)
		case *httpParent:
			debug.Println("\thttp parent: ", pc.server)
		case *socksParent:
			debug.Println("\tsocks parent: ", pc.server)
		case *cowParent:
			debug.Println("\tcow parent: ", pc.server)
		}
	}
}

type ParentWithFail struct {
	ParentProxy
	fail int
}

// Backup load balance strategy:
// Select proxy in the order they appear in config.
type backupParentPool struct {
	parent []ParentWithFail
}

func (pp *backupParentPool) empty() bool {
	return len(pp.parent) == 0
}

func (pp *backupParentPool) add(parent ParentProxy) {
	pp.parent = append(pp.parent, ParentWithFail{parent, 0})
}

func (pp *backupParentPool) connect(url *URL) (srvconn net.Conn, err error) {
	return connectInOrder(url, pp.parent, 0)
}

// Hash load balance strategy:
// Each host will use a proxy based on a hash value.
type hashParentPool struct {
	backupParentPool
}

func (pp *hashParentPool) connect(url *URL) (srvconn net.Conn, err error) {
	start := int(crc32.ChecksumIEEE([]byte(url.Host)) % uint32(len(pp.parent)))
	debug.Printf("hash host %s try %d parent first", url.Host, start)
	return connectInOrder(url, pp.parent, start)
}

func (parent *ParentWithFail) connect(url *URL) (srvconn net.Conn, err error) {
	const maxFailCnt = 30
	srvconn, err = parent.ParentProxy.connect(url)
	if err != nil {
		if parent.fail < maxFailCnt && !networkBad() {
			parent.fail++
		}
		return
	}
	parent.fail = 0
	return
}

func connectInOrder(url *URL, pp []ParentWithFail, start int) (srvconn net.Conn, err error) {
	const baseFailCnt = 9
	var skipped []int
	nproxy := len(pp)

	if nproxy == 0 {
		return nil, errors.New("no parent proxy")
	}

	for i := 0; i < nproxy; i++ {
		proxyId := (start + i) % nproxy
		parent := &pp[proxyId]
		// skip failed server, but try it with some probability
		if parent.fail > 0 && rand.Intn(parent.fail+baseFailCnt) != 0 {
			skipped = append(skipped, proxyId)
			continue
		}
		if srvconn, err = parent.connect(url); err == nil {
			return
		}
	}
	// last resort, try skipped one, not likely to succeed
	for _, skippedId := range skipped {
		if srvconn, err = pp[skippedId].connect(url); err == nil {
			return
		}
	}
	return nil, err
}

type ParentWithLatency struct {
	ParentProxy
	latency time.Duration
}

type latencyParentPool struct {
	parent []ParentWithLatency
}

func newLatencyParentPool(parent []ParentWithFail) *latencyParentPool {
	lp := &latencyParentPool{}
	for _, p := range parent {
		lp.add(p.ParentProxy)
	}
	return lp
}

func (pp *latencyParentPool) empty() bool {
	return len(pp.parent) == 0
}

func (pp *latencyParentPool) add(parent ParentProxy) {
	pp.parent = append(pp.parent, ParentWithLatency{parent, 0})
}

// Sort interface.
func (pp *latencyParentPool) Len() int {
	return len(pp.parent)
}

func (pp *latencyParentPool) Swap(i, j int) {
	p := pp.parent
	p[i], p[j] = p[j], p[i]
}

func (pp *latencyParentPool) Less(i, j int) bool {
	p := pp.parent
	return p[i].latency < p[j].latency
}

const latencyMax = time.Hour

var latencyMutex sync.RWMutex

func (pp *latencyParentPool) connect(url *URL) (srvconn net.Conn, err error) {
	var lp []ParentWithLatency
	// Read slice first.
	latencyMutex.RLock()
	lp = pp.parent
	latencyMutex.RUnlock()

	var skipped []int
	nproxy := len(lp)
	if nproxy == 0 {
		return nil, errors.New("no parent proxy")
	}

	for i := 0; i < nproxy; i++ {
		parent := lp[i]
		if parent.latency >= latencyMax {
			skipped = append(skipped, i)
			continue
		}
		if srvconn, err = parent.connect(url); err == nil {
			debug.Println("lowest latency proxy", parent.getServer())
			return
		}
		parent.latency = latencyMax
	}
	// last resort, try skipped one, not likely to succeed
	for _, skippedId := range skipped {
		if srvconn, err = lp[skippedId].connect(url); err == nil {
			return
		}
	}
	return nil, err
}

func (parent *ParentWithLatency) updateLatency(wg *sync.WaitGroup) {
	defer wg.Done()
	proxy := parent.ParentProxy
	server := proxy.getServer()

	host, port, err := net.SplitHostPort(server)
	if err != nil {
		panic("split host port parent server error" + err.Error())
	}

	// Resolve host name first, so latency does not include resolve time.
	ip, err := net.LookupHost(host)
	if err != nil {
		parent.latency = latencyMax
		return
	}
	ipPort := net.JoinHostPort(ip[0], port)

	const N = 3
	var total time.Duration
	for i := 0; i < N; i++ {
		now := time.Now()
		cn, err := net.DialTimeout("tcp", ipPort, dialTimeout)
		if err != nil {
			debug.Println("latency update dial:", err)
			total += time.Minute // 1 minute as penalty
			continue
		}
		total += time.Now().Sub(now)
		cn.Close()

		time.Sleep(5 * time.Millisecond)
	}
	parent.latency = total / N
	debug.Println("latency", server, parent.latency)
}

func (pp *latencyParentPool) updateLatency() {
	// Create a copy, update latency for the copy.
	var cp latencyParentPool
	cp.parent = append(cp.parent, pp.parent...)

	// cp.parent is value instead of pointer, if we use `_, p := range cp.parent`,
	// the value in cp.parent will not be updated.
	var wg sync.WaitGroup
	wg.Add(len(cp.parent))
	for i, _ := range cp.parent {
		cp.parent[i].updateLatency(&wg)
	}
	wg.Wait()

	// Sort according to latency.
	sort.Stable(&cp)
	debug.Println("latency lowest proxy", cp.parent[0].getServer())

	// Update parent slice.
	latencyMutex.Lock()
	pp.parent = cp.parent
	latencyMutex.Unlock()
}

func updateParentProxyLatency() {
	lp, ok := parentProxy.(*latencyParentPool)
	if !ok {
		return
	}

	for {
		lp.updateLatency()
		time.Sleep(60 * time.Second)
	}
}

// http parent proxy
type httpParent struct {
	server     string
	userPasswd string // for upgrade config
	authHeader []byte
}

type httpConn struct {
	net.Conn
	parent *httpParent
}

func (s httpConn) String() string {
	return "http parent proxy " + s.parent.server
}

func newHttpParent(server string) *httpParent {
	return &httpParent{server: server}
}

func (hp *httpParent) getServer() string {
	return hp.server
}

func (hp *httpParent) genConfig() string {
	if hp.userPasswd != "" {
		return fmt.Sprintf("proxy = http://%s@%s", hp.userPasswd, hp.server)
	} else {
		return fmt.Sprintf("proxy = http://%s", hp.server)
	}
}

func (hp *httpParent) initAuth(userPasswd string) {
	if userPasswd == "" {
		return
	}
	hp.userPasswd = userPasswd
	b64 := base64.StdEncoding.EncodeToString([]byte(userPasswd))
	hp.authHeader = []byte(headerProxyAuthorization + ": Basic " + b64 + CRLF)
}

func (hp *httpParent) connect(url *URL) (net.Conn, error) {
	c, err := net.Dial("tcp", hp.server)
	if err != nil {
		errl.Printf("can't connect to http parent %s for %s: %v\n",
			hp.server, url.HostPort, err)
		return nil, err
	}
	debug.Printf("connected to: %s via http parent: %s\n",
		url.HostPort, hp.server)
	return httpConn{c, hp}, nil
}

// https parent proxy
type httpsParent struct {
	server     string
	userPasswd string // for upgrade config
	authHeader []byte
}

type httpsConn struct {
	net.Conn
	parent *httpsParent
}

func (s httpsConn) String() string {
	return "https parent proxy " + s.parent.server
}

func newHttpsParent(server string) *httpsParent {
	return &httpsParent{server: server}
}

func (hp *httpsParent) getServer() string {
	return hp.server
}

func (hp *httpsParent) genConfig() string {
	if hp.userPasswd != "" {
		return fmt.Sprintf("proxy = https://%s@%s", hp.userPasswd, hp.server)
	} else {
		return fmt.Sprintf("proxy = https://%s", hp.server)
	}
}

func (hp *httpsParent) initAuth(userPasswd string) {
	if userPasswd == "" {
		return
	}
	hp.userPasswd = userPasswd
	b64 := base64.StdEncoding.EncodeToString([]byte(userPasswd))
	hp.authHeader = []byte(headerProxyAuthorization + ": Basic " + b64 + CRLF)
}

func (hp *httpsParent) connect(url *URL) (net.Conn, error) {
	c, err := tls.Dial("tcp", hp.server, &tls.Config{
		InsecureSkipVerify: true,
	})
	if err != nil {
		errl.Printf("can't connect to https parent %s for %s: %v\n",
			hp.server, url.HostPort, err)
		return nil, err
	}

	debug.Printf("connected to: %s via https parent: %s\n",
		url.HostPort, hp.server)
	return httpsConn{c, hp}, nil
}

// shadowsocks parent proxy
type shadowsocksParent struct {
	server string
	method string // method and passwd are for upgrade config
	passwd string
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
	return &shadowsocksParent{server: server}
}

func (sp *shadowsocksParent) getServer() string {
	return sp.server
}

func (sp *shadowsocksParent) genConfig() string {
	method := sp.method
	if method == "" {
		method = "table"
	}
	return fmt.Sprintf("proxy = ss://%s:%s@%s", method, sp.passwd, sp.server)
}

func (sp *shadowsocksParent) initCipher(method, passwd string) {
	sp.method = method
	sp.passwd = passwd
	cipher, err := ss.NewCipher(method, passwd)
	if err != nil {
		Fatal("create shadowsocks cipher:", err)
	}
	sp.cipher = cipher
}

func (sp *shadowsocksParent) connect(url *URL) (net.Conn, error) {
	c, err := ss.Dial(url.HostPort, sp.server, sp.cipher.Copy())
	if err != nil {
		errl.Printf("can't connect to shadowsocks parent %s for %s: %v\n",
			sp.server, url.HostPort, err)
		return nil, err
	}
	debug.Println("connected to:", url.HostPort, "via shadowsocks:", sp.server)
	return shadowsocksConn{c, sp}, nil
}

// cow parent proxy
type cowParent struct {
	server string
	method string
	passwd string
	cipher *ss.Cipher
}

type cowConn struct {
	net.Conn
	parent *cowParent
}

func (s cowConn) String() string {
	return "cow proxy " + s.parent.server
}

func newCowParent(srv, method, passwd string) *cowParent {
	cipher, err := ss.NewCipher(method, passwd)
	if err != nil {
		Fatal("create cow cipher:", err)
	}
	return &cowParent{srv, method, passwd, cipher}
}

func (cp *cowParent) getServer() string {
	return cp.server
}

func (cp *cowParent) genConfig() string {
	method := cp.method
	if method == "" {
		method = "table"
	}
	return fmt.Sprintf("proxy = cow://%s:%s@%s", method, cp.passwd, cp.server)
}

func (cp *cowParent) connect(url *URL) (net.Conn, error) {
	c, err := net.Dial("tcp", cp.server)
	if err != nil {
		errl.Printf("can't connect to cow parent %s for %s: %v\n",
			cp.server, url.HostPort, err)
		return nil, err
	}
	debug.Printf("connected to: %s via cow parent: %s\n",
		url.HostPort, cp.server)
	ssconn := ss.NewConn(c, cp.cipher.Copy())
	return cowConn{ssconn, cp}, nil
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

func (sp *socksParent) getServer() string {
	return sp.server
}

func (sp *socksParent) genConfig() string {
	return fmt.Sprintf("proxy = socks5://%s", sp.server)
}

func (sp *socksParent) connect(url *URL) (net.Conn, error) {
	c, err := net.Dial("tcp", sp.server)
	if err != nil {
		errl.Printf("can't connect to socks parent %s for %s: %v\n",
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
		return nil, errors.New("connection failed (by socks server " + sp.server + "). No such host?")
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
