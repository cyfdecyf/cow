package main

import (
	"bufio"
	"io"
	"os"
	"strings"
)

var blocked = map[string]bool{}

func loadBlocked(fpath string) (err error) {
	f, err := os.Open(fpath)
	if err != nil {
		// No blocked domain file has no problem
		return
	}
	fr := bufio.NewReader(f)

	for {
		domain, err := ReadLine(fr)
		if err == io.EOF {
			return nil
		} else if err != nil {
			errl.Printf("Error reading blocked domains %v", err)
			return err
		}
		if domain == "" {
			continue
		}
		blocked[strings.TrimSpace(domain)] = true
		debug.Println(domain)
	}
	return nil
}

func isDomainBlocked(domain string) bool {
	_, ok := blocked[domain]
	return ok
}

func isRequestBlocked(r *Request) bool {
	return isDomainBlocked(host2Domain(r.URL.Host))
}
