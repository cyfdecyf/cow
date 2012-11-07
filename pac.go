package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"
	"text/template"
)

var pacRawTmpl = `var direct = 'DIRECT';
var httpProxy = 'PROXY {{.ProxyAddr}}; DIRECT';

var directList = [
"{{.DirectDomains}}"
];

var directAcc = {};
for (var i = 0; i < directList.length; i += 1) {
	directAcc[directList[i]] = true;
}

function host2domain(host) {
	var dotpos = host.lastIndexOf(".");
	if (dotpos === -1)
		return host;
	// Find the second last dot
	dotpos = host.lastIndexOf(".", dotpos - 1);
	if (dotpos === -1)
		return host;
	return host.substring(dotpos + 1);
};

function FindProxyForURL(url, host) {
	return directAcc[host2domain(host)] ? direct : httpProxy;
};
`

var pacCont string

func genPAC() {
	pacTmpl, err := template.New("pac").Parse(pacRawTmpl)
	if err != nil {
		fmt.Println("Internal error on generating pac file template")
		os.Exit(1)
	}

	data := struct {
		ProxyAddr     string
		DirectDomains string
	}{
		config.listenAddr,
		strings.Join(directDs.toArray(), "\",\n\""), // domains in PAC file needs double quote
	}

	// debug.Println("direct:", data.DirectDomains)

	buf := new(bytes.Buffer)
	if err := pacTmpl.Execute(buf, data); err != nil {
		errl.Println("Error generating pac file:", err)
		return
	}
	pacCont = buf.String()
	pacHeader := "HTTP/1.1 200 Okay\r\nServer: cow-proxy\r\nContent-Type: text/html\r\nConnection: close\r\n" +
		fmt.Sprintf("Content-Length: %d\r\n\r\n", len(pacCont))
	pacCont = pacHeader + pacCont
}

func sendPAC(w *bufio.Writer) {
	w.WriteString(pacCont)
	w.Flush()
}
