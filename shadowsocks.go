package main

import (
	"errors"
	ss "github.com/shadowsocks/shadowsocks-go/shadowsocks"
)

var hasShadowSocksServer = false

var noShadowSocksErr = errors.New("No shadowsocks configuration")

var encTbl *ss.EncryptTable

func initShadowSocks() {
	if config.shadowSocks != "" && config.shadowPasswd != "" {
		hasShadowSocksServer = true
		encTbl = ss.GetTable(config.shadowPasswd)
		return
	}
	if (config.shadowSocks != "" && config.shadowPasswd == "") ||
		(config.shadowSocks == "" && config.shadowPasswd != "") {
		errl.Println("Missing option: shadowSocks and shadowPasswd should be both given")
	}
}

func createShadowSocksConnection(hostFull string) (cn conn, err error) {
	if !hasShadowSocksServer {
		return zeroConn, noShadowSocksErr
	}
	c, err := ss.Dial(hostFull, config.shadowSocks, encTbl)
	if err != nil {
		// debug.Printf("Creating shadowsocks connection: %s %v\n", hostFull, err)
		return zeroConn, err
	}
	// debug.Println("shadowsocks connection created to:", hostFull)
	return conn{c, ctShadowctSocksConn}, nil
}
