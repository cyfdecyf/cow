package main

import (
	"testing"
)

func TestHost2Domain(t *testing.T) {
	var testData = []struct {
		host   string
		domain string
	}{
		{"www.google.com", "google.com"},
		{"google.com", "google.com"},
		{"com.cn", "com.cn"},
		{"sina.com.cn", "sina.com.cn"},
		{"www.bbc.co.uk", "bbc.co.uk"},
	}

	for _, td := range testData {
		dm := host2Domain(td.host)
		if dm != td.domain {
			t.Errorf("%s got domain %v should be %v", td.host, dm, td.domain)
		}
	}
}
