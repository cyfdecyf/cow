package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/user"
	"path"
	// "reflect"
	"strconv"
	"strings"
)

var homeDir string

const (
	dotDir       = ".cow-proxy"
	blockedFname = "blocked"
	rcFname      = "rc"
)

var config struct {
	listenAddr string // server listen address
	socksAddr  string
	numProc    int // max number of cores to use

	dir         string // directory containing config file and blocked site list
	blockedFile string
	rcFile      string
}

func init() {
	u, err := user.Current()
	if err != nil {
		errl.Printf("Can't get user information %v", err)
		os.Exit(1)
	}
	homeDir = u.HomeDir

	config.listenAddr = "127.0.0.1:7777"
	config.numProc = 2
	config.socksAddr = "127.0.0.1:1080"

	config.dir = path.Join(homeDir, dotDir)
	config.blockedFile = path.Join(config.dir, blockedFname)
	config.rcFile = path.Join(config.dir, rcFname)
}

func loadBlocked(fpath string) (err error) {
	f, err := os.Open(fpath)
	if err != nil {
		// No blocked domain file has no problem, report other error though
		if os.IsNotExist(err) {
			return nil
		} else {
			errl.Println("Opening blocked file:", err)
			return
		}
	}
	defer f.Close()
	fr := bufio.NewReader(f)

	for {
		domain, err := ReadLine(fr)
		if err == io.EOF {
			return nil
		} else if err != nil {
			errl.Println("Error reading blocked domains:", err)
			return err
		}
		if domain == "" {
			continue
		}
		blocked[strings.TrimSpace(domain)] = true
		// debug.Println(domain)
	}
	return nil
}

func parseConfig() {
	f, err := os.Open(config.rcFile)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		errl.Println("Opening config file:", err)
	}
	fr := bufio.NewReader(f)

	var line string
	var n int
	for {
		n++
		line, err = ReadLine(fr)
		if err == io.EOF {
			return
		} else if err != nil {
			errl.Println("Error reading rc file:", err)
			errl.Println("Exit")
			os.Exit(1)
		}

		if line == "" {
			continue
		}

		line = strings.TrimSpace(line)
		// Ignore comment
		if line[0] == '#' {
			continue
		}

		v := strings.Split(line, "=")
		if len(v) != 2 {
			fmt.Println("Config error: syntax error on line", n)
			os.Exit(1)
		}
		key, val := strings.TrimSpace(v[0]), strings.TrimSpace(v[1])

		switch {
		case key == "listen":
			config.listenAddr = val
		case key == "core":
			config.numProc, err = strconv.Atoi(val)
			if err != nil {
				fmt.Printf("Config error: core number %d %v", n, err)
				os.Exit(1)
			}
		case key == "socks":
			config.socksAddr = val
		case key == "blocked":
			config.blockedFile = val
		default:
			fmt.Println("Config error: no such option", key)
			os.Exit(1)
		}
	}
}

func loadConfig() {
	parseConfig()

	if err := loadBlocked(config.blockedFile); err != nil {
		os.Exit(1)
	}
}
