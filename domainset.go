package main

import (
	"bufio"
	"io"
	"os"
	"path"
	"sort"
	"strings"
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

func (ds *domainSet) loadDomainList(fpath string) (lst []string, err error) {
	lst, err = loadDomainList(fpath)
	if err != nil {
		return
	}
	// This executes in single goroutine, so no need to use lock
	for _, v := range lst {
		// debug.Println("loaded domain:", v)
		ds.domain[v] = true
	}
	return
}

func (ds *domainSet) toArray() []string {
	l := len(ds.domain)
	lst := make([]string, l, l)

	i := 0
	for k, _ := range ds.domain {
		lst[i] = k
		i++
	}
	return lst
}

var blockedDs = newDomainSet()
var directDs = newDomainSet()

var hasNewBlockedDomain = false
var hasNewDirectDomain = false

func isDomainBlocked(dm string) bool {
	return blockedDs.has(dm)
}

func isRequestBlocked(r *Request) bool {
	return isDomainBlocked(host2Domain(r.URL.Host))
}

func addRequestToDs(r *Request, ds *domainSet) {
	d := host2Domain(r.URL.Host)
	ds.add(d)
}

func addBlockedRequest(r *Request) {
	addRequestToDs(r, blockedDs)
	hasNewBlockedDomain = true
	debug.Printf("%v added to blocked list\n", r.URL.Host)
}

func addDirectRequest(r *Request) {
	addRequestToDs(r, directDs)
	hasNewDirectDomain = true
	debug.Printf("%v added to direct list\n", r.URL.Host)
}

func writeBlockedDs() {
	if !hasNewBlockedDomain {
		return
	}
	writeDomainList(config.blockedFile, blockedDs.toArray())
}

func writeDirectDs() {
	if !hasNewDirectDomain {
		return
	}
	writeDomainList(config.directFile, directDs.toArray())
}

func loadDomainList(fpath string) (lst []string, err error) {
	f, err := openFile(fpath)
	if f == nil || err != nil {
		return
	}
	defer f.Close()

	fr := bufio.NewReader(f)
	lst = make([]string, 0)
	var domain string
	for {
		domain, err = ReadLine(fr)
		if err == io.EOF {
			return lst, nil
		} else if err != nil {
			errl.Println("Error reading domain list from:", fpath, err)
			return
		}
		if domain == "" {
			continue
		}
		lst = append(lst, strings.TrimSpace(domain))
	}
	return
}

func writeDomainList(fpath string, lst []string) (err error) {
	tmpPath := path.Join(config.dir, "tmp-domain")
	f, err := os.Create(tmpPath)
	if err != nil {
		errl.Println("Error creating tmp domain list file:", err)
		return
	}

	sort.Sort(sort.StringSlice(lst))

	all := strings.Join(lst, "\n")
	f.WriteString(all)
	f.Close()

	if err = os.Rename(tmpPath, fpath); err != nil {
		errl.Printf("Error moving tmp domain list file to %s: %v\n", fpath, err)
	}
	return
}
