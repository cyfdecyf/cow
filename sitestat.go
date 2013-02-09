package main

import (
	"bufio"
	"encoding/json"
	"errors"
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
	directDelta  = 30
	blockedDelta = 20
	maxCnt       = 80 // no protect to update visit cnt, use a small value to avoid overflow
	userCnt      = -1 // this represents user specified host or domain
)

type siteVisitMethod int

const (
	vmUnknown siteVisitMethod = iota
	vmDirect
	vmBlocked
	vmTempBlocked
)

type vcntint int8

type Date time.Time

const dateLayout = "2006-01-02"

func (d Date) MarshalJSON() ([]byte, error) {
	return []byte("\"" + time.Time(d).Format(dateLayout) + "\""), nil
}

func (d *Date) UnmarshalJSON(input []byte) error {
	if len(input) != len(dateLayout)+2 {
		return errors.New(fmt.Sprintf("unmarshaling date: invalid input %s", string(input)))
	}
	input = input[1 : len(dateLayout)+1]
	t, err := time.Parse(dateLayout, string(input))
	*d = Date(t)
	return err
}

// COW don't need very accurate visit count, so update to visit count value is
// not protected.
type visitCnt struct {
	Direct   vcntint `json:"direct"`
	Blocked  vcntint `json:"block"`
	Recent   Date    `json:"recent"`
	rUpdated bool    // whether Recent is updated, we only need date precision
}

func newVisitCnt(direct, blocked vcntint) *visitCnt {
	return &visitCnt{direct, blocked, Date(time.Now()), true}
}

func newVisitCntWithTime(direct, blocked vcntint, t time.Time) *visitCnt {
	return &visitCnt{direct, blocked, Date(t), true}
}

func (vc *visitCnt) userSpecified() bool {
	return vc.Blocked == userCnt || vc.Direct == userCnt
}

const siteStaleThreshold = 30 * 24 * time.Hour

// shouldDrop returns true if the a VisitCnt is not visited for a long time
// (several days) or is specified by user.
func (vc *visitCnt) shouldDrop() bool {
	return vc.userSpecified() || time.Now().Sub(time.Time(vc.Recent)) > siteStaleThreshold
}

func (vc *visitCnt) asDirect() bool {
	return (vc.Direct == userCnt) || (vc.Direct-vc.Blocked >= directDelta)
}

func (vc *visitCnt) asBlocked() bool {
	if vc.Blocked == userCnt {
		return true
	}
	// add some randomness to fix mistake
	delta := vc.Blocked - vc.Direct
	return delta >= blockedDelta && rand.Intn(int(delta)) != 0
}

// time.Time is composed of 3 fields, so need lock to protect update. As
// update of last visit is not frequent (at most once for each domain), use a
// global lock to avoid associating a lock to each VisitCnt.
var visitLock sync.Mutex

// visit updates visit cnt
func (vc *visitCnt) visit(inc *vcntint) {
	// Possible for *inc to overflow and become negative, but very unlikely.
	if *inc < maxCnt {
		*inc++
	}

	if !vc.rUpdated {
		vc.rUpdated = true
		visitLock.Lock()
		vc.Recent = Date(time.Now())
		visitLock.Unlock()
	}
}

func (vc *visitCnt) directVisit() {
	if vc.userSpecified() {
		return
	}
	vc.visit(&vc.Direct)
	// one successful direct visit probably means the site is not actually
	// blocked
	vc.Blocked = 0
}

func (vc *visitCnt) blockedVisit() {
	if vc.userSpecified() {
		return
	}
	vc.visit(&vc.Blocked)
	// blockage maybe caused by bad network connection
	vc.Direct = vc.Direct - 5
	if vc.Direct < 0 {
		vc.Direct = 0
	}
}

// directVisitStat records visit count
type SiteStat struct {
	Update Date                 `json:"update"`
	Vcnt   map[string]*visitCnt `json:"site_info"` // Vcnt uses host as key
	vcLock sync.RWMutex

	tempBlocked *TimeoutSet

	// Whether a domain has blocked host. Used to avoid considering a domain as
	// direct though it has blocked hosts.
	hasBlockedHost map[string]bool
	hbhLock        sync.RWMutex
}

