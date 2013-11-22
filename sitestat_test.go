package main

import (
	"os"
	"testing"
	"time"
)

var _ = os.Remove

func TestNetworkBad(t *testing.T) {
	if networkBad() {
		t.Error("Network by default should be good")
	}
}

func TestDateMarshal(t *testing.T) {
	d := Date(time.Date(2013, 2, 4, 0, 0, 0, 0, time.UTC))
	j, err := d.MarshalJSON()
	if err != nil {
		t.Error("Error marshalling json:", err)
	}
	if string(j) != "\"2013-02-04\"" {
		t.Error("Date marshal result wrong, got:", string(j))
	}

	err = d.UnmarshalJSON([]byte("\"2013-01-01\""))
	if err != nil {
		t.Error("Error unmarshaling Date:", err)
	}
	tm := time.Time(d)
	if tm.Year() != 2013 || tm.Month() != 1 || tm.Day() != 1 {
		t.Error("Unmarshaled date wrong, got:", tm)
	}
}

func TestSiteStatLoadStore(t *testing.T) {
	ss := newSiteStat()
	ss.load("testdata/nosuchfile") // load buildin and user specified list
	if len(ss.GetDirectList()) == 0 {
		t.Error("builtin site should appear in direct site list even with no stat file")
	}

	url1, _ := ParseRequestURI("www.foobar.com")
	url2, _ := ParseRequestURI("img.foobar.com")
	vcnt1 := ss.GetVisitCnt(url1)
	vcnt1.DirectVisit()
	vcnt1.DirectVisit()
	vcnt1.DirectVisit()
	vcnt2 := ss.GetVisitCnt(url2)
	vcnt2.DirectVisit()

	blockurl1, _ := ParseRequestURI("blocked.com")
	blockurl2, _ := ParseRequestURI("blockeurl2.com")
	si1 := ss.GetVisitCnt(blockurl1)
	si1.BlockedVisit()
	si2 := ss.GetVisitCnt(blockurl2)
	si2.BlockedVisit()

	// make google.com with a large direct count, but plus.google.com is in blocked list
	// so it shouldn't be considered as direct site
	gurl, _ := ParseRequestURI("google.com")
	gvcnt := ss.GetVisitCnt(gurl)
	gvcnt.Direct = 100

	const stfile = "testdata/stat"
	if err := ss.store(stfile); err != nil {
		t.Fatal("store error:", err)
	}

	ld := newSiteStat()
	if err := ld.load(stfile); err != nil {
		t.Fatal("load stat error:", err)
	}
	vc := ld.get(url1.Host)
	if vc == nil {
		t.Fatalf("load error, %s not loaded\n", url1.Host)
	}
	if vc.Direct != 3 {
		t.Errorf("load error, %s should have visit cnt 3, got: %d\n", url1.Host, vc.Direct)
	}

	vc = ld.get(blockurl1.Host)
	if vc == nil {
		t.Errorf("load error, %s not loaded\n", blockurl1.Host)
	}

	// test bulitin site
	ap, _ := ParseRequestURI("apple.com")
	si := ld.GetVisitCnt(ap)
	if !si.AlwaysDirect() {
		t.Error("builtin site apple.com should always use direct access")
	}
	tw, _ := ParseRequestURI("twitter.com")
	si = ld.GetVisitCnt(tw)
	if !si.AsBlocked() || !si.AlwaysBlocked() {
		t.Error("builtin site twitter.com should use blocked access")
	}
	plus, _ := ParseRequestURI("plus.google.com")
	si = ld.GetVisitCnt(plus)
	if !si.AsBlocked() || !si.AlwaysBlocked() {
		t.Error("builtin site plus.google.com should use blocked access")
	}

	directList := ld.GetDirectList()
	if len(directList) == 0 {
		t.Error("builtin site should appear in direct site list")
	}
	if !ld.hasBlockedHost["google.com"] {
		t.Error("google.com should have blocked host")
	}
	for _, d := range directList {
		if d == "google.com" {
			t.Errorf("direct list contains 2nd level domain which has sub host that's blocked")
		}
	}
	os.Remove(stfile)
}

