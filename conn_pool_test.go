package main

import (
	"testing"
	"time"
)

func TestGetFromEmptyPool(t *testing.T) {
	// should not block
	sv := connPool.Get("foo", true)
	if sv != nil {
		t.Error("get non nil server conn from empty conn pool")
	}
}

func TestConnPool(t *testing.T) {
	closeOn := time.Now().Add(10 * time.Second)
	conns := []*serverConn{
		{hostPort: "example.com:80", willCloseOn: closeOn},
		{hostPort: "example.com:80", willCloseOn: closeOn},
		{hostPort: "example.com:80", willCloseOn: closeOn},
		{hostPort: "example.com:443", willCloseOn: closeOn},
		{hostPort: "google.com:443", willCloseOn: closeOn},
		{hostPort: "google.com:443", willCloseOn: closeOn},
		{hostPort: "www.google.com:80", willCloseOn: closeOn},
	}
	for _, sv := range conns {
		connPool.Put(sv)
	}

	testData := []struct {
		hostPort string
		found    bool
	}{
		{"example.com", false},
		{"example.com:80", true},
		{"example.com:80", true},
		{"example.com:80", true},
		{"example.com:80", false}, // has 3 such conn
		{"www.google.com:80", true},
	}

	for _, td := range testData {
		sv := connPool.Get(td.hostPort, true)
		if td.found {
			if sv == nil {
				t.Error("should find conn for", td.hostPort)
			} else if sv.hostPort != td.hostPort {
				t.Errorf("hostPort should be: %s, got: %s\n", td.hostPort, sv.hostPort)
			}
		} else if sv != nil {
			t.Errorf("should NOT find conn for %s, got conn for: %s\n",
				td.hostPort, sv.hostPort)
		}
	}
}
