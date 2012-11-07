package main

var blockedDomain = map[string]bool{}
var hasNewBlockedDomain = false

func isDomainBlocked(domain string) bool {
	_, ok := blockedDomain[domain]
	return ok
}

func isRequestBlocked(r *Request) bool {
	return isDomainBlocked(host2Domain(r.URL.Host))
}

func addBlockedRequest(r *Request) {
	blockedReqChan <- r
}

var blockedReqChan = make(chan *Request, 5)

func addBlockedRequestHandler() {
	for {
		r := <-blockedReqChan
		d := host2Domain(r.URL.Host)
		debug.Printf("%v added to blocked list\n", d)
		blockedDomain[d] = true
		hasNewBlockedDomain = true
	}
}

func loadBlocked() {
	lst, err := loadDomainList(config.blockedFile)
	if err != nil {
		return
	}

	for _, v := range lst {
		// debug.Println("blocked domain:", v)
		blockedDomain[v] = true
	}
}

func writeBlocked() {
	if !hasNewBlockedDomain {
		return
	}

	l := len(blockedDomain)
	lst := make([]string, l, l)

	i := 0
	for k, _ := range blockedDomain {
		lst[i] = k
		i++
	}

	writeDomainList(config.blockedFile, lst)
}

func init() {
	go addBlockedRequestHandler()
}
