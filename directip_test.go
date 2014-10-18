package main

import (
	"net"
	"testing"
)

func TestIPShouldDirect(t *testing.T) {

	initCNIPData()

	blockedIPDomains := []string{
		"youtube.com",
		"twitter.com",
	}
	for _, domain := range blockedIPDomains {
		hostIPs, err := net.LookupIP(domain)

		if err != nil {
			continue
		}

		var ip string
		ip = hostIPs[0].String()

		if ipShouldDirect(ip) {
			t.Errorf("ip %s should be considered using proxy, domain: %s", ip, domain)
		}
	}

	directIPDomains := []string{
		"ohrz.net",
		"www.ahut.edu.cn",
	}
	for _, domain := range directIPDomains {
		hostIPs, err := net.LookupIP(domain)

		if err != nil {
			continue
		}

		var ip string
		ip = hostIPs[0].String()

		if !ipShouldDirect(ip) {
			t.Errorf("ip %s should be considered using direct, domain: %s", ip, domain)
		}
	}

}
