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
)

var recordPath string

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

func flushLog() {
	bakPath := recordPath + ".bak"
	f, err := os.OpenFile(bakPath, os.O_WRONLY | os.O_CREATE, 0600)
	if err != nil {
		Fatal("error opening/creating user record file:", err)
	}
	w := bufio.NewWriter(f)
	for k, v := range userUsage.usage {
		r := fmt.Sprintf("%s:%d\n", k, v)
		w.WriteString(r)
	}
	w.Flush()
	f.Close()

	os.Remove(recordPath)
	os.Rename(bakPath, recordPath)


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
		if interval > 7200 {
			flushLog()
			interval = 0
		}
	}
}

func initUsage() bool{
	if config.UserPasswdFile == "" ||
		config.UserCapacityFile == "" {
		return false
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
	userUsage.lastSavedts = time.Now()
	//load capacity at first
	loadCapcity(config.UserCapacityFile)

	// load usage
	loadUsage()
	return true
}


