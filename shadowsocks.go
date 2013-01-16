package main

import (
	"errors"
	ss "github.com/shadowsocks/shadowsocks-go/shadowsocks"
	"os"
)

var hasShadowSocksServer = false

var noShadowSocksErr = errors.New("No shadowsocks configuration")

var cipher ss.Cipher

func initShadowSocks() {
	if config.ShadowSocks != "" && config.ShadowPasswd != "" {
		hasShadowSocksServer = true
		var err error
		if err = ss.SetDefaultCipher(config.ShadowMethod); err != nil {
			errl.Println("Initializing shadowsocks:", err)
			os.Exit(1)
		}
		if cipher, err = ss.NewCipher(config.ShadowPasswd); err != nil {
			errl.Println("Creating shadowsocks cipher:", err)
			os.Exit(1)
		}
		debug.Println("shadowsocks server:", config.ShadowSocks)
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
	c, err := ss.Dial(hostFull, config.ShadowSocks, cipher.Copy())
	if err != nil {
		debug.Printf("Can't create shadowsocks connection for: %s %v\n", hostFull, err)
		return zeroConn, err
	}
	// debug.Println("shadowsocks connection created to:", hostFull)
	return conn{c, ctShadowctSocksConn}, nil
}
