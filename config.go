package main

import (
	"os"
	"os/user"
	"path"
)

var homeDir string
var confDir string

const (
	dotDir       = ".cow-proxy"
	blockedFName = "blocked"
)

func loadConfig() {
	u, err := user.Current()
	if err != nil {
		errl.Printf("Can't get user information %v", err)
		os.Exit(1)
	}

	homeDir = u.HomeDir
	confDir = path.Join(homeDir, dotDir)

	if err = loadBlocked(path.Join(confDir, blockedFName)); err != nil {
		os.Exit(1)
	}
}
