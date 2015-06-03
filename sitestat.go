package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cyfdecyf/bufio"
)

func init() {
	rand.Seed(time.Now().Unix())
}

// VisitCnt and SiteStat are used to track how many times a site is visited.
// With this information: COW knows which sites are frequently visited, and
// judging whether a site is blocked or not is more reliable.

const (
	directDelta  = 5
	blockedDelta = 5
	maxCnt       = 100 // no protect to update visit cnt, smaller value is unlikely to overflow
	userCnt      = -1  // this represents user specified host or domain
)

type siteVisitMethod int

// minus operation on visit count may get negative value, so use signed int
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
type VisitCnt struct {
	Direct    vcntint   `json:"direct"`
	Blocked   vcntint   `json:"block"`
	Recent    Date      `json:"recent"`
	rUpdated  bool      // whether Recent is updated, we only need date precision
	blockedOn time.Time // when is the site last blocked
}

func newVisitCnt(direct, blocked vcntint) *VisitCnt {
	return &VisitCnt{direct, blocked, Date(time.Now()), true, zeroTime}
}

func newVisitCntWithTime(direct, blocked vcntint, t time.Time) *VisitCnt {
	return &VisitCnt{direct, blocked, Date(t), true, zeroTime}
}

func (vc *VisitCnt) userSpecified() bool {
	return vc.Blocked == userCnt || vc.Direct == userCnt
}

const siteStaleThreshold = 10 * 24 * time.Hour

func (vc *VisitCnt) isStale() bool {
	return time.Now().Sub(time.Time(vc.Recent)) > siteStaleThreshold
}

// shouldNotSave returns true if the a VisitCnt is not visited for a long time
// (several days) or is specified by user.
func (vc *VisitCnt) shouldNotSave() bool {
	return vc.userSpecified() || vc.isStale() || (vc.Blocked == 0 && vc.Direct == 0)
}

const tmpBlockedTimeout = 2 * time.Minute

func (vc *VisitCnt) AsTempBlocked() bool {
	return time.Now().Sub(vc.blockedOn) < tmpBlockedTimeout
}

func (vc *VisitCnt) AsDirect() bool {
	return (vc.Blocked == 0) || (vc.Direct-vc.Blocked >= directDelta) || vc.AlwaysDirect()
}

func (vc *VisitCnt) AsBlocked() bool {
	if vc.Blocked == userCnt || vc.AsTempBlocked() {
		return true
	}
	// add some randomness to fix mistake
	delta := vc.Blocked - vc.Direct
	return delta >= blockedDelta && rand.Intn(int(delta)) != 0
}

func (vc *VisitCnt) AlwaysDirect() bool {
	return vc.Direct == userCnt
}

func (vc *VisitCnt) AlwaysBlocked() bool {
	return vc.Blocked == userCnt
}

func (vc *VisitCnt) OnceBlocked() bool {
	return vc.Blocked > 0 || vc.AlwaysBlocked() || vc.AsTempBlocked()
}

func (vc *VisitCnt) tempBlocked() {
	vc.blockedOn = time.Now()
}

// time.Time is composed of 3 fields, so need lock to protect update. As
// update of last visit is not frequent (at most once for each domain), use a
// global lock to avoid associating a lock to each VisitCnt.
var visitLock sync.Mutex

// visit updates visit cnt
func (vc *VisitCnt) visit(inc *vcntint) {
	if *inc < maxCnt {
		*inc++
	}
	// Because of concurrent update, possible for *inc to overflow and become
	// negative, but very unlikely.
	if *inc > maxCnt || *inc < 0 {
		*inc = maxCnt
	}

	if !vc.rUpdated {
		vc.rUpdated = true
		visitLock.Lock()
		vc.Recent = Date(time.Now())
		visitLock.Unlock()
	}
}

func (vc *VisitCnt) DirectVisit() {
	if networkBad() || vc.userSpecified() {
		return
	}
	// one successful direct visit probably means the site is not actually
	// blocked
	vc.visit(&vc.Direct)
	vc.Blocked = 0
}

func (vc *VisitCnt) BlockedVisit() {
	if networkBad() || vc.userSpecified() {
		return
	}
	// When a site changes from direct to blocked by GFW, COW should learn
	// this quickly and remove it from the PAC ASAP. So change direct to 0
	// once there's a single blocked visit, this ensures the site is removed
	// upon the next PAC update.
	vc.visit(&vc.Blocked)
	vc.Direct = 0
}

