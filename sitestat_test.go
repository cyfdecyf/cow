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

func TestSiteStatGetVisitCnt(t *testing.T) {
	ss := newSiteStat()

	g, _ := ParseRequestURI("gtemp.com")
	si := ss.GetVisitCnt(g)
	if !si.AsDirect() {
		t.Error("never visited site should be considered as direct")
	}
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
	if !si.AsDirect() {
		t.Errorf("%s should use direct visit\n", b.Host)
	}

	tw, _ := ParseRequestURI("www.tblocked.com")
	ss.Vcnt[tw.Domain] = newVisitCnt(0, userCnt)
	si = ss.GetVisitCnt(tw)
	if si.AlwaysDirect() {
		t.Errorf("%s should not alwaysDirect\n", tw.Host)
	}

	g1, _ := ParseRequestURI("www.shoulddirect.com")
	si = ss.GetVisitCnt(g1)
	if !si.AsDirect() {
		t.Errorf("%s direct %d times, should use direct visit\n", g1.Host, directDelta+1)
	}
	si = ss.GetVisitCnt(g1)
}
