package main

var blocked = map[string]bool{}

func isDomainBlocked(domain string) bool {
	_, ok := blocked[domain]
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
		blocked[d] = true
	}
}

func init() {
	go addBlockedRequestHandler()
}
