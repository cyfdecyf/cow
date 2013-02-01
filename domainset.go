package main

import (
	"bufio"
	"io"
	"log"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
)

// Use direct connection after blocked for chouTimeout
const chouTimeout = 2 * time.Minute

type dmSet map[string]bool

// Basically a concurrent map. I don't want to use channels to implement
// concurrent access to this as I'm comfortable to use locks for simple tasks
// like this
type paraDmSet struct {
	sync.RWMutex
	dmSet
}

func newDmSet() dmSet {
	return make(map[string]bool)
}

func (ds dmSet) addList(lst []string) {
	// This executes in single goroutine, so no need to use lock
	for _, v := range lst {
		// debug.Println("loaded domain:", v)
		ds[v] = true
	}
}

func (ds dmSet) loadFromFile(fpath string) (err error) {
	lst, err := loadDomainList(fpath)
	if err != nil {
		return
	}
	ds.addList(lst)
	return
}

func (ds dmSet) toSlice() []string {
	l := len(ds)
	lst := make([]string, l)

	i := 0
	for k, _ := range ds {
		lst[i] = k
		i++
	}
	return lst
}

func newParaDmSet() *paraDmSet {
	return &paraDmSet{dmSet: newDmSet()}
}

func (ds *paraDmSet) add(dm string) {
	ds.Lock()
	ds.dmSet[dm] = true
	ds.Unlock()
}

func (ds *paraDmSet) has(dm string) bool {
	ds.RLock()
	_, ok := ds.dmSet[dm]
	ds.RUnlock()
	return ok
}

func (ds *paraDmSet) del(dm string) {
	ds.Lock()
	delete(ds.dmSet, dm)
	ds.Unlock()
}

type DomainSet struct {
	direct  *paraDmSet
	blocked *paraDmSet

	blockedChanged bool
	directChanged  bool

	alwaysBlocked dmSet
	alwaysDirect  dmSet

	chouSet *TimeoutSet
}

func newDomainSet() *DomainSet {
	ds := new(DomainSet)
	ds.direct = newParaDmSet()
	ds.blocked = newParaDmSet()

	ds.alwaysBlocked = newDmSet()
	ds.alwaysDirect = newDmSet()

	ds.chouSet = NewTimeoutSet(chouTimeout)
	return ds
}

var domainSet = newDomainSet()

func (ds *DomainSet) isURLInAlwaysDs(url *URL) bool {
	return url.Domain == "" || ds.alwaysDirect[url.Host] || ds.alwaysDirect[url.Domain] ||
		ds.alwaysBlocked[url.Host] || ds.alwaysBlocked[url.Domain]
}

func (ds *DomainSet) isURLAlwaysDirect(url *URL) bool {
	if url.Domain == "" { // always use direct access for simple host name
		return true
	}
	return ds.alwaysDirect[url.Host] || ds.alwaysDirect[url.Domain]
}

func (ds *DomainSet) isURLAlwaysBlocked(url *URL) bool {
	if url.Domain == "" {
		return false
	}
	return ds.alwaysBlocked[url.Host] || ds.alwaysBlocked[url.Domain]
}

func (ds *DomainSet) lookupBlocked(s string) bool {
	if debug {
		if _, port := splitHostPort(s); port != "" {
			panic("lookupBlocked got host with port")
		}
	}
	if ds.alwaysDirect[s] {
		return false
	}
	if ds.alwaysBlocked[s] {
		return true
	}
	if ds.chouSet.has(s) {
		return true
	}
	return ds.blocked.has(s)
}

func (ds *DomainSet) isURLBlocked(url *URL) bool {
	if url.Domain == "" {
		return false
	}
	return ds.lookupBlocked(url.Host) || ds.lookupBlocked(url.Domain)
}

func (ds *DomainSet) lookupDirect(s string) bool {
	if debug {
		if _, port := splitHostPort(s); port != "" {
			panic("lookupDirect got host with port")
		}
	}
	if ds.alwaysDirect[s] {
		return true
	}
	if ds.alwaysBlocked[s] {
		return false
	}
	return ds.direct.has(s)
}

func (ds *DomainSet) isURLDirect(url *URL) bool {
	if url.Domain == "" {
		return true
	}
	return ds.lookupDirect(url.Host) || ds.lookupDirect(url.Domain)
}

func (ds *DomainSet) addChouURL(url *URL) bool {
	if ds.isURLAlwaysDirect(url) || url.Domain == "" || url.HostIsIP() {
		return false
	}
	if !ds.chouSet.has(url.Domain) {
		ds.chouSet.add(url.Domain)
		info.Printf("%s blocked\n", url.HostPort)
	}
	return true
}

// Return true if the host is taken as blocked
func (ds *DomainSet) addBlockedURL(url *URL) bool {
	if !config.UpdateBlocked {
		return ds.addChouURL(url)
	}
	if ds.isURLAlwaysDirect(url) || url.Domain == "" || url.HostIsIP() {
		return false
	}
	if ds.blocked.has(url.Domain) {
		return true
	}
	ds.blocked.add(url.Domain)
	ds.blockedChanged = true
	debug.Printf("%s added to blocked list\n", url.Domain)
	// Delete this domain from direct domain set
	if ds.direct.has(url.Domain) {
		ds.direct.del(url.Domain)
		ds.directChanged = true
		debug.Printf("%s deleted from direct list\n", url.Domain)
	}
	return true
}

