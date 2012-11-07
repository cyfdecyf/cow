package main

import (
	"sync"
)

type domainCmd int

// Basically a concurrent map. I don't want to use channels to implement
// concurrent access to this as I'm comfortable to use locks for simple tasks
// like this
type domainSet struct {
	sync.RWMutex
	domain map[string]bool
}

func newDomainSet() *domainSet {
	ds := new(domainSet)
	ds.domain = make(map[string]bool)
	return ds
}

func (ds *domainSet) add(dm string) {
	ds.Lock()
	ds.domain[dm] = true
	ds.Unlock()
}

func (ds *domainSet) has(dm string) bool {
	ds.RLock()
	_, ok := ds.domain[dm]
	ds.RUnlock()
	return ok
}

func (ds *domainSet) del(dm string) {
	ds.Lock()
	delete(ds.domain, dm)
	ds.Unlock()
}

var blocked = newDomainSet()

var hasNewBlockedDomain = false

func isDomainBlocked(dm string) bool {
	return blocked.has(dm)
}

func isRequestBlocked(r *Request) bool {
	return isDomainBlocked(host2Domain(r.URL.Host))
}

func addBlockedRequest(r *Request) {
	d := host2Domain(r.URL.Host)
	blocked.add(d)
	hasNewBlockedDomain = true
	debug.Printf("%v added to blocked list\n", d)
}

func loadBlocked() {
	lst, err := loadDomainList(config.blockedFile)
	if err != nil {
		return
	}

	// This executes in single goroutine, so no need to use lock
	for _, v := range lst {
		// debug.Println("blocked domain:", v)
		blocked.domain[v] = true
	}
}

func writeBlocked() {
	if !hasNewBlockedDomain {
		return
	}

	l := len(blocked.domain)
	lst := make([]string, l, l)

	i := 0
	for k, _ := range blocked.domain {
		lst[i] = k
		i++
	}

	writeDomainList(config.blockedFile, lst)
}
