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

func (ds dmSet) load(fpath string) (err error) {
	lst, err := loadDomainList(fpath)
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

func (ds dmSet) toSlice() []string {
	l := len(ds)
	lst := make([]string, l, l)

	i := 0
	for k, _ := range ds {
		lst[i] = k
		i++
	}
	return lst
}

func newParadmSet() *paraDmSet {
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
	chou    *paraDmSet

	blockedChanged bool
	directChanged  bool

	alwaysBlocked dmSet
	alwaysDirect  dmSet

	chouTime chouBlockTime
}

func newDomainSet() *DomainSet {
	ds := new(DomainSet)
	ds.direct = newParadmSet()
	ds.blocked = newParadmSet()
	ds.chou = newParadmSet()

	ds.alwaysBlocked = newDmSet()
	ds.alwaysDirect = newDmSet()

	ds.chouTime = chouBlockTime{time: map[string]time.Time{}}
	return ds
}

var domainSet = newDomainSet()

// Record when is the domain added to chou domain set
type chouBlockTime struct {
	sync.RWMutex
	time map[string]time.Time
}

func (cb *chouBlockTime) add(dm string) {
	now := time.Now()
	cb.Lock()
	cb.time[dm] = now
	cb.Unlock()
	debug.Printf("chou domain %s blocked at %v\n", dm, now)
}

func (cb *chouBlockTime) has(dm string) bool {
	cb.RLock()
	t, ok := cb.time[dm]
	cb.RUnlock()
	if !ok {
		return false
	}
	if time.Now().Sub(t) > chouTimeout {
		cb.del(dm)
		return false
	}
	return true
}

func (cb *chouBlockTime) del(dm string) {
	cb.Lock()
	delete(cb.time, dm)
	cb.Unlock()
	debug.Printf("chou domain %s block time unset\n", dm)
}

func (ds *DomainSet) isHostInAlwaysDs(host string) bool {
	dm := host2Domain(host)
	return ds.alwaysBlocked[dm] || ds.alwaysDirect[dm]
}

func (ds *DomainSet) isHostAlwaysDirect(host string) bool {
	return ds.alwaysDirect[host2Domain(host)]
}

func (ds *DomainSet) isHostAlwaysBlocked(host string) bool {
	return ds.alwaysBlocked[host2Domain(host)]
}

func (ds *DomainSet) isHostBlocked(host string) bool {
	dm := host2Domain(host)
	if ds.alwaysDirect[dm] {
		return false
	}
	if ds.alwaysBlocked[dm] {
		return true
	}
	if ds.chouTime.has(dm) {
		return true
	}
	return ds.blocked.has(dm)
}

func (ds *DomainSet) isHostDirect(host string) bool {
	dm := host2Domain(host)
	if ds.alwaysDirect[dm] {
		return true
	}
	if ds.alwaysBlocked[dm] {
		return false
	}
	return ds.direct.has(dm)
}

func (ds *DomainSet) isHostChouFeng(host string) bool {
	return ds.chou.has(host2Domain(host))
}

func (ds *DomainSet) addChouHost(host string) bool {
	dm := host2Domain(host)
	if ds.chouTime.has(dm) {
		return false
	}
	ds.chouTime.add(dm)
	return true
}

// Return true if the host is taken as blocked later
func (ds *DomainSet) addBlockedHost(host string) bool {
	dm := host2Domain(host)
	if ds.isHostAlwaysDirect(host) || ds.chou.has(dm) || dm == "localhost" ||
		hostIsIP(host) || ds.blocked.has(dm) {
		return false
	}
	ds.blocked.add(dm)
	ds.blockedChanged = true
	debug.Printf("%s added to blocked list\n", dm)
	// Delete this domain from direct domain set
	if ds.direct.has(dm) {
		ds.direct.del(dm)
		ds.directChanged = true
		debug.Printf("%s deleted from direct list\n", dm)
	}
	return true
}

func (ds *DomainSet) addDirectHost(host string) (added bool) {
	dm := host2Domain(host)
	if ds.isHostInAlwaysDs(host) || ds.chou.has(dm) || dm == "localhost" ||
		hostIsIP(host) || ds.direct.has(dm) {
		return false
	}
	ds.direct.add(dm)
	ds.directChanged = true
	debug.Printf("%s added to direct list\n", dm)
	// Delete this domain from blocked domain set
	if ds.blocked.has(dm) {
		ds.blocked.del(dm)
		ds.blockedChanged = true
		debug.Printf("%s deleted from blocked list\n", dm)
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
	// chou domain set maybe added to blocked site during execution,
	// filter them out before writing back to disk.
	ds.filterOutDs(ds.chou.dmSet)
	ds.writeBlockedDs()
	ds.writeDirectDs()
}

// TODO reload domain list when receives SIGUSR1
// one difficult here is that we may concurrently access maps which is not
// safe.
// Can we create a new domain set first, then change the reference of the original one?
// Domain set reference changing should be atomic.

func (ds *DomainSet) load() {
	ds.blocked.load(dsFile.blocked)
	ds.direct.load(dsFile.direct)
	ds.alwaysBlocked.load(dsFile.alwaysBlocked)
	ds.alwaysDirect.load(dsFile.alwaysDirect)
	ds.chou.load(dsFile.chou)

	ds.filterOutDs(ds.chou.dmSet)
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

func host2Domain(host string) (domain string) {
	host, _ = splitHostPort(host)
	host = trimLastDot(host)
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
