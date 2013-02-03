package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"os"
	"strings"
	"sync"
	"time"
)

func init() {
	rand.Seed(time.Now().Unix())
}

// visitCnt and visitStat are used to track how many times a site is visited.
// With this information: COW knows which sites are frequently visited, and
// judging whether a site is blocked or not is more reliable.

const (
	directDelta = 100
	blockDelta  = 20
	maxCnt      = 120 // no protect to update visit cnt, so value may exceed maxCnt
	userCnt     = -1  // this represents user specified host or domain
)

type siteVisitMethod int

const (
	vmDirect siteVisitMethod = iota
	vmBlocked
	vmUnknown
)

// COW don't need very accurate visit count, so update to visit count value is
// not protected.
type visitCnt struct {
	Blocked  int8      `json:"block"`
	Direct   int8      `json:"direct"`
	Recent   time.Time `json:"recent"`
	rUpdated bool      // whether Recent is updated, we only need date precision
}

func newVisitCnt(blocked, direct int8) *visitCnt {
	return &visitCnt{blocked, direct, time.Now(), true}
}

func newVisitCntBlocked() *visitCnt {
	return newVisitCnt(1, 0)
}

func newVisitCntDirect() *visitCnt {
	return newVisitCnt(0, 1)
}

func (vc *visitCnt) userSpecified() bool {
	return vc.Blocked == userCnt || vc.Direct == userCnt
}

// shouldDrop returns true if the a VisitCnt is not visited for a long time
// (several days) or is specified by user.
func (vc *visitCnt) shouldDrop() bool {
	return vc.userSpecified() || time.Now().Sub(vc.Recent) > (5*24*time.Hour)
}

func (vc *visitCnt) asDirect() bool {
	return (vc.Direct == userCnt) || (vc.Direct-vc.Blocked > directDelta)
}

func (vc *visitCnt) asBlocked() bool {
	if vc.Blocked == userCnt {
		return true
	}
	// add some randomness to fix mistake
	delta := vc.Blocked - vc.Direct
	return delta > blockDelta && rand.Intn(int(delta/3)) == 0
}

// time.Time is composed of 3 fields, so need lock to protect update. As
// update of last visit is not frequent (at most once for each domain), use a
// global lock to avoid associating a lock to each VisitCnt.
var visitLock sync.Mutex

// visit updates visit cnt
func (vc *visitCnt) visit(inc, dec *int8) {
	if vc.userSpecified() {
		return
	}
	// Possible for *cnt to overflow and become negative, but not likely. Even
	// if becomes negative, it should get chance to increase back to positive.
	if *inc < maxCnt {
		*inc++
	} else if *inc == maxCnt {
	} else {
		*inc = maxCnt
	}

	if *dec == 0 {
	} else if *dec > 0 {
		*dec--
	} else {
		*dec = 0
	}
	if !vc.rUpdated {
		vc.rUpdated = true
		visitLock.Lock()
		vc.Recent = time.Now()
		visitLock.Unlock()
	}
}

func (vc *visitCnt) directVisit() {
	vc.visit(&vc.Direct, &vc.Blocked)
}

func (vc *visitCnt) blockedVisit() {
	vc.visit(&vc.Blocked, &vc.Direct)
}

// directVisitStat records visit count
type SiteStat struct {
	Vcnt   map[string]*visitCnt `json:"site_stat"` // Vcnt uses host as key
	vcLock sync.RWMutex

	// TODO add chouSet

	// Whether a domain has blocked host. Used to avoid considering a domain as
	// direct though it has blocked hosts.
	hasBlockedHost map[string]bool
	hbhLock        sync.RWMutex
}

func newSiteStat() *SiteStat {
	return &SiteStat{Vcnt: map[string]*visitCnt{}, hasBlockedHost: map[string]bool{}}
}

func (vs *SiteStat) get(s string) *visitCnt {
	vs.vcLock.RLock()
	Vcnt, ok := vs.Vcnt[s]
	vs.vcLock.RUnlock()
	if ok {
		return Vcnt
	}
	return nil
}

