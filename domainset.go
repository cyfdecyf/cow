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

type domainSet map[string]bool

// Basically a concurrent map. I don't want to use channels to implement
// concurrent access to this as I'm comfortable to use locks for simple tasks
// like this
type paraDomainSet struct {
	sync.RWMutex
	domainSet
}

func newDomainSet() domainSet {
	return make(map[string]bool)
}

func (ds domainSet) loadDomainList(fpath string) (lst []string, err error) {
	lst, err = loadDomainList(fpath)
	if err != nil {
		return
	}
	// This executes in single goroutine, so no need to use lock
	for _, v := range lst {
		// debug.Println("loaded domain:", v)
		ds[v] = true
	}
	return
}

func (ds domainSet) toArray() []string {
	l := len(ds)
	lst := make([]string, l, l)

	i := 0
	for k, _ := range ds {
		lst[i] = k
		i++
	}
	return lst
}

func newParaDomainSet() *paraDomainSet {
	return &paraDomainSet{domainSet: newDomainSet()}
}

func (ds *paraDomainSet) add(dm string) {
	ds.Lock()
	ds.domainSet[dm] = true
	ds.Unlock()
}

func (ds *paraDomainSet) has(dm string) bool {
	ds.RLock()
	_, ok := ds.domainSet[dm]
	ds.RUnlock()
	return ok
}

func (ds *paraDomainSet) del(dm string) {
	ds.Lock()
	delete(ds.domainSet, dm)
	ds.Unlock()
}

var blockedDs = newParaDomainSet()
var directDs = newParaDomainSet()

var blockedDomainChanged = false
var directDomainChanged = false

var alwaysBlockedDs = newDomainSet()
var alwaysDirectDs = newDomainSet()
var chouDs = newDomainSet()

func inAlwaysDs(dm string) bool {
	return alwaysBlockedDs[dm] || alwaysDirectDs[dm]
}

func hostInAlwaysDirectDs(host string) bool {
	h, _ := splitHostPort(host)
	return alwaysDirectDs[host2Domain(h)]
}

func hostInAlwaysBlockedDs(host string) bool {
	h, _ := splitHostPort(host)
	return alwaysBlockedDs[host2Domain(h)]
}

func isRequestBlocked(r *Request) bool {
	host, _ := splitHostPort(r.URL.Host)
	dm := host2Domain(host)
	if alwaysDirectDs[dm] {
		return false
	}
	if alwaysBlockedDs[dm] {
		return true
	}
	return blockedDs.has(dm)
}

func addBlockedRequest(r *Request) bool {
	host, _ := splitHostPort(r.URL.Host)
	if hostIsIP(host) {
		return false
	}
	dm := host2Domain(host)
	// For chou domain, we should add it to the blocked list in order to use
	// parent proxy, but don't write it back to auto-block file.
	if inAlwaysDs(dm) {
		return false
	}
	if !blockedDs.has(dm) {
		blockedDs.add(dm)
		blockedDomainChanged = true
		debug.Printf("%v added to blocked list\n", dm)
		return true
	}
	// Delete this request from direct domain set
	delDirectRequest(r)
	return false
}

func delBlockedRequest(r *Request) {
	host, _ := splitHostPort(r.URL.Host)
	dm := host2Domain(host)
	if blockedDs.has(dm) {
		blockedDs.del(dm)
		blockedDomainChanged = true
		debug.Printf("%v deleted from blocked list\n", dm)
	}
}

func addDirectRequest(r *Request) {
	host, _ := splitHostPort(r.URL.Host)
	if hostIsIP(host) {
		return
	}
	dm := host2Domain(host)
	if inAlwaysDs(dm) || chouDs[dm] {
		return
	}
	if !directDs.has(dm) {
		directDs.add(dm)
		directDomainChanged = true
	}
	// Delete this request from blocked domain set
	delBlockedRequest(r)
}

func delDirectRequest(r *Request) {
	host, _ := splitHostPort(r.URL.Host)
	dm := host2Domain(host)
	if directDs.has(dm) {
		directDs.del(dm)
		directDomainChanged = true
	}
}

func writeBlockedDs() {
	if !config.updateBlocked {
		return
	}
	if !blockedDomainChanged {
		return
	}
	writeDomainList(config.blockedFile, blockedDs.toArray())
}

func writeDirectDs() {
	if !config.updateDirect {
		return
	}
	if !directDomainChanged {
		return
	}
	writeDomainList(config.directFile, directDs.toArray())
}

func writeDomainSet() {
	for k, _ := range chouDs {
		delete(blockedDs.domainSet, k)
		delete(directDs.domainSet, k)
	}
	writeBlockedDs()
	writeDirectDs()
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

var topLevelDomain = map[string]bool{
	"co":  true,
	"org": true,
	"com": true,
	"net": true,
	"edu": true,
}

func host2Domain(host string) (domain string) {
	lastDot := strings.LastIndex(host, ".")
	if lastDot == -1 {
		return host // simple host name, we should not hanlde this
	}
	// Find the 2nd last dot
	dot2ndLast := strings.LastIndex(host[:lastDot], ".")
	if dot2ndLast == -1 {
		return host
	}

	part := host[dot2ndLast+1 : lastDot]
	// If the 2nd last part of a domain name equals to a top level
	// domain, search for the 3rd part in the host name.
	// So domains like bbc.co.uk will not be recorded as co.uk
	if topLevelDomain[part] {
		dot3rdLast := strings.LastIndex(host[:dot2ndLast], ".")
		if dot3rdLast == -1 {
			return host
		}
		return host[dot3rdLast+1:]
	}
	return host[dot2ndLast+1:]
}
