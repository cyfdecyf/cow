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
	if config.ShadowSocks != "" && config.ShadowPasswd != "" {
		var err error
		if err = ss.SetDefaultCipher(config.ShadowMethod); err != nil {
			fmt.Println("Initializing shadowsocks:", err)
			os.Exit(1)
		}
		if cipher, err = ss.NewCipher(config.ShadowPasswd); err != nil {
			fmt.Println("Creating shadowsocks cipher:", err)
			os.Exit(1)
		}
		debug.Println("shadowsocks server:", config.ShadowSocks)
		return
	}
	if (config.ShadowSocks != "" && config.ShadowPasswd == "") ||
		(config.ShadowSocks == "" && config.ShadowPasswd != "") {
		fmt.Println("Missing option: shadowSocks and shadowPasswd should be both given")
		os.Exit(1)
	}
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
	// debug.Println("shadowsocks connection created to:", hostFull)
	return conn{c, ctShadowctSocksConn}, nil
}