func (vs *SiteStat) BlockedVisit(url *URL) {
	vcnt := vs.get(url.Host)
	if vcnt != nil {
		vcnt.blockedVisit()
	} else {
		vs.vcLock.Lock()
		vs.Vcnt[url.Host] = newVisitCntBlocked()
		vs.vcLock.Unlock()
	}
	// TODO If not taken as blocked from now on, block for some time.

	// Mistakenly consider a partial blocked domain as direct will make that
	// domain into PAC and never have a chance to correct the error.
	// Once using blocked visit, a host is considered to maybe blocked even if
	// it's block visit count decrease to 0. As hasBlockedHost is not saved,
	// upon next start up of COW, the information will reflect the current
	// status of that host.
	vs.hbhLock.RLock()
	t := vs.hasBlockedHost[url.Domain]
	vs.hbhLock.RUnlock()
	if !t {
		vs.hbhLock.Lock()
		vs.hasBlockedHost[url.Domain] = true
		vs.hbhLock.Unlock()
	}
}

func (ss *SiteStat) DirectVisit(url *URL) {
	vcnt := ss.get(url.Host)
	if vcnt != nil {
		vcnt.directVisit()
	} else {
		ss.vcLock.Lock()
		ss.Vcnt[url.Host] = newVisitCntDirect()
		ss.vcLock.Unlock()
	}

}

func (ss *SiteStat) GetVisitMethod(url *URL) siteVisitMethod {
	if url.Domain == "" { // simple host
		return vmDirect
	}
	// First check if host has visit info
	hostCnt := ss.get(url.Host)
	if hostCnt != nil {
		if hostCnt.asBlocked() {
			return vmBlocked
		} else if hostCnt.asDirect() {
			return vmDirect
		}
	}
	dmCnt := ss.get(url.Domain)
	if dmCnt != nil {
		if dmCnt.asBlocked() {
			return vmBlocked
		} else if dmCnt.asDirect() {
			return vmDirect
		}
	}
	return vmUnknown
}

func (ss *SiteStat) IsDirect(url *URL) bool {
	if url.Domain == "" {
		return true
	}

	// First check if host has visit info
	hostCnt := ss.get(url.Host)
	if hostCnt != nil {
		if hostCnt.asBlocked() {
			return false
		} else if hostCnt.asDirect() {
			return true
		}
	}
	dmCnt := ss.get(url.Domain)
	if dmCnt != nil {
		if dmCnt.asBlocked() {
			return false
		} else if dmCnt.asDirect() {
			return true
		}
	}
	// unknow site, taken as direct first
	return true
}

func (ss *SiteStat) store(file string) (err error) {
	s := newSiteStat()
	ss.vcLock.RLock()
	for k, v := range ss.Vcnt {
		if !v.shouldDrop() {
			s.Vcnt[k] = v
		}
	}
	ss.vcLock.RUnlock()
	b, err := json.MarshalIndent(s, "", "\t")
	if err != nil {
		errl.Println("Error marshalling site stat:", err)
		return
	}

	f, err := os.Create(file)
	if err != nil {
		errl.Println("Can't create stat file:", err)
		return
	}
	defer f.Close()
	if _, err = f.Write(b); err != nil {
		fmt.Println("Error writing stat file:", err)
		return
	}
	return
}

func (ss *SiteStat) load(file string) {
	var err error

	var exists bool
	if exists, err = isFileExists(file); err != nil {
		fmt.Println("Error loading stat:", err)
		os.Exit(1)
	}
	if !exists {
		return
	}
	var f *os.File
	if f, err = os.Open(file); err != nil {
		fmt.Printf("Error opening site stat %s: %v\n", file, err)
		os.Exit(1)
	}
	b, err := ioutil.ReadAll(f)
	if err != nil {
		fmt.Println("Error reading site stat:", err)
		os.Exit(1)
	}
	if err = json.Unmarshal(b, ss); err != nil {
		fmt.Println("Error decoding site stat:", err)
		os.Exit(1)
	}

	// load user specified sites
	if directList, err := loadSiteList(dsFile.alwaysDirect); err == nil {
		for _, d := range directList {
			ss.Vcnt[d] = newVisitCnt(0, userCnt)
		}
	}
	if blockedList, err := loadSiteList(dsFile.alwaysBlocked); err == nil {
		for _, d := range blockedList {
			ss.Vcnt[d] = newVisitCnt(userCnt, 0)
		}
	}

	for k, v := range ss.Vcnt {
		if v.Blocked > 0 {
			ss.hasBlockedHost[k] = true
		}
	}
}

var siteStat = newSiteStat()

func loadSiteList(fpath string) (lst []string, err error) {
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
	var site string
	for {
		site, err = ReadLine(fr)
		if err == io.EOF {
			return lst, nil
		} else if err != nil {
			errl.Printf("Error reading domain list %s: %v\n", fpath, err)
			return
		}
		if site == "" {
			continue
		}
		lst = append(lst, strings.TrimSpace(site))
	}
	return
}
