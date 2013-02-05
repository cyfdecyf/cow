package main

import (
	"io"
	"net"
	"time"
)

var dialTimeout, readTimeout time.Duration
var maxTimeout = time.Duration(20)

// use a fast to fetch web site
const estimateSite = "www.baidu.com"

var estimateReq = []byte("GET / HTTP/1.1\r\n" +
	"Host: " + estimateSite + "\r\n" +
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10.8; rv:11.0) Gecko/20100101 Firefox/11.0\r\n" +
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
		errl.Printf("estimateTimeout: can't connect to %s: %v, network has problem?\n",
			estimateSite, err)
		goto onErr
	}
	defer c.Close()

	est = time.Now().Sub(start) * 5
	debug.Println("estimated dialTimeout:", est)
	if est > maxTimeout {
		est = maxTimeout
	}
	if est > config.DialTimeout {
		dialTimeout = est
		info.Println("new dial timeout:", dialTimeout)
	} else if dialTimeout != config.DialTimeout {
		dialTimeout = config.DialTimeout
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
		errl.Printf("estimateTimeout: error getting %s: %v, network has problem?\n",
			estimateSite, err)
	}
	est = time.Now().Sub(start) * 5
	if est > maxTimeout {
		est = maxTimeout
	}
	debug.Println("estimated read timeout:", est)
	if est > time.Duration(config.ReadTimeout) {
		readTimeout = est
		info.Println("new read timeout:", readTimeout)
	} else if readTimeout != config.ReadTimeout {
		readTimeout = config.ReadTimeout
		info.Println("new read timeout:", readTimeout)
	}
	return
onErr:
	dialTimeout += 2
	readTimeout += 2
}

func runEstimateTimeout() {
	readTimeout = config.ReadTimeout
	dialTimeout = config.DialTimeout
	for {
		estimateTimeout()
		time.Sleep(time.Minute)
	}
}
