package main

import (
	"bytes"
	"fmt"
	"net"
	"strings"
	"text/template"
	"time"
)

var pac struct {
	template       *template.Template
	topLevelDomain string
	directList     string
}

func init() {
	const pacRawTmpl = `var direct = 'DIRECT';
var httpProxy = 'PROXY {{.ProxyAddr}}; DIRECT';

var directList = [
"",
"{{.DirectDomains}}"
];

var directAcc = {};
for (var i = 0; i < directList.length; i += 1) {
	directAcc[directList[i]] = true;
}

var topLevel = {
{{.TopLevel}}
};

// only handles IPv4 address now
function hostIsIP(host) {
	var parts = host.split('.');
	if (parts.length != 4) {
		return false;
	}
	for (var i = 3; i >= 0; i--) {
		if (parts[i].length === 0 || parts[i].length > 3) {
			return false;
		}
		var n = Number(parts[i]);
		if (isNaN(n) || n < 0 || n > 255) {
			return false;
		}
	}
	return true;
}

function host2Domain(host) {
	if (hostIsIP(host)) {
		return ""; // IP address has no domain
	}
	var lastDot = host.lastIndexOf('.');
	if (lastDot === -1) {
		return ""; // simple host name has no domain
	}
	// Find the second last dot
	dot2ndLast = host.lastIndexOf(".", lastDot-1);
	if (dot2ndLast === -1)
		return host;

	var part = host.substring(dot2ndLast+1, lastDot);
	if (topLevel[part]) {
		var dot3rdLast = host.lastIndexOf(".", dot2ndLast-1);
		if (dot3rdLast === -1) {
			return host;
		}
		return host.substring(dot3rdLast+1);
	}
	return host.substring(dot2ndLast+1);
}

function FindProxyForURL(url, host) {
	return (directAcc[host] || directAcc[host2Domain(host)]) ? direct : httpProxy;
}
`
	var err error
	pac.template, err = template.New("pac").Parse(pacRawTmpl)
	if err != nil {
		Fatal("Internal error on generating pac file template:", err)
	}

	var buf bytes.Buffer
	for k, _ := range topLevelDomain {
		buf.WriteString(fmt.Sprintf("\t\"%s\": true,\n", k))
	}
	pac.topLevelDomain = buf.String()[:buf.Len()-2] // remove the final comma
}

// No need for content-length as we are closing connection
var pacHeader = []byte("HTTP/1.1 200 OK\r\nServer: cow-proxy\r\n" +
	"Content-Type: application/x-ns-proxy-autoconfig\r\nConnection: close\r\n\r\n")

// Different client will have different proxy URL, so generate it upon each request.
func genPAC(c *clientConn) []byte {
	buf := new(bytes.Buffer)

	proxyAddr := c.proxy.addrInPAC
	if proxyAddr == "" {
		host, _ := splitHostPort(c.LocalAddr().String())
		proxyAddr = net.JoinHostPort(host, c.proxy.port)
	}

	if pac.directList == "" {
		// Empty direct domain list
		buf.Write(pacHeader)
		pacproxy := fmt.Sprintf("function FindProxyForURL(url, host) { return 'PROXY %s; DIRECT'; };",
			proxyAddr)
		buf.Write([]byte(pacproxy))
		return buf.Bytes()
	}

	data := struct {
		ProxyAddr     string
		DirectDomains string
		TopLevel      string
	}{
		proxyAddr,
		pac.directList,
		pac.topLevelDomain,
	}

	buf.Write(pacHeader)
	if err := pac.template.Execute(buf, data); err != nil {
		errl.Println("Error generating pac file:", err)
		panic("Error generating pac file")
	}
	return buf.Bytes()
}

func initPAC() {
	pac.directList = strings.Join(siteStat.GetDirectList(), "\",\n\"")
	go func() {
		for {
			time.Sleep(10 * time.Minute)
			pac.directList = strings.Join(siteStat.GetDirectList(), "\",\n\"")
		}
	}()
}

func sendPAC(c *clientConn) {
	if _, err := c.Write(genPAC(c)); err != nil {
		debug.Println("Error sending PAC file")
		return
	}
}
