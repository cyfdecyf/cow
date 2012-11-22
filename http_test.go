package main

import (
	"testing"
)

func TestSplitHostPort(t *testing.T) {
	var testData = []struct {
		host       string
		hostNoPort string
		port       string
	}{
		{"google.com", "google.com", ""},
		{"google.com:80", "google.com", "80"},
		{"google.com80", "google.com80", ""},
	}

	for _, td := range testData {
		h, p := splitHostPort(td.host)
		if h != td.hostNoPort || p != td.port {
			t.Errorf("%s returns %v %v", td.host, td.hostNoPort, td.port)
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
		{"https://google.com/ncr/tree", &URL{"google.com:80", "/ncr/tree", "http"}},
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
		if url.Host != td.url.Host {
			t.Error(td.rawurl, "parsed host wrong:", td.url.Host, "got", url.Host)
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
