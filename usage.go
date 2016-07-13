package main

import (
	"os"
	"strings"
	"strconv"
	"github.com/cyfdecyf/bufio"
	"errors"
	"time"
	"bytes"
	"fmt"
	"sync"
	"net"
)

var recordPath string

var userUsage struct {
	usage map[string]int
	capacity map[string]int
	addrToUser map[string]string
	lastSavedts time.Time
}

func parseCapacity(line string) (user string, capacity int, err error) {
	arr := strings.Split(line, ":")
	n := len(arr)
	if n != 2 {
		err = errors.New("User capacity limitation: " + line +
		" syntax wrong, should be username:capacity")
		return "", 0, err
	}
	c, err := strconv.Atoi(arr[1])
	if err != nil {
		err = errors.New("Record file format error: " + arr[1] +
		" syntax wrong, should be int")
		return "", 0, err
	}
	debug.Printf("user: %s, capacity: %d", arr[0], c)
	return arr[0], c, nil
}

func parseUsage(line string) (user string, usage int, err error) {
	arr := strings.Split(line, ":")
	n := len(arr)
	if n != 2 {
		err = errors.New("Record file format error: " + line +
		" syntax wrong, should be username:usage")
		return "", 0, err
	}
	c, err := strconv.Atoi(arr[1])
	if err != nil {
		err = errors.New("Record file format error: " + arr[1] +
		" syntax wrong, should be int")
		return "", 0, err
	}
	debug.Printf("user: %s, usage: %d", arr[0], c)
	return arr[0], c, nil
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
	f, err := os.OpenFile(recordPath, os.O_CREATE, 0600)
	if err != nil {
		Fatal("error opening/creating user record file:", err)
	}
	r := bufio.NewReader(f)
	s := bufio.NewScanner(r)
	for s.Scan() {
		ts := s.Text()
		if ts == "" {
			continue
		}
		if t, e := time.Parse(time.ANSIC, ts); e == nil {
			userUsage.lastSavedts = t
			break
		} else {
			Fatal("incomplete user record, please delete ", recordPath, " and restart: ", e)
			return
		}
	}
	for s.Scan() {
		line := s.Text()
		if line == "" {
			continue
		}
		u, c, err := parseUsage(s.Text())
		if err != nil {
			Fatal(err)
		}
		userUsage.usage[u] = c

	}
	f.Close()
}

func flushLog() {
	if time.Now().Day() == config.UsageResetDate &&
		config.UsageResetDate != -1 &&
		userUsage.lastSavedts.Day() != config.UsageResetDate {
		//it's time to clear the record of last month
		for k, _ := range userUsage.usage {
			userUsage.usage[k] = 0
		}

	}
	bakPath := recordPath + ".bak"
	f, err := os.OpenFile(bakPath, os.O_WRONLY | os.O_CREATE, 0600)
	if err != nil {
		Fatal("error opening/creating user record file:", err)
	}
	w := bufio.NewWriter(f)
	t := time.Now()
	w.WriteString(t.Format(time.ANSIC))
	w.WriteString("\n")
	w.Flush()
	for k, v := range userUsage.usage {
		r := fmt.Sprintf("%s:%d\n", k, v)
		w.WriteString(r)
	}
	w.Flush()
	f.Close()

	os.Remove(recordPath)
	os.Rename(bakPath, recordPath)
	userUsage.lastSavedts = t


}

func startUsageRecorder(wg *sync.WaitGroup, quit <-chan struct{}) {
	defer func() {
		flushLog()
		debug.Println("exit the usage recorder")
		wg.Done()
	}()
	var exit bool
	go func() {
		<-quit
		exit=true
	}()

	debug.Println("start usage recording!")
	interval := 0
	for {
		time.Sleep(1000 * time.Millisecond)
		interval += 1
		if exit {
			break
		}
		if interval > 1800 {
			flushLog()
			interval = 0
		}
	}
}

func initUsage() bool{
	if config.UserPasswdFile == "" ||
		config.UserCapacityFile == ""{
		return false
	}

	if config.UsageResetDate == 0 || config.UsageResetDate > 30 {
		Fatal("wrong UsageResetDate: ", config.UsageResetDate)
	}
	// get current running path
	dir, err := os.Getwd()
	if err != nil {
		Fatal("error opening current directory:", err)
	}
	buf := new(bytes.Buffer)
	fmt.Fprint(buf, dir, "/_records.log")
	recordPath = buf.String()

	userUsage.capacity = make(map[string]int)
	userUsage.usage = make(map[string]int)
	userUsage.addrToUser = make(map[string]string)
	//load capacity at first
	loadCapcity(config.UserCapacityFile)

	// load usage
	loadUsage()
	return true
}

func checkUsage(addr string) bool {
	clientIP, _, _ := net.SplitHostPort(addr)
	var user string
	var capacity int
	var usage int
	if val, ok := userUsage.addrToUser[clientIP]; ok {
		user = val
	} else {
		errl.Println("unkonw address: ", addr)
		return false
	}
	if val, ok := userUsage.capacity[user]; ok {
		capacity = val
	} else {
		errl.Println("unkonw user: ", user)
		return false
	}
	// don't have to check here
	usage = userUsage.usage[user]
	usageInMB := usage / 1024 / 1024
	return (usageInMB < capacity)
}

func accumulateUsage(addr string, size int) {
	clientIP, _, _ := net.SplitHostPort(addr)
	var user string
	if val, ok := userUsage.addrToUser[clientIP]; ok {
		user = val
	} else {
		debug.Println("un recorded addr: ", addr)
		Fatal("un recorded addr: ", addr)
	}
	if _, ok := userUsage.usage[user]; ok {
		if size > 0 {
			userUsage.usage[user] += size
			debug.Printf("user: %s add %d BYTE, total %d", user, size, userUsage.usage[user])
		}
	}


}

func updateAddrToUser(addr string, user string)  {
	userUsage.addrToUser[addr] = user
	// add record
	debug.Println("add addr: ", addr, "to user: ", user)
}

func addAllowedClient(addr string) {
	if _, ok := userUsage.addrToUser[addr]; ok {
		debug.Println("duplicated allowed client ip: ", addr)
		return
	}

	userUsage.addrToUser[addr] = addr
}
