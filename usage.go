package main

import (
	"os"
	"strings"
	"github.com/cyfdecyf/bufio"
	"errors"
	"time"
	"bytes"
	"fmt"
)

var userUsage struct {
	usage map[string]int
	capacity map[string]int
	lastSavedts time.Time
}

func parseCapacity(line string) (user string, capacity int, err error) {
	arr := strings.Split(line, ":")
	n := len(arr)
	if n != 2 {
		err = errors.New("User capacity limitation: " + line +
		" syntax wrong, should be username:capacity")
		return
	}
	u, c := arr[0], uint32(arr[1])
	return u, c, nil
}

func parseUsage(line string) (user string, usage int, err error) {
	arr := strings.Split(line, ":")
	n := len(arr)
	if n != 2 {
		err = errors.New("Record file format error: " + line +
		" syntax wrong, should be username:usage")
		return
	}
	u, c := arr[0], uint32(arr[1])
	return u, c, nil
}

func loadCapcity(file string) {
	// load capcity first
	if file == "" {
		return
	}
	f, err := os.Open(file)
	if err != nil {
		Fatal("error opening user usage file:", err)
	}

	r := bufio.NewReader(f)
	s := bufio.NewScanner(r)
	for s.Scan() {
		line := s.Text()
		if line == "" {
			continue
		}
		u, c, err := parseCapacity(s.Text())
		if err != nil {
			Fatal(err)
		}
		if _, ok := userUsage.capacity[u]; ok {
			Fatal("duplicate user:", u)
		}
		userUsage.capacity[u] = c
		userUsage.usage[u] = 0

	}
	f.Close()
}

func loadUsage() {
	dir, err := os.Getwd()
	if err != nil {
		Fatal("error opening current directory:", err)
	}
	buf := new(bytes.Buffer)
	fmt.Fprint(buf, dir, "/_records.log")
	f, err := os.OpenFile(buf.String(), os.O_CREATE, 0666)
	if err != nil {
		Fatal("error opening/creating user record file:", err)
	}
	r := bufio.NewReader(f)
	s := bufio.NewScanner(r)
	for s.Scan() {
		line := s.Text()
		if line == "" {
			continue
		}
		u, c, err := parseUsage(s.Text())
		if err != nil {
			Fatal(err)
		}
		if _, ok := userUsage.usage[u]; ok {
			Fatal("duplicate record:", line)
		}
		userUsage.usage[u] = c

	}
	f.Close()
}

func loadUserUsageFile(file string) {
	//load capacity at first
	loadCapcity(file)

	// load usage
	loadUsage()
}


