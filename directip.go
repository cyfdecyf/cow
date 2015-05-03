package main

import (
	"sort"
)

func ipShouldDirect(ip string) (direct bool) {
	direct = false
	defer func() {
		if r := recover(); r != nil {
			errl.Printf("error judging ip should direct: %s", ip)
		}
	}()
	_, isPrivate := hostIsIP(ip)
	if isPrivate {
		return true
	}
	ipLong, err := ip2long(ip)
	if err != nil {
		return false
	}
	if ipLong == 0 {
		return true
	}
	ipIndex := sort.Search(len(CNIPDataStart), func(i int) bool {
		return CNIPDataStart[i] > ipLong
	})
	ipIndex--
	return ipLong <= CNIPDataStart[ipIndex]+(uint32)(CNIPDataNum[ipIndex])
}
