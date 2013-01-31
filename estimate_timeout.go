package main

import (
	"io"
	"net"
	"time"
)

// use a fast to fetch web site
const estimateSite = "www.baidu.com"

var estimateReq = []byte("GET / HTTP/1.1\r\n" +
	"Host: " + estimateSite + "\r\n" +
	"User-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10.8; rv:11.0) Gecko/20100101 Firefox/11.0\r\n" +
	"Accept: */*\r\n" +
	"Accept-Language: en-us,en;q=0.5\r\n" +
	"Accept-Encoding: gzip, deflate\r\n" +
	"Referer: http://www.baidu.com/\r\n" +
	"Connection: close\r\n\r\n")

// estimateTimeout tries to fetch a url and adjust timeout value according to
// how much time is spent on connect and fetch. This avoids incorrectly
// considering non-blocked sites as blocked when network connection is bad.
func estimateTimeout() {
	debug.Println("estimating timeout")
	buf := make([]byte, 4096)
	var est time.Duration

	start := time.Now()
	c, err := net.Dial("tcp", estimateSite+":80")
	if err != nil {
		errl.Println("estimateTimeout: can't connect to %s, network has problem?",
			estimateSite)
		goto onErr
	}
	defer c.Close()

	est = time.Now().Sub(start) * 2
	if est > dialTimeout {
		dialTimeout = est
		info.Println("new dial timeout:", dialTimeout)
	} else {
		dialTimeout = minDialTimeout
	}

	c.Write(estimateReq)
	start = time.Now()
	// read all content to make the time spent a little longer
	for err == nil {
		_, err = c.Read(buf)
	}
	if err != io.EOF {
		errl.Printf("estimateTimeout: error getting %s: %v, network has problem?",
			estimateSite, err)
	}
	est = time.Now().Sub(start) * 2
	if est > readTimeout {
		readTimeout = est
		info.Println("new read timeout:", readTimeout)
	} else {
		readTimeout = minReadTimeout
	}
	return
onErr:
	dialTimeout += 2
	readTimeout += 2
}

func runEstimateTimeout() {
	estimateTimeout()
	for {
		select {
		case <-time.After(30 * time.Second):
			estimateTimeout()
		}
	}
}
