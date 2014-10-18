package main

import (
	"github.com/cyfdecyf/bufio"
	"os"
	"strings"
)

type DirectList struct {
	Domain map[string]bool
}

func newDirectList() *DirectList {
	return &DirectList{
		Domain: map[string]bool{},
	}
}

func (directList *DirectList) shouldDirect(url *URL) (direct bool) {
	if parentProxy.empty() { // no way to retry, so always visit directly
		return true
	}
	if url.Domain == "" { // simple host or private ip
		return true
	}
	return directList.Domain[url.Host] || directList.Domain[url.Domain]
}

func (directList *DirectList) loadList(lst []string) {
	for _, d := range lst {
		directList.Domain[d] = true
	}
}

func (directList *DirectList) GetDirectList() []string {
	lst := make([]string, 0)
	for site, direct := range directList.Domain {
		if direct {
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
		errl.Printf("Error loading domaint list: %v\n", err)
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
		directList.Domain[domain] = true
	}
	if scanner.Err() != nil {
		errl.Printf("Error reading domain list %s: %v\n", configPath.alwaysDirect, scanner.Err())
	}
}
