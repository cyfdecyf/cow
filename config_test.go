package main

import (
	"testing"
)

func TestParseListen(t *testing.T) {
	parser := configParser{}
	parser.ParseListen("http://127.0.0.1:8888")

	hp, ok := listenProxy[0].(*httpProxy)
	if !ok {
		t.Error("listen http proxy type wrong")
	}
	if hp.addr != "127.0.0.1:8888" {
		t.Error("listen http server address parse error")
	}

	parser.ParseListen("http://127.0.0.1:8888 1.2.3.4:5678")
	hp, ok = listenProxy[1].(*httpProxy)
	if hp.addrInPAC != "1.2.3.4:5678" {
		t.Error("listen http addrInPAC parse error")
	}
}

func TestParseProxy(t *testing.T) {
	pool, ok := parentProxy.(*backupParentPool)
	if !ok {
		t.Fatal("parentPool by default should be backup pool")
	}
	cnt := -1

	var parser configParser
	parser.ParseProxy("http://127.0.0.1:8080")
	cnt++

	hp, ok := pool.parent[cnt].ParentProxy.(*httpParent)
	if !ok {
		t.Fatal("1st http proxy parsed not as httpParent")
	}
	if hp.server != "127.0.0.1:8080" {
		t.Error("1st http proxy server address wrong, got:", hp.server)
	}

	parser.ParseProxy("http://user:passwd@127.0.0.2:9090")
	cnt++
	hp, ok = pool.parent[cnt].ParentProxy.(*httpParent)
	if !ok {
		t.Fatal("2nd http proxy parsed not as httpParent")
	}
	if hp.server != "127.0.0.2:9090" {
		t.Error("2nd http proxy server address wrong, got:", hp.server)
	}
	if hp.authHeader == nil {
		t.Error("2nd http proxy server user password not parsed")
	}

	parser.ParseProxy("socks5://127.0.0.1:1080")
	cnt++
	sp, ok := pool.parent[cnt].ParentProxy.(*socksParent)
	if !ok {
		t.Fatal("socks proxy parsed not as socksParent")
	}
	if sp.server != "127.0.0.1:1080" {
		t.Error("socks server address wrong, got:", sp.server)
	}

	parser.ParseProxy("ss://aes-256-cfb:foobar!@127.0.0.1:1080")
	cnt++
	_, ok = pool.parent[cnt].ParentProxy.(*shadowsocksParent)
	if !ok {
		t.Fatal("shadowsocks proxy parsed not as shadowsocksParent")
	}
}
