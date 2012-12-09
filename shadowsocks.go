package main

import (
	"errors"
	"github.com/cyfdecyf/shadowsocks-go/src/shadowsocks"
)

var hasShadowSocksServer = false

func initShadowSocks() {
	if config.shadowSocks != "" && config.shadowPasswd != "" {
		shadowsocks.InitTable(config.shadowPasswd)
		hasShadowSocksServer = true
		return
	}
	if (config.shadowSocks != "" && config.shadowPasswd == "") ||
		(config.shadowSocks == "" && config.shadowPasswd != "") {
		errl.Println("Missing option: shadowSocks and shadowPasswd should be both given")
	}
}

var noShadowSocksErr = errors.New("No shadowsocks configuration")

func createShadowSocksConnection(hostFull string) (cn conn, err error) {
	if !hasShadowSocksServer {
		return zeroConn, noShadowSocksErr
	}
	c, err := shadowsocks.Dial(hostFull, config.shadowSocks)
	if err != nil {
		// debug.Printf("Creating shadowsocks connection: %s %v\n", hostFull, err)
		return zeroConn, err
	}
	// debug.Println("shadowsocks connection created to:", hostFull)
	return conn{c, shadowSocksConn}, nil
}
