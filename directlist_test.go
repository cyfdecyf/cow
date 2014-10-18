package main

import (
	"testing"
)

func TestShouldDirect(t *testing.T) {
	directList := newDirectList()

	directList.Domain["com.cn"] = domainTypeDirect
	directList.Domain["edu.cn"] = domainTypeDirect
	directList.Domain["baidu.com"] = domainTypeDirect

	g, _ := ParseRequestURI("gtemp.com")
	if directList.shouldDirect(g) {
		t.Error("never visited site should be considered using proxy")
	}

	directDomains := []string{
		"baidu.com",
		"www.baidu.com",
		"www.ahut.edu.cn",
	}
	for _, domain := range directDomains {
		url, _ := ParseRequestURI(domain)
		if !directList.shouldDirect(url) {
			t.Errorf("domain %s in direct list should be considered using direct, host: %s", domain, url.Host)
		}
	}

}
