package main

import (
	"testing"
)

func TestHostHasPort(t *testing.T) {
	var testData = []struct {
		host    string
		hasPort bool
	}{
		{"google.com", false},
		{"google.com:80", true},
		{"google.com80", false},
	}

	for _, td := range testData {
		if hostHasPort(td.host) != td.hasPort {
			t.Errorf("%s should return %v", td.host, td.hasPort)
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
		{"http://google.com/ncr/tree", &URL{"google.com:80", "/ncr/tree"}},
		{"google.com:80/", &URL{"google.com:80", "/"}},
		{"google.com:80", &URL{"google.com:80", "/"}},
		{"google.com", &URL{"google.com:80", "/"}},
		{"google.com:80/ncr", &URL{"google.com:80", "/ncr"}},
		{"google.com/ncr/tree", &URL{"google.com:80", "/ncr/tree"}},
		{"/ncr/tree", nil},
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
		}
		if url != nil {
			if err != nil {
				t.Error(td.rawurl, "non nil URL should not report error")
			}
			if td.url == nil {
				t.Error(td.rawurl, "should report error")
			} else {
				if url.Host != td.url.Host {
					t.Error(td.rawurl, "parsed host wrong:", td.url.Host, "got", url.Host)
				}
				if url.Path != td.url.Path {
					t.Error(td.rawurl, "parsed path wrong:", td.url.Path, "got", url.Path)
				}
			}
		}
	}
}
