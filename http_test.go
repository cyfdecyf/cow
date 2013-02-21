package main

import (
	"github.com/cyfdecyf/bufio"
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestSplitHostPort(t *testing.T) {
	var testData = []struct {
		hostPort   string
		hostNoPort string
		port       string
	}{
		{"google.com", "google.com", ""},
		{"google.com:80", "google.com", "80"},
		{"google.com80", "google.com80", ""},
		{":7777", "", "7777"},
	}

	for _, td := range testData {
		h, p := splitHostPort(td.hostPort)
		if h != td.hostNoPort || p != td.port {
			t.Errorf("%s returns %v:%v", td.hostPort, td.hostNoPort, td.port)
		}
	}
}

func TestParseRequestURI(t *testing.T) {
	var testData = []struct {
		rawurl string
		url    *URL
	}{
		// I'm really tired of typing google.com ...
		{"http://www.g.com", &URL{"www.g.com:80", "www.g.com", "80", "g.com", ""}},
		{"http://plus.g.com/", &URL{"plus.g.com:80", "plus.g.com", "80", "g.com", "/"}},
		{"https://g.com:80", &URL{"g.com:80", "g.com", "80", "g.com", ""}},
		{"http://mail.g.com:80/", &URL{"mail.g.com:80", "mail.g.com", "80", "g.com", "/"}},
		{"http://g.com:80/ncr", &URL{"g.com:80", "g.com", "80", "g.com", "/ncr"}},
		{"https://g.com/ncr/tree", &URL{"g.com:443", "g.com", "443", "g.com", "/ncr/tree"}},
		{"www.g.com.hk:80/", &URL{"www.g.com.hk:80", "www.g.com.hk", "80", "g.com.hk", "/"}},
		{"g.com.jp:80", &URL{"g.com.jp:80", "g.com.jp", "80", "g.com.jp", ""}},
		{"g.com", &URL{"g.com:80", "g.com", "80", "g.com", ""}},
		{"g.com:8000/ncr", &URL{"g.com:8000", "g.com", "8000", "g.com", "/ncr"}},
		{"g.com/ncr/tree", &URL{"g.com:80", "g.com", "80", "g.com", "/ncr/tree"}},
		{"simplehost", &URL{"simplehost:80", "simplehost", "80", "", ""}},
		{"simplehost:8080", &URL{"simplehost:8080", "simplehost", "8080", "", ""}},
		{"192.168.1.1:8080/", &URL{"192.168.1.1:8080", "192.168.1.1", "8080", "", "/"}},
	}
	for _, td := range testData {
		url, err := ParseRequestURI(td.rawurl)
		if url == nil {
			if err == nil {
				t.Error("nil URL must report error")
			}
			if td.url != nil {
				t.Error(td.rawurl, "should not report error")
			}
			continue
		}
		if err != nil {
			t.Error(td.rawurl, "non nil URL should not report error")
		}
		if url.HostPort != td.url.HostPort {
			t.Error(td.rawurl, "parsed hostPort wrong:", td.url.HostPort, "got", url.HostPort)
		}
		if url.Host != td.url.Host {
			t.Error(td.rawurl, "parsed host wrong:", td.url.Host, "got", url.Host)
		}
		if url.Port != td.url.Port {
			t.Error(td.rawurl, "parsed port wrong:", td.url.Port, "got", url.Port)
		}
		if url.Domain != td.url.Domain {
			t.Error(td.rawurl, "parsed domain wrong:", td.url.Domain, "got", url.Domain)
		}
		if url.Path != td.url.Path {
			t.Error(td.rawurl, "parsed path wrong:", td.url.Path, "got", url.Path)
		}
	}
}

func TestParseHeader(t *testing.T) {
	var testData = []struct {
		raw    string
		newraw string
		header *Header
	}{
		{"content-length: 64\r\nConnection: keep-alive\r\n\r\n",
			"content-length: 64\r\nConnection: keep-alive\r\n",
			&Header{ContLen: 64, Chunking: false, ConnectionKeepAlive: true}},
		{"Connection: keep-alive\r\nKeep-Alive: timeout=10\r\nTransfer-Encoding: chunked\r\nTE: trailers\r\n\r\n",
			"Connection: keep-alive\r\nTransfer-Encoding: chunked\r\n",
			&Header{ContLen: -1, Chunking: true, ConnectionKeepAlive: true,
				KeepAlive: 10 * time.Second}},
		/*
			{"Connection: keep-alive\r\nKeep-Alive: max=5,\r\n timeout=10\r\n\r\n", // test multi-line header
				"Connection: keep-alive\r\n",
				&Header{ContLen: -1, Chunking: false, ConnectionKeepAlive: true,
					KeepAlive: 10 * time.Second}},
			{"Connection: \r\n keep-alive\r\n\r\n", // test multi-line header
				"Connection: keep-alive\r\n",
				&Header{ContLen: -1, Chunking: false, ConnectionKeepAlive: true}},
		*/
	}
	for _, td := range testData {
		var h Header
		var newraw bytes.Buffer
		h.parseHeader(bufio.NewReader(strings.NewReader(td.raw)), &newraw, nil)
		if h.ContLen != td.header.ContLen {
			t.Errorf("%q parsed content length wrong, should be %d, get %d\n",
				td.raw, td.header.ContLen, h.ContLen)
		}
		if h.Chunking != td.header.Chunking {
			t.Errorf("%q parsed chunking wrong, should be %t, get %t\n",
				td.raw, td.header.Chunking, h.Chunking)
		}
		if h.ConnectionKeepAlive != td.header.ConnectionKeepAlive {
			t.Errorf("%q parsed connection wrong, should be %v, get %v\n",
				td.raw, td.header.ConnectionKeepAlive, h.ConnectionKeepAlive)
		}
		if h.KeepAlive != td.header.KeepAlive {
			t.Errorf("%q parsed keep alive wrong, should be %v, get %v\n",
				td.raw, td.header.KeepAlive, h.KeepAlive)
		}
		if newraw.String() != td.newraw {
			t.Errorf("%q parsed raw wrong\nshould be: %q\ngot: %q\n",
				td.raw, td.newraw, newraw.Bytes())
		}
	}
}
