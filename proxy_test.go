package main

import (
	"bytes"
	"github.com/cyfdecyf/bufio"
	"strings"
	"testing"
)

func TestSendBodyChunked(t *testing.T) {
	testData := []struct {
		raw  string
		want string // empty means same as raw
	}{
		{"1a; ignore-stuff-here\r\nabcdefghijklmnopqrstuvwxyz\r\n10\r\n1234567890abcdef\r\n0\r\n\r\n", ""},
		{"0\r\n\r\n", ""},
		/*
			{"0\n\r\n", "0\r\n\r\n"}, // test for buggy web servers
			{"1a; ignore-stuff-here\nabcdefghijklmnopqrstuvwxyz\r\n10\n1234567890abcdef\n0\n\n",
				// COW will only sanitize CRLF at chunk ending
				"1a; ignore-stuff-here\nabcdefghijklmnopqrstuvwxyz\r\n10\n1234567890abcdef\r\n0\r\n\r\n"},
		*/
	}

	// supress error log when finding chunk extension
	errl = false
	defer func() {
		errl = true
	}()
	// use different reader buffer size to test for both all buffered and partially buffered chunk
	sizeArr := []int{32, 64, 128}
	for _, size := range sizeArr {
		for _, td := range testData {
			r := bufio.NewReaderSize(strings.NewReader(td.raw), size)
			w := new(bytes.Buffer)

			if err := sendBodyChunked(w, r, size); err != nil {
				t.Fatalf("sent data %q err: %v\n", w.Bytes(), err)
			}
			if td.want == "" {
				if w.String() != td.raw {
					t.Errorf("sendBodyChunked wrong with buf size %d, raw data is:\n%q\ngot:\n%q\n",
						size, td.raw, w.String())
				}
			} else {
				if w.String() != td.want {
					t.Errorf("sendBodyChunked wrong with buf sizwe %d, raw data is:\n%q\nwant:\n%q\ngot :\n%q\n",
						size, td.raw, td.want, w.String())
				}
			}
		}
	}
}

func TestInitSelfListenAddr(t *testing.T) {
	listenProxy = []Proxy{newHttpProxy("0.0.0.0:7777", "")}
	initSelfListenAddr()

	testData := []struct {
		r    Request
		self bool
	}{
		{Request{Header: Header{Host: "google.com:443"}, URL: &URL{}}, false},
		{Request{Header: Header{Host: "localhost"}, URL: &URL{}}, true},
		{Request{Header: Header{Host: "127.0.0.1:7777"}, URL: &URL{}}, true},
		{Request{Header: Header{Host: ""}, URL: &URL{HostPort: "google.com"}}, false},
		{Request{Header: Header{Host: "localhost"}, URL: &URL{HostPort: "google.com"}}, false},
	}

	for _, td := range testData {
		if isSelfRequest(&td.r) != td.self {
			t.Error(td.r.Host, "isSelfRequest should be", td.self)
		}
		if td.self && td.r.URL.Host == "" {
			t.Error("isSelfRequest should set url host", td.r.Header.Host)
		}
	}

	// Another set of listen addr.
	listenProxy = []Proxy{
		newHttpProxy("192.168.1.1:7777", ""),
		newHttpProxy("127.0.0.1:8888", ""),
	}
	initSelfListenAddr()

	testData2 := []struct {
		r    Request
		self bool
	}{
		{Request{Header: Header{Host: "google.com:443"}, URL: &URL{}}, false},
		{Request{Header: Header{Host: "localhost"}, URL: &URL{}}, true},
		{Request{Header: Header{Host: "127.0.0.1:8888"}, URL: &URL{}}, true},
		{Request{Header: Header{Host: "192.168.1.1"}, URL: &URL{}}, true},
		{Request{Header: Header{Host: "192.168.1.2"}, URL: &URL{}}, false},
		{Request{Header: Header{Host: ""}, URL: &URL{HostPort: "google.com"}}, false},
		{Request{Header: Header{Host: "localhost"}, URL: &URL{HostPort: "google.com"}}, false},
	}

	for _, td := range testData2 {
		if isSelfRequest(&td.r) != td.self {
			t.Error(td.r.Host, "isSelfRequest should be", td.self)
		}
		if td.self && td.r.URL.Host == "" {
			t.Error("isSelfRequest should set url host", td.r.Header.Host)
		}
	}
}
