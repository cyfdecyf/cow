package main

import (
	"errors"
	ss "github.com/shadowsocks/shadowsocks-go/shadowsocks"
)

var noShadowSocksErr = errors.New("No shadowsocks configuration")

var cipher []ss.Cipher

func initShadowSocks() {
	// error checking is done when parsing config
	if len(config.ShadowSocks) == 0 {
		return
	}
	for i, _ := range config.ShadowSocks {
		// initialize cipher for each shadowsocks connection
		if c, err := ss.NewCipher(config.ShadowMethod[i], config.ShadowPasswd[i]); err != nil {
			Fatal("creating shadowsocks cipher:", err)
		} else {
			cipher = append(cipher, c)
		}
		if debug {
			if config.ShadowMethod[i] != "" {
				debug.Println("shadowsocks server:", config.ShadowSocks[i], "encryption:", config.ShadowMethod[i])
			} else {
				debug.Println("shadowsocks server:", config.ShadowSocks[i])
			}
		}
	}
}

// Create shadowsocks connection function which uses the ith shadowsocks server
func createShadowSocksConnecter(i int) parentProxyConnectionFunc {
	f := func(url *URL) (cn conn, err error) {
		c, err := ss.Dial(url.HostPort, config.ShadowSocks[i], cipher[i].Copy())
		if err != nil {
			errl.Printf("Can't create shadowsocks connection for: %s %v\n", url.HostPort, err)
			return zeroConn, err
		}
		debug.Println("shadowsocks connection created to:", url.HostPort)
		return conn{c, ctShadowctSocksConn}, nil
	}
	return f
}
