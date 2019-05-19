package main

import (
	"fmt"
	"net"

	core "github.com/shadowsocks/go-shadowsocks2/core"
	ss "github.com/shadowsocks/shadowsocks-go/shadowsocks"
)

// shadowsocks2 parent proxy
type shadowsocks2Parent struct {
	server string
	method string // method and passwd are for upgrade config
	passwd string
	cipher core.Cipher
}

type shadowsocks2Conn struct {
	net.Conn
	parent *shadowsocks2Parent
}

func (s shadowsocks2Conn) String() string {
	return "shadowsocks2 proxy " + s.parent.server
}

// In order to use parent proxy in the order specified in the config file, we
// insert an uninitialized proxy into parent proxy list, and initialize it
// when all its config have been parsed.

func newShadowsocks2Parent(server string) *shadowsocks2Parent {
	return &shadowsocks2Parent{server: server}
}

func (sp *shadowsocks2Parent) getServer() string {
	return sp.server
}

func (sp *shadowsocks2Parent) genConfig() string {
	method := sp.method
	if method == "" {
		method = "table"
	}
	return fmt.Sprintf("proxy = ss://%s:%s@%s", method, sp.passwd, sp.server)
}

func (sp *shadowsocks2Parent) initCipher(method, passwd string) error {
	sp.method = method
	sp.passwd = passwd
	cipher, err := core.PickCipher(method, nil, passwd)
	if err != nil {
		return err
	}
	sp.cipher = cipher
	return nil
}

func (sp *shadowsocks2Parent) connect(url *URL) (net.Conn, error) {
	rawaddr, err := ss.RawAddr(url.HostPort)
	if err != nil {
		errl.Printf("can't connect to shadowsocks parent %s for %s: %v\n",
			sp.server, url.HostPort, err)
		return nil, err
	}

	conn, err := net.Dial("tcp", sp.server)
	if err != nil {
		errl.Printf("can't connect to shadowsocks parent %s for %s: %v\n",
			sp.server, url.HostPort, err)
		return nil, err
	}

	conn = sp.cipher.StreamConn(conn)

	if _, err = conn.Write(rawaddr); err != nil {
		conn.Close()
		errl.Printf("can't connect to shadowsocks parent %s for %s: %v\n",
			sp.server, url.HostPort, err)
		return nil, err
	}

	debug.Println("connected to:", url.HostPort, "via shadowsocks2:", sp.server)
	return conn, nil
}