func TestSiteStatVisitCnt(t *testing.T) {
	ss := newSiteStat()

	g1, _ := ParseRequestURI("www.gtemp.com")
	g2, _ := ParseRequestURI("calendar.gtemp.com")
	g3, _ := ParseRequestURI("docs.gtemp.com")

	sg1 := ss.GetVisitCnt(g1)
	for i := 0; i < 30; i++ {
		sg1.DirectVisit()
	}
	sg2 := ss.GetVisitCnt(g2)
	sg2.DirectVisit()
	sg3 := ss.GetVisitCnt(g3)
	sg3.DirectVisit()

	if ss.hasBlockedHost[g1.Domain] {
		t.Errorf("direct domain %s should not have host at first\n", g1.Domain)
	}

	vc := ss.get(g1.Host)
	if vc == nil {
		t.Fatalf("no VisitCnt for %s\n", g1.Host)
	}
	if vc.Direct != 30 {
		t.Errorf("direct cnt for %s not correct, should be 30, got: %d\n", g1.Host, vc.Direct)
	}
	if vc.Blocked != 0 {
		t.Errorf("block cnt for %s not correct, should be 0 before blocked visit, got: %d\n", g1.Host, vc.Blocked)
	}
	if vc.rUpdated != true {
		t.Errorf("VisitCnt lvUpdated should be true after visit")
	}

	vc.BlockedVisit()
	if vc.Blocked != 1 {
		t.Errorf("blocked cnt for %s after 1 blocked visit should be 1, got: %d\n", g1.Host, vc.Blocked)
	}
	if vc.Direct != 0 {
		t.Errorf("direct cnt for %s after 1 blocked visit should be 0, got: %d\n", g1.Host, vc.Direct)
	}
	if vc.AsDirect() {
		t.Errorf("after blocked visit, a site should not be considered as direct\n")
	}

	// test blocked visit
	g4, _ := ParseRequestURI("plus.gtemp.com")
	si := ss.GetVisitCnt(g4)
	ss.TempBlocked(g4)
	// should be blocked for 2 minutes
	if !si.AsTempBlocked() {
		t.Error("should be blocked for 2 minutes after blocked visit")
	}
	si.BlockedVisit() // After temp blocked, update blocked visit count
	if si.Blocked != 1 {
		t.Errorf("blocked cnt for %s not correct, should be 1, got: %d\n", g4.Host, vc.Blocked)
	}
	vc = ss.get(g4.Host)
	if vc == nil {
		t.Fatal("no VisitCnt for ", g4.Host)
	}
	if vc.Direct != 0 {
		t.Errorf("direct cnt for %s not correct, should be 0, got: %d\n", g4.Host, vc.Direct)
	}
	if !ss.hasBlockedHost[g4.Domain] {
		t.Errorf("direct domain %s should have blocked host after blocked visit\n", g4.Domain)
	}
}

func TestSiteStatGetVisitCnt(t *testing.T) {
	ss := newSiteStat()

	g, _ := ParseRequestURI("gtemp.com")
	si := ss.GetVisitCnt(g)
	if !si.AsDirect() {
		t.Error("never visited site should be considered as direct")
	}
	if si.AsBlocked() || si.AsTempBlocked() {
		t.Error("never visited site should not be considered as blocked/temp blocked")
	}
	si.DirectVisit()
	gw, _ := ParseRequestURI("www.gtemp.com")
	sig := ss.GetVisitCnt(gw)
	// gtemp.com is not user specified, www.gtemp.com should get separate visitCnt
	if sig == si {
		t.Error("host should get separate visitCnt for not user specified domain")
	}

	b, _ := ParseRequestURI("www.btemp.com")
	ss.Vcnt[b.Host] = newVisitCnt(userCnt, 0)
	vc := ss.get(b.Host)
	if !vc.userSpecified() {
		t.Error("should be user specified")
	}
	if !vc.shouldNotSave() {
		t.Error("user specified should be dropped")
	}
	si = ss.GetVisitCnt(b)
	if !si.AlwaysDirect() {
		t.Errorf("%s should alwaysDirect\n", b.Host)
	}
	if si.AlwaysBlocked() {
		t.Errorf("%s should not alwaysBlocked\n", b.Host)
	}
	if si.OnceBlocked() {
		t.Errorf("%s should not onceBlocked\n", b.Host)
	}
	if !si.AsDirect() {
		t.Errorf("%s should use direct visit\n", b.Host)
	}

	tw, _ := ParseRequestURI("www.tblocked.com")
	ss.Vcnt[tw.Domain] = newVisitCnt(0, userCnt)
	si = ss.GetVisitCnt(tw)
	if !si.AsBlocked() {
		t.Errorf("%s should use blocked visit\n", tw.Host)
	}
	if si.AlwaysDirect() {
		t.Errorf("%s should not alwaysDirect\n", tw.Host)
	}
	if !si.AlwaysBlocked() {
		t.Errorf("%s should not alwaysBlocked\n", tw.Host)
	}
	if !si.OnceBlocked() {
		t.Errorf("%s should onceBlocked\n", tw.Host)
	}

	g1, _ := ParseRequestURI("www.shoulddirect.com")
	for i := 0; i < directDelta; i++ {
		si.DirectVisit()
	}
	si = ss.GetVisitCnt(g1)
	if !si.AsDirect() {
		t.Errorf("%s direct %d times, should use direct visit\n", g1.Host, directDelta+1)
	}
	if si.OnceBlocked() {
		t.Errorf("%s has not blocked visit, should not has once blocked\n", g1.Host)
	}
	si = ss.GetVisitCnt(g1)
	si.BlockedVisit()
	if !si.OnceBlocked() {
		t.Errorf("%s has one blocked visit, should has once blocked\n", g1.Host)
	}
}