func newSiteStat() *SiteStat {
	const blockedTimeout = 2 * time.Minute
	return &SiteStat{
		Vcnt:           map[string]*visitCnt{},
		hasBlockedHost: map[string]bool{},
		tempBlocked:    NewTimeoutSet(blockedTimeout),
	}
}

func (ss *SiteStat) get(s string) *visitCnt {
	ss.vcLock.RLock()
	Vcnt, ok := ss.Vcnt[s]
	ss.vcLock.RUnlock()
	if ok {
		return Vcnt
	}
	return nil
}

func (ss *SiteStat) getOrCreate(s string) (vcnt *visitCnt) {
	if vcnt = ss.get(s); vcnt != nil {
		return
	}
	vcnt = newVisitCnt(0, 0)
	ss.vcLock.Lock()
	ss.Vcnt[s] = vcnt
	ss.vcLock.Unlock()
	return
}

// Caller should guarantee that always direct url does not attempt
// blocked visit.
func (ss *SiteStat) BlockedVisit(url *URL) {
	debug.Printf("%s blocked\n", url.Host)
	ss.tempBlocked.add(url.Host)

	vcnt := ss.getOrCreate(url.Host)
	vcnt.blockedVisit()

	// Mistakenly consider a partial blocked domain as direct will make that
	// domain into PAC and never have a chance to correct the error.
	// Once using blocked visit, a host is considered to maybe blocked even if
	// it's block visit count decrease to 0. As hasBlockedHost is not saved,
	// upon next start up of COW, the information will reflect the current
	// status of that host.
	ss.hbhLock.RLock()
	t := ss.hasBlockedHost[url.Domain]
	ss.hbhLock.RUnlock()
	if !t {
		ss.hbhLock.Lock()
		ss.hasBlockedHost[url.Domain] = true
		ss.hbhLock.Unlock()
	}
}

func (ss *SiteStat) DirectVisit(url *URL) {
	vcnt := ss.getOrCreate(url.Host)
	vcnt.directVisit()
}

type SiteInfo struct {
	alwaysDirect  bool
	alwaysBlocked bool
	onceBlocked   bool
	visitMethod   siteVisitMethod
}

func (ss *SiteStat) GetSiteInfo(url *URL) *SiteInfo {
	if url.Domain == "" { // simple host or ip
		return &SiteInfo{true, false, false, vmDirect}
	}
	if ss.tempBlocked.has(url.Host) {
		return &SiteInfo{false, false, true, vmTempBlocked}
	}
	vcnt := ss.get(url.Host)
	var alwaysDirect, alwaysBlocked, onceBlocked bool
	var visitMethod siteVisitMethod
	if vcnt != nil {
		onceBlocked = (vcnt.Blocked != 0)
		if vcnt.userSpecified() {
			if vcnt.asBlocked() {
				alwaysBlocked = true
				visitMethod = vmBlocked
			} else {
				alwaysDirect = true
				visitMethod = vmDirect
			}
		} else {
			if vcnt.asBlocked() {
				debug.Printf("host %s as blocked\n", url.Host)
				visitMethod = vmBlocked
			} else if vcnt.asDirect() {
				debug.Printf("host %s as direct\n", url.Host)
				visitMethod = vmDirect
			}
		}
		return &SiteInfo{alwaysDirect, alwaysBlocked, onceBlocked, visitMethod}
	}
	if len(url.Domain) == len(url.Host) {
		return &SiteInfo{false, false, false, vmUnknown}
	}
	vcnt = ss.get(url.Domain)
	if vcnt != nil {
		onceBlocked = (vcnt.Blocked != 0)
		if vcnt.userSpecified() {
			if vcnt.asBlocked() {
				alwaysBlocked = true
				visitMethod = vmBlocked
			} else {
				alwaysDirect = true
				visitMethod = vmDirect
			}
		} else {
			if vcnt.asBlocked() {
				debug.Printf("domain %s as blocked\n", url.Domain)
				visitMethod = vmBlocked
			} else if vcnt.asDirect() {
				debug.Printf("domain %s as direct\n", url.Domain)
				visitMethod = vmDirect
			}
		}
		return &SiteInfo{alwaysDirect, alwaysBlocked, onceBlocked, visitMethod}
	}
	return &SiteInfo{false, false, false, vmUnknown}
}

