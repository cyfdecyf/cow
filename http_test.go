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
		{"http://google.com", &URL{"google.com:80", "/", "http"}},
		{"http://google.com/", &URL{"google.com:80", "/", "http"}},
		{"https://google.com:80", &URL{"google.com:80", "/", "http"}},
		{"http://google.com:80/", &URL{"google.com:80", "/", "http"}},
		{"http://google.com:80/ncr", &URL{"google.com:80", "/ncr", "http"}},
		{"https://google.com/ncr/tree", &URL{"google.com:443", "/ncr/tree", "http"}},
		{"google.com:80/", &URL{"google.com:80", "/", "http"}},
		{"google.com:80", &URL{"google.com:80", "/", "http"}},
		{"google.com", &URL{"google.com:80", "/", "http"}},
		{"google.com:80/ncr", &URL{"google.com:80", "/ncr", "http"}},
		{"google.com/ncr/tree", &URL{"google.com:80", "/ncr/tree", "http"}},
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
			t.Error(td.rawurl, "parsed host wrong:", td.url.HostPort, "got", url.HostPort)
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
		{URL{"google.com", "/ncr", "http"}, "http://google.com/ncr"},
		{URL{"www.google.com", "/ncr", "https"}, "https://www.google.com/ncr"},
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
