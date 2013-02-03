package main

import (
	"testing"
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
		{"http://www.g.com", &URL{"www.g.com:80", "www.g.com", "g.com", "/", "http"}},
		{"http://plus.g.com/", &URL{"plus.g.com:80", "plus.g.com", "g.com", "/", "http"}},
		{"https://g.com:80", &URL{"g.com:80", "g.com", "g.com", "/", "http"}},
		{"http://mail.g.com:80/", &URL{"mail.g.com:80", "mail.g.com", "g.com", "/", "http"}},
		{"http://g.com:80/ncr", &URL{"g.com:80", "g.com", "g.com", "/ncr", "http"}},
		{"https://g.com/ncr/tree", &URL{"g.com:443", "g.com", "g.com", "/ncr/tree", "http"}},
		{"www.g.com.hk:80/", &URL{"www.g.com.hk:80", "www.g.com.hk", "g.com.hk", "/", "http"}},
		{"g.com.jp:80", &URL{"g.com.jp:80", "g.com.jp", "g.com.jp", "/", "http"}},
		{"g.com", &URL{"g.com:80", "g.com", "g.com", "/", "http"}},
		{"g.com:80/ncr", &URL{"g.com:80", "g.com", "g.com", "/ncr", "http"}},
		{"g.com/ncr/tree", &URL{"g.com:80", "g.com", "g.com", "/ncr/tree", "http"}},
		{"simplehost", &URL{"simplehost:80", "simplehost", "", "/", "http"}},
		{"simplehost:8080", &URL{"simplehost:8080", "simplehost", "", "/", "http"}},
		{"192.168.1.1:8080", &URL{"192.168.1.1:8080", "192.168.1.1", "", "/", "http"}},
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
		if url.Domain != td.url.Domain {
			t.Error(td.rawurl, "parsed domain wrong:", td.url.Domain, "got", url.Domain)
		}
		if url.Path != td.url.Path {
			t.Error(td.rawurl, "parsed path wrong:", td.url.Path, "got", url.Path)
		}
	}
}

func TestURLToURI(t *testing.T) {
	var testData = []struct {
		url URL
		uri string
	}{
		{URL{HostPort: "google.com", Path: "/ncr", Scheme: "http"}, "http://google.com/ncr"},
		{URL{HostPort: "www.google.com", Path: "/ncr", Scheme: "https"}, "https://www.google.com/ncr"},
	}
	for _, td := range testData {
		if td.url.toURI() != td.uri {
			t.Error("URL", td.url.String(), "toURI got", td.url.toURI(), "should be", td.uri)
		}
	}
}

func TestParseKeyValueList(t *testing.T) {
	var testData = []struct {
		str string
		kvm map[string]string
	}{
		{"\tk1=\"v1\", k2=\"v2\",  \tk3=\"v3\"", map[string]string{"k1": "v1", "k2": "v2", "k3": "v3"}},
		{"k1=\"v1\"", map[string]string{"k1": "v1"}},
		{"", map[string]string{}},
	}
	for _, td := range testData {
		kvm := parseKeyValueList(td.str)
		if len(kvm) != len(td.kvm) {
			t.Error("key value list parse error, element count not equal:", td.str, td.kvm)
		}
		for k, v := range td.kvm {
			if kvm[k] != v {
				t.Error("key value list parse error:", td.str, "for element:", k, v, "got:", kvm[k])
			}
		}
	}
}
