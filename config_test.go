package main

import (
	"testing"
)

func TestParseListen(t *testing.T) {
	parser := configParser{}
	parser.ParseListen("127.0.0.1:8888")
	if config.ListenAddr[0] != "127.0.0.1:8888" {
		t.Error("single listen address parse error")
	}

	config.ListenAddr = nil
	parser.ParseListen("127.0.0.1:8888, 127.0.0.1:7777")
	if len(config.ListenAddr) != 2 {
		t.Error("multiple listen address parse error")
	}
}

func TestTunnelAllowedPort(t *testing.T) {
	parser := configParser{}
	parser.ParseTunnelAllowedPort("1, 2, 3, 4, 5")
	parser.ParseTunnelAllowedPort("6")
	parser.ParseTunnelAllowedPort("7")
	parser.ParseTunnelAllowedPort("8")

	testData := []struct {
		port    string
		allowed bool
	}{
		{"80", true}, // default allowd ports
		{"443", true},
		{"1", true},
		{"3", true},
		{"5", true},
		{"7", true},
		{"8080", false},
		{"8388", false},
	}

	for _, td := range testData {
		allowed := config.TunnelAllowedPort[td.port]
		if allowed != td.allowed {
			t.Errorf("port %s allowed %v, got %v\n", td.port, td.allowed, allowed)
		}
	}
}