func (ds *DomainSet) addDirectURL(url *URL) (added bool) {
	if !config.UpdateDirect {
		return
	}
	if ds.isURLInAlwaysDs(url) || url.Domain == "" ||
		url.HostIsIP() || ds.direct.has(url.Domain) {
		return false
	}
	ds.direct.add(url.Domain)
	ds.directChanged = true
	debug.Printf("%s added to direct list\n", url.Domain)
	// Delete this domain from blocked domain set
	if ds.blocked.has(url.Domain) {
		ds.blocked.del(url.Domain)
		ds.blockedChanged = true
		debug.Printf("%s deleted from blocked list\n", url.Domain)
	}
	return true
}

func (ds *DomainSet) writeBlockedDs() {
	if !config.UpdateBlocked || !ds.blockedChanged {
		return
	}
	writeDomainList(dsFile.blocked, ds.blocked.toSlice())
}

func (ds *DomainSet) writeDirectDs() {
	if !config.UpdateDirect || !ds.directChanged {
		return
	}
	writeDomainList(dsFile.direct, ds.direct.toSlice())
}

// filter out domain in blocked and direct domain set.
func (ds *DomainSet) filterOutDs(dmset dmSet) {
	for k, _ := range dmset {
		if ds.blocked.dmSet[k] {
			delete(ds.blocked.dmSet, k)
			ds.blockedChanged = true
		}
		if ds.direct.dmSet[k] {
			delete(ds.direct.dmSet, k)
			ds.directChanged = true
		}
	}
}

// If a domain name appears in both blocked and direct domain set, only keep
// it in the blocked set.
func (ds *DomainSet) filterOutBlockedInDirect() {
	for k, _ := range ds.blocked.dmSet {
		if ds.direct.dmSet[k] {
			delete(ds.direct.dmSet, k)
			ds.directChanged = true
		}
	}
	for k, _ := range ds.alwaysBlocked {
		if ds.alwaysDirect[k] {
			errl.Printf("%s in both always blocked and direct domain lists, taken as blocked.\n", k)
			delete(ds.alwaysDirect, k)
		}
	}
}

func (ds *DomainSet) write() {
	ds.writeBlockedDs()
	ds.writeDirectDs()
}

// TODO reload domain list when receives SIGUSR1
// one difficult here is that we may concurrently access maps which is not
// safe.
// Can we create a new domain set first, then change the reference of the original one?
// Domain set reference changing should be atomic.

func (ds *DomainSet) load() {
	ds.blocked.addList(blockedDomainList)
	blockedDomainList = nil
	ds.direct.addList(directDomainList)
	directDomainList = nil
	ds.blocked.loadFromFile(dsFile.blocked)
	ds.direct.loadFromFile(dsFile.direct)
	ds.alwaysBlocked.loadFromFile(dsFile.alwaysBlocked)
	ds.alwaysDirect.loadFromFile(dsFile.alwaysDirect)

	ds.filterOutDs(ds.alwaysDirect)
	ds.filterOutDs(ds.alwaysBlocked)
	ds.filterOutBlockedInDirect()
}

func loadDomainList(fpath string) (lst []string, err error) {
	var exists bool
	if exists, err = isFileExists(fpath); err != nil {
		errl.Printf("Error loading domaint list: %v\n", err)
	}
	if !exists {
		return
	}
	f, err := os.Open(fpath)
	if err != nil {
		errl.Printf("Error opening domain list %s: %v\n", fpath)
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
			errl.Printf("Error reading domain list %s: %v\n", fpath, err)
			return
		}
		if domain == "" {
			continue
		}
		lst = append(lst, strings.TrimSpace(domain))
	}
	return
}

func mkConfigDir() (err error) {
	if dsFile.dir == "" {
		return
	}
	exists, err := isDirExists(dsFile.dir)
	if err != nil {
		errl.Printf("Error creating config directory: %v\n", err)
		return
	}
	if exists {
		return
	}
	if err = os.Mkdir(dsFile.dir, 0755); err != nil {
		log.Printf("Error create config directory %s: %v\n", dsFile.dir, err)
	}
	return
}

func writeDomainList(fpath string, lst []string) (err error) {
	if err = mkConfigDir(); err != nil {
		return
	}
	tmpPath := path.Join(dsFile.dir, "tmpdomain")
	f, err := os.Create(tmpPath)
	if err != nil {
		errl.Println("Error creating tmp domain list:", err)
		return
	}

	sort.Sort(sort.StringSlice(lst))

	all := strings.Join(lst, newLine)
	f.WriteString(all)
	f.Close()

	if isWindows() {
		// On windows, can't rename to a file which already exists.
		var exists bool
		if exists, err = isFileExists(fpath); err != nil {
			errl.Printf("Error removing domain list: %v\n", err)
			return
		}
		if exists {
			if err = os.Remove(fpath); err != nil {
				errl.Printf("Error removing domain list %s for update: %v\n", fpath, err)
			}
		}
	}
	if err = os.Rename(tmpPath, fpath); err != nil {
		errl.Printf("Error renaming tmp domain list file to %s: %v\n", fpath, err)
	}
	return
}

var topLevelDomain = map[string]bool{
	"ac":  true,
	"co":  true,
	"org": true,
	"com": true,
	"net": true,
	"edu": true,
}

// host2Domain returns the domain of a host. It will recognize domains like
// google.com.hk. Returns empty string for simple host.
func host2Domain(host string) (domain string) {
	host, _ = splitHostPort(host)
	host = trimLastDot(host)
	lastDot := strings.LastIndex(host, ".")
	if lastDot == -1 {
		return ""
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
