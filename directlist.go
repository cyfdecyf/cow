package main

import (
	"github.com/cyfdecyf/bufio"
	"net"
	"os"
	"strings"
	"sync"
)

type DirectList struct {
	Domain map[string]DomainType
	sync.RWMutex
}

type DomainType byte

const (
	domainTypeUnknown DomainType = iota
	domainTypeDirect
	domainTypeProxy
)

func newDirectList() *DirectList {
	return &DirectList{
		Domain: map[string]DomainType{},
	}
}

func (directList *DirectList) shouldDirect(url *URL) (direct bool) {
	if parentProxy.empty() { // no way to retry, so always visit directly
		return true
	}
	if url.Domain == "" { // simple host or private ip
		return true
	}
	if directList.Domain[url.Host] == domainTypeDirect || directList.Domain[url.Domain] == domainTypeDirect {
		return true
	}

	if !config.JudgeByIP {
		return false
	}

	// if JudgeByIP not enabled, no domain will be set as domainTypeProxy
	// because domainTypeProxy will only be set when MEOW finds the ip
	// of the host should not be `direct` at running time
	if directList.Domain[url.Host] == domainTypeProxy {
		return false
	}

	var ip string
	isIP, isPrivate := hostIsIP(url.Host)
	if isIP {
		if isPrivate {
			directList.add(url.Host, domainTypeDirect)
			return true
		}
		ip = url.Host
	} else {
		hostIPs, err := net.LookupIP(url.Host)
		if err != nil {
			errl.Printf("error looking up host ip %s, err %s", url.Host, err)
			return false
		}
		ip = hostIPs[0].String()
	}

	if ipShouldDirect(ip) {
		directList.add(url.Host, domainTypeDirect)
		return true
	} else {
		directList.add(url.Host, domainTypeProxy)
		return false
	}
}

func (directList *DirectList) add(host string, domainType DomainType) {
	directList.Lock()
	defer directList.Unlock()
	directList.Domain[host] = domainType
}

func (directList *DirectList) loadList(lst []string) {
	for _, d := range lst {
		directList.Domain[d] = domainTypeDirect
	}
}

func (directList *DirectList) GetDirectList() []string {
	lst := make([]string, 0)
	for site, domainType := range directList.Domain {
		if domainType == domainTypeDirect {
			lst = append(lst, site)
		}
	}
	return lst
}

var directList = newDirectList()

func initDirectList() {
	var exists bool
	var err error
	if exists, err = isFileExists(configPath.alwaysDirect); err != nil {
		errl.Printf("Error loading direct domain list: %v\n", err)
	}
	if !exists {
		return
	}
	f, err := os.Open(configPath.alwaysDirect)
	if err != nil {
		errl.Println("Error opening domain list:", err)
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		domain := strings.TrimSpace(scanner.Text())
		if domain == "" {
			continue
		}
		directList.Domain[domain] = domainTypeDirect
	}
	if scanner.Err() != nil {
		errl.Printf("Error reading domain list %s: %v\n", configPath.alwaysDirect, scanner.Err())
	}
}
