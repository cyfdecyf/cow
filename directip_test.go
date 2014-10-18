package main

import (
	"net"
	"testing"
)

func TestIPShouldDirect(t *testing.T) {

	directIPDomains := []string{
		"ohrz.net",
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
