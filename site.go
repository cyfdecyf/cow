package main

import (
	"bytes"
	"sort"
	"strings"
	"text/template"
	"time"
)

func sendSite(c *clientConn, r *Request) error {
	buf, ok := genSite(r)
	var err error
	if ok {
		_, err = c.Write(buf)
		if err != nil {
			debug.Printf("cli(%s) error sending site: %s", c.RemoteAddr(), err)
		}
	} else {
		sendErrorPage(c, "404 not found", "Page not found",
			genErrMsg(r, nil, "Serving request to COW proxy."))
		errl.Printf("cli(%s) page not found, serving request to cow %s\n%s",
			c.RemoteAddr(), r, r.Verbose())
	}
	return err
}

func genSite(r *Request) ([]byte, bool) {
	switch {
	case strings.HasPrefix(r.URL.Path, "/site/direct/"):
		return genSiteChange(r)
	case strings.HasPrefix(r.URL.Path, "/site/block/"):
		return genSiteChange(r)
	case strings.HasPrefix(r.URL.Path, "/site/auto/"):
		return genSiteChange(r)
	case r.URL.Path == "/site":
		return genSiteList(r)
	}
	return []byte(""), false
}

func genSiteChange(r *Request) ([]byte, bool) {
	tokens := strings.Split(r.URL.Path, "/")
	domain := tokens[3]
	newStat := tokens[2]
	result := changeSiteStat(domain, newStat)
	buf := new(bytes.Buffer)
	buf.WriteString(siteHeader)
	buf.WriteString(result)
	return buf.Bytes(), true
}

func changeSiteStat(domain string, newStat string) string {
	siteStat.vcLock.Lock()
	defer siteStat.vcLock.Unlock()

	vcnt, ok := siteStat.Vcnt[domain]
	if ok {
		switch newStat {
		case "direct":
			vcnt.Direct = userCnt
			vcnt.Blocked = 0
			return "direct"
		case "block":
			vcnt.Direct = 0
			vcnt.Blocked = userCnt
			return "block"
		case "auto":
			vcnt.Direct = 0
			vcnt.Blocked = 0
			return "auto"
		default:
			panic("invalid stat")
		}
	} else {
		return "notfound"
	}
}

func genSiteList(r *Request) ([]byte, bool) {
	buf := new(bytes.Buffer)
	buf.WriteString(siteHeader)

	sites := getSites()
	sort.Sort(sites)

	if err := siteTemplate.Execute(buf, sites); err != nil {
		errl.Println("Error generating site file:", err)
		panic("Error generating site file")
	}
	return buf.Bytes(), true
}

const siteHeader = "HTTP/1.1 200 OK\r\n" +
	"Server: cow-proxy\r\n" +
	"Content-Type: text/html\r\n" +
	"Connection: close\r\n\r\n"

const siteRawTmpl = `<!DOCTYPE HTML>
<html>
	<head>
		<title>COW Proxy</title>
		<style type="text/css">
			table, th, td {
				border: 1px solid black;
				border-collapse: collapse;
			}
			button:disabled {
				background-color: #DDFFFF;
			}
		</style>
		<script type="text/javascript">
			function changeSiteStat(domain, newStat) {
				var req = new XMLHttpRequest();
				req.open("GET", "/site/" + newStat + "/" + domain, true);
				req.onreadystatechange = function() {
					if (req.readyState == 4 && req.status == 200) {
						var result = req.responseText;
						if (result == "direct" || result == "block" || result == "auto") {
							var td = document.getElementById("domain:" + domain);
							for (var i = 0; i < td.children.length; ++i) {
								var button = td.children[i];
								if (button.getAttribute("class") == result) {
									button.setAttribute("disabled", "disabled");
								} else {
									button.removeAttribute("disabled");
								}
							}
						}
					}
				};
				req.send();
			}
		</script>
	</head>
	<body>
		<table>
			{{range .}}
				<tr>
					<td>{{.Domain}}</td>
					<td id="domain:{{.Domain}}">
						<button type="button" class="direct"
							{{if .IsDirect}} disabled="disabled" {{end}}
							onclick='changeSiteStat("{{.Domain}}", "direct");'>
							Direct
						</button>
						<button type="button" class="block"
							{{if .IsBlock}} disabled="disabled" {{end}}
							onclick='changeSiteStat("{{.Domain}}", "block");'>
							Block
						</button>
						<button type="button" class="auto"
							{{if .IsAuto}} disabled="disabled" {{end}}
							onclick='changeSiteStat("{{.Domain}}", "auto");'>
							Auto
						</button>
					</td>
				</tr>
			{{end}}
		</table>
	</body>
</html>
`

var siteTemplate *template.Template

func init() {
	var err error
	siteTemplate, err = template.New("site").Parse(siteRawTmpl)
	if err != nil {
		Fatal("Internal error on generating site template:", err)
	}
}

type siteEntry struct {
	Domain   string
	Vcnt     *VisitCnt
	Recent   time.Time
	IsDirect bool
	IsBlock  bool
	IsAuto   bool
}

type siteList []siteEntry

func (sl siteList) Len() int {
	return len(sl)
}

func (sl siteList) Less(i, j int) bool {
	return time.Time(sl[i].Vcnt.Recent).Unix() > time.Time(sl[j].Vcnt.Recent).Unix()
}

func (sl siteList) Swap(i, j int) {
	sl[i], sl[j] = sl[j], sl[i]
}

func getSites() siteList {
	siteStat.vcLock.RLock()
	defer siteStat.vcLock.RUnlock()

	var sites siteList
	for domain, vcnt := range siteStat.Vcnt {
		site := siteEntry{Domain: domain, Vcnt: vcnt}
		site.Recent = time.Time(vcnt.Recent)
		site.IsDirect = vcnt.AlwaysDirect()
		site.IsBlock = vcnt.AlwaysBlocked()
		site.IsAuto = !vcnt.AlwaysDirect() && !vcnt.AlwaysBlocked()
		sites = append(sites, site)
	}

	return sites
}
