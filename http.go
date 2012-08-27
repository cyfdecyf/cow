package main

import (
	"net/url"
	"strings"
)

type Request struct {
	Method string
	URL    *url.URL
	Proto  string

	keepAlive bool
	header    []string
	body      []byte
}

// If an http response may have message body
func responseMayHaveBody(method, status string) bool {
	// when we have tenary search tree, can optimize this a little
	return !(method == "HEAD" || status == "304" || status == "204" || strings.HasPrefix(status, "1"))
}
