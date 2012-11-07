package main

var blockedDomain = map[string]bool{}

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

func init() {
	go addBlockedRequestHandler()
}