type SiteStat struct {
	Update Date                 `json:"update"`
	Vcnt   map[string]*VisitCnt `json:"site_info"` // Vcnt uses host as key
	vcLock sync.RWMutex

	// Whether a domain has blocked host. Used to avoid considering a domain as
	// direct though it has blocked hosts.
	hasBlockedHost map[string]bool
	hbhLock        sync.RWMutex
}

func newSiteStat() *SiteStat {
	return &SiteStat{
		Vcnt:           map[string]*VisitCnt{},
		hasBlockedHost: map[string]bool{},
	}
}

func (ss *SiteStat) get(s string) *VisitCnt {
	ss.vcLock.RLock()
	Vcnt, ok := ss.Vcnt[s]
	ss.vcLock.RUnlock()
	if ok {
		return Vcnt
	}
	return nil
}

func (ss *SiteStat) create(s string) (vcnt *VisitCnt) {
	vcnt = newVisitCnt(0, 0)
	ss.vcLock.Lock()
	ss.Vcnt[s] = vcnt
	ss.vcLock.Unlock()
	return
}

// Caller should guarantee that always direct url does not attempt
// blocked visit.
func (ss *SiteStat) TempBlocked(url *URL) {
	debug.Printf("%s temp blocked\n", url.Host)

	vcnt := ss.get(url.Host)
	if vcnt == nil {
		panic("TempBlocked should always get existing visitCnt")
	}
	vcnt.tempBlocked()

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

var alwaysDirectVisitCnt = newVisitCnt(userCnt, 0)

func (ss *SiteStat) GetVisitCnt(url *URL) (vcnt *VisitCnt) {
	if parentProxy.empty() { // no way to retry, so always visit directly
		return alwaysDirectVisitCnt
	}
	if url.Domain == "" { // simple host or private ip
		return alwaysDirectVisitCnt
	}
	if vcnt = ss.get(url.Host); vcnt != nil {
		return
	}
	if len(url.Domain) != len(url.Host) {
		if dmcnt := ss.get(url.Domain); dmcnt != nil && dmcnt.userSpecified() {
			// if the domain is not specified by user, should create a new host
			// visitCnt
			return dmcnt
		}
	}
	return ss.create(url.Host)
}

func (ss *SiteStat) store(statPath string) (err error) {
	now := time.Now()
	var savedSS *SiteStat
	if ss.Update == Date(zeroTime) {
		ss.Update = Date(time.Now())
	}
	if now.Sub(time.Time(ss.Update)) > siteStaleThreshold {
		// Not updated for a long time, don't drop any record
		savedSS = ss
		// Changing update time too fast will also drop useful record
		savedSS.Update = Date(time.Time(ss.Update).Add(siteStaleThreshold / 2))
		if time.Time(savedSS.Update).After(now) {
			savedSS.Update = Date(now)
		}
	} else {
		savedSS = newSiteStat()
		savedSS.Update = Date(now)
		ss.vcLock.RLock()
		for site, vcnt := range ss.Vcnt {
			if vcnt.shouldNotSave() {
				continue
			}
			savedSS.Vcnt[site] = vcnt
		}
		ss.vcLock.RUnlock()
	}

	b, err := json.MarshalIndent(savedSS, "", "\t")
	if err != nil {
		errl.Println("Error marshalling site stat:", err)
		panic("internal error: error marshalling site")
	}

	// Store stat into temp file first and then rename.
	// Ensures atomic update to stat file to avoid file damage.

	// Create tmp file inside config firectory to avoid cross FS rename.
	f, err := ioutil.TempFile(config.dir, "stat")
	if err != nil {
		errl.Println("create tmp file to store stat", err)
		return
	}
	if _, err = f.Write(b); err != nil {
		errl.Println("Error writing stat file:", err)
		f.Close()
		return
	}
	f.Close()

	// Windows don't allow rename to existing file.
	os.Remove(statPath + ".bak")
	os.Rename(statPath, statPath+".bak")
	if err = os.Rename(f.Name(), statPath); err != nil {
		errl.Println("rename new stat file", err)
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

func (ss *SiteStat) loadUserList() {
	if directList, err := loadSiteList(config.DirectFile); err == nil {
		ss.loadList(directList, userCnt, 0)
	}
	if blockedList, err := loadSiteList(config.BlockedFile); err == nil {
		ss.loadList(blockedList, 0, userCnt)
	}
}

// Filter sites covered by user specified domains, also filter out stale
// sites.
func (ss *SiteStat) filterSites() {
	// It's not safe to remove element while iterating over a map.
	var removeSites []string

	// find what to remove first
	ss.vcLock.RLock()
	for site, vcnt := range ss.Vcnt {
		if vcnt.userSpecified() {
			continue
		}
		if vcnt.isStale() {
			removeSites = append(removeSites, site)
			continue
		}
		var dmcnt *VisitCnt
		domain := host2Domain(site)
		if domain != site {
			dmcnt = ss.get(domain)
		}
		if dmcnt != nil && dmcnt.userSpecified() {
			removeSites = append(removeSites, site)
		}
	}
	ss.vcLock.RUnlock()

	// do remove
	ss.vcLock.Lock()
	for _, site := range removeSites {
		delete(ss.Vcnt, site)
	}
	ss.vcLock.Unlock()
}

func (ss *SiteStat) load(file string) (err error) {
	defer func() {
		// load builtin list first, so user list can override builtin
		ss.loadBuiltinList()
		ss.loadUserList()
		ss.filterSites()
		for host, vcnt := range ss.Vcnt {
			if vcnt.OnceBlocked() {
				ss.hasBlockedHost[host2Domain(host)] = true
			}
		}
	}()
	if file == "" {
		return
	}
	if err = isFileExists(file); err != nil {
		if !os.IsNotExist(err) {
			errl.Println("Error loading stat:", err)
		}
		return
	}
	var f *os.File
	if f, err = os.Open(file); err != nil {
		errl.Printf("Error opening site stat %s: %v\n", file, err)
		return
	}
	defer f.Close()
	b, err := ioutil.ReadAll(f)
	if err != nil {
		errl.Println("Error reading site stat:", err)
		return
	}
	if err = json.Unmarshal(b, ss); err != nil {
		errl.Println("Error decoding site stat:", err)
		return
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
		if vc.AsDirect() {
			lst = append(lst, site)
		}
	}
	ss.vcLock.RUnlock()
	return lst
}

var siteStat = newSiteStat()

func initSiteStat() {
	err := siteStat.load(config.StatFile)
	if err != nil {
		// Simply try to load the stat.back, create a new object to avoid error
		// in default site list.
		siteStat = newSiteStat()
		err = siteStat.load(config.StatFile + ".bak")
		// After all its not critical , simply re-create a stat object if anything is not ok
		if err != nil {
			siteStat = newSiteStat()
			siteStat.load("") // load default site list
		}
	}

	// Dump site stat while running, so we don't always need to close cow to
	// get updated stat.
	go func() {
		for {
			time.Sleep(5 * time.Minute)
			storeSiteStat(siteStatCont)
		}
	}()
}

const (
	siteStatExit = iota
	siteStatCont
)

// Lock ensures only one goroutine calling store.
// siteStatFini ensures no more calls after going to exit.
var storeLock sync.Mutex
var siteStatFini bool

func storeSiteStat(cont byte) {
	storeLock.Lock()
	defer storeLock.Unlock()

	if siteStatFini {
		return
	}
	siteStat.store(config.StatFile)
	if cont == siteStatExit {
		siteStatFini = true
	}
}

func loadSiteList(fpath string) (lst []string, err error) {
	if fpath == "" {
		return
	}
	if err = isFileExists(fpath); err != nil {
		if !os.IsNotExist(err) {
			info.Printf("Error loading domaint list: %v\n", err)
		}
		return
	}
	f, err := os.Open(fpath)
	if err != nil {
		errl.Println("Error opening domain list:", err)
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lst = make([]string, 0)
	for scanner.Scan() {
		site := strings.TrimSpace(scanner.Text())
		if site == "" {
			continue
		}
		lst = append(lst, site)
	}
	if scanner.Err() != nil {
		errl.Printf("Error reading domain list %s: %v\n", fpath, scanner.Err())
	}
	return lst, scanner.Err()
}
