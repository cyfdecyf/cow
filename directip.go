package main

import (
	"sort"
)

func ipShouldDirect(ip string) (direct bool) {
	ipLong, err := ip2long(ip)
	if err != nil {
		return false
	}
	ipIndex := sort.Search(len(CNIPDataStart), func(i int) bool {
		return CNIPDataStart[i] > ipLong
	})
	ipIndex--
	return ipLong <= CNIPDataStart[ipIndex]+(uint32)(CNIPDataNum[ipIndex])
}
