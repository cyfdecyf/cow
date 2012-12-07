package main

import (
	"errors"
	"github.com/cyfdecyf/shadowsocks-go/src/shadowsocks"
)

func initShadowSocks() {
	if config.shadowPasswd != "" {
		if config.shadowSocks == "" {
			goto confnotcomplete
		}
		shadowsocks.InitTable(config.shadowPasswd)
	} else if config.shadowSocks != "" {
		goto confnotcomplete
	}
confnotcomplete:
	errl.Println("Missing option, shadowSocks and shadowPasswd should be both given")
}

var noShadowSocksErr = errors.New("No shadowsocks configuration")

func createShadowSocksConnection(hostFull string) (cn conn, err error) {
	if config.shadowSocks == "" {
		return zeroConn, noShadowSocksErr
	}
	c, err := shadowsocks.Dial(hostFull, config.shadowSocks)
	if err != nil {
		debug.Printf("Creating shadowsocks connection: %s %v\n", hostFull, err)
		return zeroConn, err
	}
	debug.Println("shadowsocks connection created to:", hostFull)
	return conn{c, shadowSocksConn}, nil
}
