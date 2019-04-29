package main

import (
	"bytes"
	"io"
	"os"
	"text/template"
	"time"
)

// Do not end with "\r\n" so we can add more header later
var headRawTmpl = "HTTP/1.1 {{.CodeReason}}\r\n" +
	"Connection: keep-alive\r\n" +
	"Cache-Control: no-cache\r\n" +
	"Pragma: no-cache\r\n" +
	"Content-Type: text/html;charset=utf-8\r\n" +
	"Content-Length: {{.Length}}\r\n"

var errPageTmpl, headTmpl *template.Template

func init() {
	hostName, err := os.Hostname()
	if err != nil {
		hostName = "unknown host"
	}

	errPageRawTmpl := `<!DOCTYPE html>
<html>
	<head> <title>COW Proxy</title> </head>
	<body>
		<h1>{{.H1}}</h1>
		{{.Msg}}
		<hr />
		<i>你电脑经过了代理, 如果是非预期记得关闭代理(系统, 终端等)设置</i> <br />
		Host <i>` + hostName + `</i> <br />
		{{.T}}
	</body>
</html>
`
	if headTmpl, err = template.New("errorHead").Parse(headRawTmpl); err != nil {
		Fatal("Internal error on generating error head template")
	}
	if errPageTmpl, err = template.New("errorPage").Parse(errPageRawTmpl); err != nil {
		Fatalf("Internal error on generating error page template")
	}
}

func genErrorPage(h1, msg string) (string, error) {
	var err error
	data := struct {
		H1  string
		Msg string
		T   string
	}{
		h1,
		msg,
		time.Now().Format(time.ANSIC),
	}

	buf := new(bytes.Buffer)
	err = errPageTmpl.Execute(buf, data)
	return buf.String(), err
}

func sendPageGeneric(w io.Writer, codeReason, h1, msg string) {
	page, err := genErrorPage(h1, msg)
	if err != nil {
		errl.Println("Error generating error page:", err)
		return
	}

	data := struct {
		CodeReason string
		Length     int
	}{
		codeReason,
		len(page),
	}
	buf := new(bytes.Buffer)
	if err := headTmpl.Execute(buf, data); err != nil {
		errl.Println("Error generating error page header:", err)
		return
	}

	buf.WriteString("\r\n")
	buf.WriteString(page)
	w.Write(buf.Bytes())
}

func sendErrorPage(w io.Writer, codeReason, h1, msg string) {
	sendPageGeneric(w, codeReason, "[Error] "+h1, msg)
}
