package main

import (
	"errors"
	"net"
	"os"
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

func createSocksConnection(hostFull string) (c net.Conn, err error) {
	if c, err = net.Dial("tcp", "127.0.0.1:1087"); err != nil {
		errl.Printf("Can't connect to socks server %v\n", err)
		return nil, err
	}
	debug.Println("Connected to socks server")

	var n int
	if n, err = c.Write(socksMsgVerMethodSelection); n != 3 || err != nil {
		errl.Printf("sending ver/method selection msg %v n = %v\n", err, n)
		c.Close()
		return nil, err
	}

	// version/method selection
	repBuf := make([]byte, 2, 2)
	_, err = c.Read(repBuf)
	if err != nil {
		errl.Printf("read ver/method selection error %v\n", err)
		c.Close()
		return nil, err
	}
	if repBuf[0] != 5 || repBuf[1] != 0 {
		errl.Printf("socks ver/method selection reply error ver %d method %d",
			repBuf[0], repBuf[1])
		c.Close()
		return nil, socksProtocolErr
	}
	debug.Println("Socks version selection done")

	// send connect request
	host, portStr := splitHostPort(hostFull)
	port, err := strconv.Atoi(portStr)
	if err != nil {
		errl.Printf("Should not happen, port error %v\n", port)
		os.Exit(1)
	}

	hostLen := len(host)
	bufLen := 5 + hostLen + 2 // last 2 is port
	reqBuf := make([]byte, bufLen, bufLen)
	reqBuf[0] = 5 // version 5
	reqBuf[1] = 1 // cmd: connect
	// reqBuf[2] = 0 // rsv: set to 0 when initializing
	reqBuf[3] = 3 // atyp: domain name
	reqBuf[4] = byte(hostLen)
	copy(reqBuf[5:], host)
	reqBuf[5+hostLen] = byte(port >> 8 & 0xFF)
	reqBuf[5+hostLen+1] = byte(port) & 0xFF

	if debug {
		debug.Println("Send socks connect request", (host + ":" + portStr))
	}

	if n, err = c.Write(reqBuf); err != nil || n != bufLen {
		errl.Printf("Send socks request err %v n %d\n", err, n)
		return nil, err
	}

	// I'm not clear why the buffer is fixed at 10. The rfc document does not say this.
	// Polipo set this to 10 and I also observed the reply is always 10.
	replyBuf := make([]byte, 10, 10)
	if n, err = c.Read(replyBuf); err != nil {
		errl.Printf("Read socks reply err %v n %d\n", err, n)
		return nil, err
	}
	// debug.Printf("Socks reply length %d\n", n)

	if replyBuf[0] != 5 {
		errl.Printf("Socks reply connect %s VER %d not supported\n", hostFull, replyBuf[0])
		return nil, socksProtocolErr
	}
	if replyBuf[1] != 0 {
		errl.Printf("Socks reply connect %s error %d\n", hostFull, socksError[replyBuf[1]])
		return nil, socksProtocolErr
	}
	if replyBuf[3] != 1 {
		errl.Printf("Socks reply connect %s ATYP %d\n", hostFull, replyBuf[3])
		return nil, socksProtocolErr
	}

	// Now the socket can be used to pass data.

	return
}
