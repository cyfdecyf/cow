package main

import (
	"errors"
	"fmt"
	"github.com/cyfdecyf/bufio"
	"math/rand"
	"os"
	"strings"
	"sync"
	"time"
)

func init() {
	rand.Seed(time.Now().Unix())
}

// VisitCnt and SiteStat are used to track how many times a site is visited.
// With this information: meow knows which sites are frequently visited, and
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

// meow don't need very accurate visit count, so update to visit count value is
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

func (vc *VisitCnt) AsDirect() bool {
	return (vc.Blocked == 0) || (vc.Direct-vc.Blocked >= directDelta) || vc.AlwaysDirect()
}

func (vc *VisitCnt) AlwaysDirect() bool {
	return vc.Direct == userCnt
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
	// When a site changes from direct to blocked by GFW, meow should learn
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
	hbhLock sync.RWMutex
}

func newSiteStat() *SiteStat {
	return &SiteStat{
		Vcnt: map[string]*VisitCnt{},
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

func (ss *SiteStat) loadList(lst []string, direct, blocked vcntint) {
	for _, d := range lst {
		ss.Vcnt[d] = newVisitCntWithTime(direct, blocked, zeroTime)
	}
}

func (ss *SiteStat) GetDirectList() []string {
	lst := make([]string, 0)
	// anyway to do more fine grained locking?
	ss.vcLock.RLock()
	for site, vc := range ss.Vcnt {
		if vc.AsDirect() {
			lst = append(lst, site)
		}
	}
	ss.vcLock.RUnlock()
	return lst
}

var siteStat = newSiteStat()

func initSiteStat() {
	if directList, err := loadSiteList(configPath.alwaysDirect); err == nil {
		siteStat.loadList(directList, userCnt, 0)
	}
}

const (
	siteStatExit = iota
	siteStatCont
)

// Lock ensures only one goroutine calling store.
// siteStatFini ensures no more calls after going to exit.
var storeLock sync.Mutex
var siteStatFini bool

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