func (ss *SiteStat) store(file string) (err error) {
	if err = mkConfigDir(); err != nil {
		return
	}

	now := time.Now()
	var s *SiteStat
	if ss.Update == Date(zeroTime) {
		ss.Update = Date(time.Now())
	}
	if now.Sub(time.Time(ss.Update)) > siteStaleThreshold {
		// Not updated for a long time, don't drop any record
		s = ss
		// Changing update time too fast will also drop useful record
		s.Update = Date(time.Time(ss.Update).Add(siteStaleThreshold / 5))
		if time.Time(s.Update).Sub(now) > 0 {
			s.Update = Date(now)
		}
	} else {
		s = newSiteStat()
		s.Update = Date(now)
		ss.vcLock.RLock()
		for site, vcnt := range ss.Vcnt {
			// user specified sites may change, always filter them out
			dmcnt := ss.get(host2Domain(site))
			if (dmcnt != nil && dmcnt.userSpecified()) || vcnt.shouldDrop() {
				continue
			}
			s.Vcnt[site] = vcnt
		}
		ss.vcLock.RUnlock()
	}

	b, err := json.MarshalIndent(s, "", "\t")
	if err != nil {
		errl.Println("Error marshalling site stat:", err)
		panic("internal error: error marshalling site")
	}

	f, err := os.Create(file)
	if err != nil {
		errl.Println("Can't create stat file:", err)
		return
	}
	defer f.Close()
	if _, err = f.Write(b); err != nil {
		errl.Println("Error writing stat file:", err)
		return
	}
	return
}

func (ss *SiteStat) loadList(lst []string, direct, blocked vcntint) {
	for _, d := range lst {
		ss.Vcnt[d] = newVisitCntWithTime(direct, blocked, zeroTime)
	}
}

func (ss *SiteStat) loadBuiltinList() {
	ss.loadList(blockedDomainList, 0, userCnt)
	ss.loadList(directDomainList, userCnt, 0)
}

func (ss *SiteStat) load(file string) (err error) {
	var exists bool
	if exists, err = isFileExists(file); err != nil {
		fmt.Println("Error loading stat:", err)
		return
	}
	if !exists {
		return
	}
	var f *os.File
	if f, err = os.Open(file); err != nil {
		fmt.Printf("Error opening site stat %s: %v\n", file, err)
		return
	}
	b, err := ioutil.ReadAll(f)
	if err != nil {
		fmt.Println("Error reading site stat:", err)
		return
	}
	if err = json.Unmarshal(b, ss); err != nil {
		fmt.Println("Error decoding site stat:", err)
		return
	}

	ss.loadBuiltinList()

	// load user specified sites at last to override previous values
	if directList, err := loadSiteList(dsFile.alwaysDirect); err == nil {
		ss.loadList(directList, userCnt, 0)
	}
	if blockedList, err := loadSiteList(dsFile.alwaysBlocked); err == nil {
		ss.loadList(blockedList, 0, userCnt)
	}

	for k, v := range ss.Vcnt {
		if v.Blocked > 0 {
			ss.hasBlockedHost[k] = true
		}
	}
	return
}

func (ss *SiteStat) GetDirectList() []string {
	lst := make([]string, 0)
	// anyway to do more fine grained locking?
	ss.vcLock.RLock()
	for site, vc := range ss.Vcnt {
		if ss.hasBlockedHost[host2Domain(site)] {
			continue
		}
		if vc.asDirect() {
			lst = append(lst, site)
		}
	}
	ss.vcLock.RUnlock()
	return lst
}

var siteStat = newSiteStat()

func initSiteStat() {
	loadSiteStat()
	if isWindows() {
		// TODO How to detect program exit on Windows? This
		// is just a workaround.
		go func() {
			for {
				time.Sleep(time.Hour)
				storeSiteStat()
			}
		}()
	}
}

func loadSiteStat() {
	if siteStat.load(dsFile.stat) != nil {
		os.Exit(1)
	}
}

func storeSiteStat() {
	siteStat.store(dsFile.stat)
}

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
