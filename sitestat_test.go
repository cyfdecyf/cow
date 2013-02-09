package main

import (
	"os"
	"testing"
	"time"
)

var _ = os.Remove

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
	st := newSiteStat()

	d1, _ := ParseRequestURI("www.foobar.com")
	d2, _ := ParseRequestURI("img.foobar.com")
	st.DirectVisit(d1)
	st.DirectVisit(d1)
	st.DirectVisit(d1)
	st.DirectVisit(d2)

	b1, _ := ParseRequestURI("blocked.com")
	b2, _ := ParseRequestURI("blocked2.com")
	st.BlockedVisit(b1)
	st.BlockedVisit(b2)

	const stfile = "testdata/stat"
	if err := st.store(stfile); err != nil {
		t.Fatal("store error:", err)
	}

	ld := newSiteStat()
	if err := ld.load(stfile); err != nil {
		t.Fatal("load stat error:", err)
	}
	vc := ld.get(d1.Host)
	if vc == nil {
		t.Fatalf("load error, %s not loaded\n", d1.Host)
	}
	if vc.Direct != 3 {
		t.Errorf("load error, %s should have visit cnt 3, got: %d\n", d1.Host, vc.Direct)
	}

	vc = ld.get(b1.Host)
	if vc == nil {
		t.Errorf("load error, %s not loaded\n", b1.Host)
	}

	// test bulitin site
	ap, _ := ParseRequestURI("apple.com")
	si := ld.GetSiteInfo(ap)
	if si.visitMethod != vmDirect {
		t.Error("builtin site apple.com should use direct access")
	}
	tw, _ := ParseRequestURI("twitter.com")
	blockeVisit := false
	// there're some randomness in treating a site as blocked
	// so try several times
	for i := 0; i < 2*blockedDelta; i++ {
		si = ld.GetSiteInfo(tw)
		if si.visitMethod == vmBlocked {
			blockeVisit = true
			break
		}
	}
	if blockeVisit == false {
		t.Error("builtin site twitter.com should use blocked access")
	}
	if len(ld.GetDirectList()) == 0 {
		t.Error("builtin site should appear in direct site list")
	}
	os.Remove(stfile)
}

func TestSiteStatVisit(t *testing.T) {
	ss := newSiteStat()

	g1, _ := ParseRequestURI("www.gtemp.com")
	g2, _ := ParseRequestURI("calendar.gtemp.com")
	g3, _ := ParseRequestURI("docs.gtemp.com")

	for i := 0; i < 10; i++ {
		ss.DirectVisit(g1)
	}
	ss.DirectVisit(g2)
	ss.DirectVisit(g3)

	if ss.hasBlockedHost[g1.Domain] {
		t.Errorf("direct domain %s should not have host at first\n", g1.Domain)
	}

	vc := ss.get(g1.Host)
	if vc == nil {
		t.Fatalf("no visitCnt for %s\n", g1.Host)
	}
	if vc.Direct != 10 {
		t.Errorf("direct cnt for %s not correct, should be 3, got: %d\n", g1.Host, vc.Direct)
	}
	if vc.Blocked != 0 {
		t.Errorf("block cnt for %s not correct, should be 0 before blocked visit, got: %d\n", g1.Host, vc.Blocked)
	}
	if vc.rUpdated != true {
		t.Errorf("visitCnt lvUpdated should be true after visit")
	}

	ss.BlockedVisit(g1)
	if vc.Blocked != 1 {
		t.Errorf("blocked cnt for %s after 1 blocked visit should be 1, got: %d\n", g1.Host, vc.Blocked)
	}
	if vc.Direct != 5 {
		t.Errorf("direct cnt for %s after 1 blocked visit should be 5, got: %d\n", g1.Host, vc.Direct)
	}

	// test blocked visit
	g4, _ := ParseRequestURI("plus.gtemp.com")
	ss.BlockedVisit(g4)
	ss.BlockedVisit(g4)
	// should be blocked for 2 minutes
	si := ss.GetSiteInfo(g4)
	if si.visitMethod != vmTempBlocked {
		t.Error("should be blocked for 2 minutes after blocked visit")
	}
	vc = ss.get(g4.Host)
	if vc == nil {
		t.Fatal("no visitCnt for ", g4.Host)
	}
	if vc.Blocked != 2 {
		t.Errorf("blocked cnt for %s not correct, should be 2, got: %d\n", g4.Host, vc.Blocked)
	}
	if vc.Direct != 0 {
		t.Errorf("direct cnt for %s not correct, should be 0, got: %d\n", g4.Host, vc.Direct)
	}
	if !ss.hasBlockedHost[g4.Domain] {
		t.Errorf("direct domain %s should have blocked host after blocked visit\n", g4.Domain)
	}
}

func TestSiteStatGetSiteInfo(t *testing.T) {
	ss := newSiteStat()

	g, _ := ParseRequestURI("gtemp.com")
	si := ss.GetSiteInfo(g)
	if si.visitMethod != vmUnknown {
		t.Error("should get unknown visit method")
	}

	b, _ := ParseRequestURI("www.btemp.com")
	ss.Vcnt[b.Host] = newVisitCnt(userCnt, 0)
	vc := ss.get(b.Host)
	if !vc.userSpecified() {
		t.Error("should be user specified")
	}
	if !vc.shouldDrop() {
		t.Error("user specified should be dropped")
	}
	si = ss.GetSiteInfo(b)
	if !si.alwaysDirect {
		t.Errorf("%s should alwaysDirect\n", b.Host)
	}
	if si.alwaysBlocked {
		t.Errorf("%s should not alwaysBlocked\n", b.Host)
	}
	if si.onceBlocked {
		t.Errorf("%s should not onceBlocked\n", b.Host)
	}
	if si.visitMethod != vmDirect {
		t.Errorf("%s should use direct visit\n", b.Host)
	}

	tw, _ := ParseRequestURI("www.tblocked.com")
	ss.Vcnt[tw.Domain] = newVisitCnt(0, userCnt)
	si = ss.GetSiteInfo(tw)
	if si.visitMethod != vmBlocked {
		t.Errorf("%s should use blocked visit\n", tw.Host)
	}
	if si.alwaysDirect {
		t.Errorf("%s should not alwaysDirect\n", tw.Host)
	}
	if !si.alwaysBlocked {
		t.Errorf("%s should not alwaysBlocked\n", tw.Host)
	}
	if !si.onceBlocked {
		t.Errorf("%s should onceBlocked\n", tw.Host)
	}

	g1, _ := ParseRequestURI("www.shoulddirect.com")
	si = ss.GetSiteInfo(g1)
	if si.visitMethod != vmUnknown {
		t.Errorf("%s not visited, should return unknow visit method\n", g1.Host)
	}
	ss.DirectVisit(g1)
	si = ss.GetSiteInfo(g1)
	if si.visitMethod != vmUnknown {
		t.Errorf("%s visited only once, should still return unknow visit method\n", g1.Host)
	}
	for i := 0; i < directDelta; i++ {
		ss.DirectVisit(g1)
	}
	si = ss.GetSiteInfo(g1)
	if si.visitMethod != vmDirect {
		t.Errorf("%s direct %d times, should use direct visit\n", g1.Host, directDelta+1)
	}
	if si.onceBlocked {
		t.Errorf("%s has not blocked visit, should not has once blocked\n", g1.Host)
	}
	ss.BlockedVisit(g1)
	si = ss.GetSiteInfo(g1)
	if !si.onceBlocked {
		t.Errorf("%s has one blocked visit, should has once blocked\n", g1.Host)
	}
}
