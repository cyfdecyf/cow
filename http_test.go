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
		{"http://google.com", &URL{"google.com:80", "/"}},
		{"http://google.com/", &URL{"google.com:80", "/"}},
		{"https://google.com:80", &URL{"google.com:80", "/"}},
		{"http://google.com:80/", &URL{"google.com:80", "/"}},
		{"http://google.com:80/ncr", &URL{"google.com:80", "/ncr"}},
		{"https://google.com/ncr/tree", &URL{"google.com:80", "/ncr/tree"}},
		{"google.com:80/", &URL{"google.com:80", "/"}},
		{"google.com:80", &URL{"google.com:80", "/"}},
		{"google.com", &URL{"google.com:80", "/"}},
		{"google.com:80/ncr", &URL{"google.com:80", "/ncr"}},
		{"google.com/ncr/tree", &URL{"google.com:80", "/ncr/tree"}},
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
