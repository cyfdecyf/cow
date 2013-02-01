package main

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"strings"
	"text/template"
)

var pac struct {
	template       *template.Template
	topLevelDomain string
}

func init() {
	const pacRawTmpl = `var direct = 'DIRECT';
var httpProxy = 'PROXY {{.ProxyAddr}}; DIRECT';

var directList = [
"",
"0.1"{{.DirectDomains}}
];

var directAcc = {};
for (var i = 0; i < directList.length; i += 1) {
	directAcc[directList[i]] = true;
}

var topLevel = {
{{.TopLevel}}
};

function host2domain(host) {
	var lastDot = host.lastIndexOf(".");
	if (lastDot === -1)
		return ""; // simple host name has no domain
	// Find the second last dot
	dot2ndLast = host.lastIndexOf(".", lastDot-1);
	if (dot2ndLast === -1)
		return host;

	var part = host.substring(dot2ndLast+1, lastDot)
	if (topLevel[part]) {
		var dot3rdLast = host.lastIndexOf(".", dot2ndLast-1)
		if (dot3rdLast === -1) {
			return host
		}
		return host.substring(dot3rdLast+1)
	}
	return host.substring(dot2ndLast+1);
};

function FindProxyForURL(url, host) {
	return (directAcc[host] || directAcc[host2domain(host)]) ? direct : httpProxy;
};
`
	var err error
	pac.template, err = template.New("pac").Parse(pacRawTmpl)
	if err != nil {
		fmt.Println("Internal error on generating pac file template:", err)
		os.Exit(1)
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

func sendPAC(c *clientConn) {
	// domains in PAC file needs double quote
	ds1 := strings.Join(domainSet.alwaysDirect.toSlice(), "\",\n\"")
	ds2 := strings.Join(domainSet.direct.toSlice(), "\",\n\"")
	var ds string
	if ds1 == "" {
		ds = ds2
	} else if ds2 == "" {
		ds = ds1
	} else {
		ds = ds1 + "\",\n\"" + ds2
	}

	host, _ := splitHostPort(c.LocalAddr().String())
	_, port := splitHostPort(c.proxy.addr)
	proxyAddr := net.JoinHostPort(host, port)

	if ds == "" {
		// Empty direct domain list
		c.Write(pacHeader)
		pacproxy := fmt.Sprintf("function FindProxyForURL(url, host) { return 'PROXY %s; DIRECT'; };",
			proxyAddr)
		c.Write([]byte(pacproxy))
		return
	}

	data := struct {
		ProxyAddr     string
		DirectDomains string
		TopLevel      string
	}{
		proxyAddr,
		",\n\"" + ds + "\"",
		pac.topLevelDomain,
	}

	if _, err := c.Write(pacHeader); err != nil {
		debug.Println("Error writing pac header")
		return
	}
	// debug.Println("direct:", data.DirectDomains)
	buf := new(bytes.Buffer)
	if err := pac.template.Execute(buf, data); err != nil {
		errl.Println("Error generating pac file:", err)
	}
	if _, err := c.Write(buf.Bytes()); err != nil {
		debug.Println("Error writing pac content:", err)
	}
}
