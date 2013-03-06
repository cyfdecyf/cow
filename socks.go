package main

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
)

// For socks documentation, refer to rfc 1928 http://www.ietf.org/rfc/rfc1928.txt

var socksError = [...]string{
	1: "General SOCKS server failure",
	2: "Connection not allowed by ruleset",
	3: "Network unreachable",
	4: "Host unreachable",
	5: "Connection refused",
	6: "TTL expired",
	7: "Command not supported",
	8: "Address type not supported",
	9: "to X'FF' unassigned",
}

var socksProtocolErr = errors.New("socks protocol error")

var socksMsgVerMethodSelection = []byte{
	0x5, // version 5
	1,   // n method
	0,   // no authorization required
}

func initSocksServer() {
	if config.SocksParent != "" {
		debug.Println("has socks server:", config.SocksParent)
	}
}

func createctSocksConnection(url *URL) (cn conn, err error) {
	c, err := net.Dial("tcp", config.SocksParent)
	if err != nil {
		debug.Printf("Can't connect to socks server %v\n", err)
		return
	}
	hasErr := false
	defer func() {
		if hasErr {
			c.Close()
		}
	}()
	// debug.Println("Connected to socks server")

	var n int
	if n, err = c.Write(socksMsgVerMethodSelection); n != 3 || err != nil {
		errl.Printf("sending ver/method selection msg %v n = %v\n", err, n)
		hasErr = true
		return
	}

	// version/method selection
	repBuf := make([]byte, 2)
	_, err = c.Read(repBuf)
	if err != nil {
		errl.Printf("read ver/method selection error %v\n", err)
		hasErr = true
		return
	}
	if repBuf[0] != 5 || repBuf[1] != 0 {
		errl.Printf("socks ver/method selection reply error ver %d method %d",
			repBuf[0], repBuf[1])
		hasErr = true
		return
	}
	// debug.Println("Socks version selection done")

	// send connect request
	host := url.Host
	port, err := strconv.Atoi(url.Port)
	if err != nil {
		errl.Printf("Should not happen, port error %v\n", port)
		hasErr = true
		return
	}

	hostLen := len(host)
	bufLen := 5 + hostLen + 2 // last 2 is port
	reqBuf := make([]byte, bufLen)
	reqBuf[0] = 5 // version 5
	reqBuf[1] = 1 // cmd: connect
	// reqBuf[2] = 0 // rsv: set to 0 when initializing
	reqBuf[3] = 3 // atyp: domain name
	reqBuf[4] = byte(hostLen)
	copy(reqBuf[5:], host)
	binary.BigEndian.PutUint16(reqBuf[5+hostLen:5+hostLen+2], uint16(port))

	/*
		if debug {
			debug.Println("Send socks connect request", (host + ":" + portStr))
		}
	*/

	if n, err = c.Write(reqBuf); err != nil || n != bufLen {
		errl.Printf("Send socks request err %v n %d\n", err, n)
		hasErr = true
		return
	}

	// I'm not clear why the buffer is fixed at 10. The rfc document does not say this.
	// Polipo set this to 10 and I also observed the reply is always 10.
	replyBuf := make([]byte, 10)
	if n, err = c.Read(replyBuf); err != nil {
		// Seems that socks server will close connection if it can't find host
		if err != io.EOF {
			errl.Printf("Read socks reply err %v n %d\n", err, n)
		}
		hasErr = true
		return zeroConn, errors.New("Connection failed (by socks server). No such host?")
	}
	// debug.Printf("Socks reply length %d\n", n)

	if replyBuf[0] != 5 {
		errl.Printf("Socks reply connect %s VER %d not supported\n", url.HostPort, replyBuf[0])
		hasErr = true
		return zeroConn, socksProtocolErr
	}
	if replyBuf[1] != 0 {
		errl.Printf("Socks reply connect %s error %d\n", url.HostPort, socksError[replyBuf[1]])
		hasErr = true
		return zeroConn, socksProtocolErr
	}
	if replyBuf[3] != 1 {
		errl.Printf("Socks reply connect %s ATYP %d\n", url.HostPort, replyBuf[3])
		hasErr = true
		return zeroConn, socksProtocolErr
	}

	// Now the socket can be used to pass data.
	return conn{c, ctSocksConn}, nil
}
