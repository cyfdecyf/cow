package main

import (
	ss "github.com/shadowsocks/shadowsocks-go/shadowsocks"
)

type shadowsocksParent struct {
	server string
	cipher *ss.Cipher
}

// In order to use parent proxy in the order specified in the config file, we
// insert an uninitialized proxy into parent proxy list, and initialize it
// when all its config have been parsed.

func newShadowsocksParent(server string) *shadowsocksParent {
	return &shadowsocksParent{server, nil}
}

func (sp *shadowsocksParent) initCipher(passwd, method string) {
	cipher, err := ss.NewCipher(method, passwd)
	if err != nil {
		Fatal("creating shadowsocks cipher:", err)
	}
	sp.cipher = cipher
}

func (sp *shadowsocksParent) connect(url *URL) (conn, error) {
	c, err := ss.Dial(url.HostPort, sp.server, sp.cipher.Copy())
	if err != nil {
		errl.Printf("can't create shadowsocks connection for: %s %v\n", url.HostPort, err)
		return zeroConn, err
	}
	debug.Println("connected to:", url.HostPort, "via shadowsocks:", sp.server)
	return conn{ctShadowctSocksConn, c, sp}, nil
}
