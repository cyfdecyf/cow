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
