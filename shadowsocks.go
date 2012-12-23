package main

import (
	"errors"
	ss "github.com/shadowsocks/shadowsocks-go/shadowsocks"
)

var hasShadowSocksServer = false

var noShadowSocksErr = errors.New("No shadowsocks configuration")

var encTbl *ss.EncryptTable

func initShadowSocks() {
	if config.ShadowSocks != "" && config.ShadowPasswd != "" {
		hasShadowSocksServer = true
		debug.Println("shadowsocks server:", config.ShadowSocks)
		encTbl = ss.GetTable(config.ShadowPasswd)
		return
	}
	if (config.ShadowSocks != "" && config.ShadowPasswd == "") ||
		(config.ShadowSocks == "" && config.ShadowPasswd != "") {
		errl.Println("Missing option: shadowSocks and shadowPasswd should be both given")
	}
}

func createShadowSocksConnection(hostFull string) (cn conn, err error) {
	if !hasShadowSocksServer {
		return zeroConn, noShadowSocksErr
	}
	c, err := ss.Dial(hostFull, config.ShadowSocks, encTbl)
	if err != nil {
		debug.Printf("Can't create shadowsocks connection for: %s %v\n", hostFull, err)
		return zeroConn, err
	}
	// debug.Println("shadowsocks connection created to:", hostFull)
	return conn{c, ctShadowctSocksConn}, nil
}
