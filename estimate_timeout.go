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
	"User-Agent: curl/7.24.0 (x86_64-apple-darwin12.0) libcurl/7.24.0 OpenSSL/0.9.8r zlib/1.2.5\r\n" +
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

	est = time.Now().Sub(start) * 5
	debug.Println("estimated dialTimeout:", est)
	if est > dialTimeout {
		dialTimeout = est
		info.Println("new dial timeout:", dialTimeout)
	} else if dialTimeout != minDialTimeout {
		dialTimeout = minDialTimeout
		info.Println("new dial timeout:", dialTimeout)
	}

	start = time.Now()
	// include time spent on sending request, reading all content to make it a
	// little longer
	c.Write(estimateReq)
	for err == nil {
		_, err = c.Read(buf)
	}
	if err != io.EOF {
		errl.Printf("estimateTimeout: error getting %s: %v, network has problem?",
			estimateSite, err)
	}
	est = time.Now().Sub(start) * 10
	debug.Println("estimated read timeout:", est)
	if est > readTimeout {
		readTimeout = est
		info.Println("new read timeout:", readTimeout)
	} else if readTimeout != minReadTimeout {
		readTimeout = minReadTimeout
		info.Println("new read timeout:", readTimeout)
	}
	return
onErr:
	dialTimeout += 2
	readTimeout += 2
}

func runEstimateTimeout() {
	for {
		estimateTimeout()
		time.Sleep(time.Minute)
	}
}
