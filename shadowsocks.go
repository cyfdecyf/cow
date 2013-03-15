package main

import (
	"errors"
	"fmt"
	ss "github.com/shadowsocks/shadowsocks-go/shadowsocks"
	"os"
)

var noShadowSocksErr = errors.New("No shadowsocks configuration")

var cipher ss.Cipher

func initShadowSocks() {
	// error checking is done in checkConfig
	if config.ShadowSocks == "" {
		return
	}
	var err error
	if cipher, err = ss.NewCipher(config.ShadowMethod, config.ShadowPasswd); err != nil {
		fmt.Println("Creating shadowsocks cipher:", err)
		os.Exit(1)
	}
	debug.Println("shadowsocks server:", config.ShadowSocks)
}

func createShadowSocksConnection(url *URL) (cn conn, err error) {
	if len(config.ShadowSocks) == 0 {
		return zeroConn, noShadowSocksErr
	}
	c, err := ss.Dial(url.HostPort, config.ShadowSocks, cipher.Copy())
	if err != nil {
		errl.Printf("Can't create shadowsocks connection for: %s %v\n", url.HostPort, err)
		return zeroConn, err
	}
	debug.Println("shadowsocks connection created to:", url.HostPort)
	return conn{c, ctShadowctSocksConn}, nil
}
