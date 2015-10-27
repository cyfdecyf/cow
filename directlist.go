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
	domainTypeReject
)

func newDirectList() *DirectList {
	return &DirectList{
		Domain: map[string]DomainType{},
	}
}

func (directList *DirectList) shouldDirect(url *URL) (domainType DomainType) {
	debug.Printf("judging host: %s", url.Host)
	if parentProxy.empty() { // no way to retry, so always visit directly
		return domainTypeDirect
	}
	if url.Domain == "" { // simple host or private ip
		return domainTypeDirect
	}
	if directList.Domain[url.Host] == domainTypeDirect || directList.Domain[url.Domain] == domainTypeDirect {
		debug.Printf("host or domain should direct")
		return domainTypeDirect
	}

	if directList.Domain[url.Host] == domainTypeProxy || directList.Domain[url.Domain] == domainTypeProxy {
		debug.Printf("host or domain should using proxy")
		return domainTypeProxy
	}

	if directList.Domain[url.Host] == domainTypeReject || directList.Domain[url.Domain] == domainTypeReject {
		debug.Printf("host or domain should reject")
		return domainTypeReject
	}

	if !config.JudgeByIP {
		return domainTypeProxy
	}

	var ip string
	isIP, isPrivate := hostIsIP(url.Host)
	if isIP {
		if isPrivate {
			directList.add(url.Host, domainTypeDirect)
			return domainTypeDirect
		}
		ip = url.Host
	} else {
		hostIPs, err := net.LookupIP(url.Host)
		if err != nil {
			errl.Printf("error looking up host ip %s, err %s", url.Host, err)
			return domainTypeProxy
		}
		ip = hostIPs[0].String()
	}

	if ipShouldDirect(ip) {
		directList.add(url.Host, domainTypeDirect)
		return domainTypeDirect
	} else {
		directList.add(url.Host, domainTypeProxy)
		return domainTypeProxy
	}
}

func (directList *DirectList) add(host string, domainType DomainType) {
	directList.Lock()
	defer directList.Unlock()
	directList.Domain[host] = domainType
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

func initDomainList(domainListFile string, domainType DomainType) {
	var err error
	if err = isFileExists(domainListFile); err != nil {
		return
	}
	f, err := os.Open(domainListFile)
	if err != nil {
		errl.Println("Error opening domain list:", err)
		return
	}
	defer f.Close()

	directList.Lock()
	defer directList.Unlock()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		domain := strings.TrimSpace(scanner.Text())
		if domain == "" {
			continue
		}
		debug.Printf("Loaded domain %s as type %v", domain, domainType)
		directList.Domain[domain] = domainType
	}
	if scanner.Err() != nil {
		errl.Printf("Error reading domain list %s: %v\n", domainListFile, scanner.Err())
	}
}
