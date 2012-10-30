package main

import (
	"bufio"
	"io"
	"os"
	"os/user"
	"path"
	"strings"
)

var homeDir string

const (
	dotDir       = ".cow-proxy"
	blockedFname = "blocked"
)

var config struct {
	dir         string // directory containing config file and blocked site list
	socksAddr   string
	blockedFile string
}

func init() {
	u, err := user.Current()
	if err != nil {
		errl.Printf("Can't get user information %v", err)
		os.Exit(1)
	}
	homeDir = u.HomeDir

	config.socksAddr = "127.0.0.1:1080"
	config.dir = path.Join(homeDir, dotDir)
	config.blockedFile = path.Join(config.dir, blockedFname)
}

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
		// debug.Println(domain)
	}
	return nil
}

func loadConfig() {
	if err := loadBlocked(config.blockedFile); err != nil {
		os.Exit(1)
	}
}
