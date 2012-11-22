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
localhost,
0.1,
"{{.DirectDomains}}"
];

var directAcc = {};
for (var i = 0; i < directList.length; i += 1) {
	directAcc[directList[i]] = true;
}

var topLevel = {
	"co":  true,
	"org": true,
	"com": true,
	"net": true,
	"edu": true
};

function host2domain(host) {
	var lastDot = host.lastIndexOf(".");
	if (lastDot === -1)
		return host;
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
	return directAcc[host2domain(host)] ? direct : httpProxy;
};
`

var pacTmpl *template.Template

func init() {
	var err error
	pacTmpl, err = template.New("pac").Parse(pacRawTmpl)
	if err != nil {
		fmt.Println("Internal error on generating pac file template")
		os.Exit(1)
	}
}

func genPAC() string {
	// domains in PAC file needs double quote
	ds1 := strings.Join(alwaysDirectDs.toArray(), "\",\n\"")
	ds2 := strings.Join(directDs.toArray(), "\",\n\"")
	var ds string
	if ds1 == "" {
		ds = ds2
	} else if ds2 == "" {
		ds = ds1
	} else {
		ds = ds1 + "\",\n\"" + ds2
	}
	data := struct {
		ProxyAddr     string
		DirectDomains string
	}{
		config.listenAddr,
		ds,
	}

	// debug.Println("direct:", data.DirectDomains)

	buf := new(bytes.Buffer)
	if err := pacTmpl.Execute(buf, data); err != nil {
		errl.Println("Error generating pac file:", err)
		os.Exit(1)
	}
	pac := buf.String()
	pacHeader := "HTTP/1.1 200 Okay\r\nServer: cow-proxy\r\nContent-Type: text/html\r\nConnection: close\r\n" +
		fmt.Sprintf("Content-Length: %d\r\n\r\n", len(pac))
	pac = pacHeader + pac
	return pac
}

func sendPAC(w *bufio.Writer) {
	w.WriteString(genPAC())
	w.Flush()
}
