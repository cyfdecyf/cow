package main

import (
	"testing"
)

func Testjudge(t *testing.T) {
	domainList := newDomainList()

	domainList.Domain["com.cn"] = domainTypeDirect
	domainList.Domain["edu.cn"] = domainTypeDirect
	domainList.Domain["baidu.com"] = domainTypeDirect

	g, _ := ParseRequestURI("gtemp.com")
	if domainList.judge(g) == domainTypeProxy {
		t.Error("never visited site should be considered using proxy")
	}

	directDomains := []string{
		"baidu.com",
		"www.baidu.com",
		"www.ahut.edu.cn",
	}
	for _, domain := range directDomains {
		url, _ := ParseRequestURI(domain)
		if domainList.judge(url) == domainTypeDirect {
			t.Errorf("domain %s in direct list should be considered using direct, host: %s", domain, url.Host)
		}
	}

}
