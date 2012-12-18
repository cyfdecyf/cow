package main

import (
	"os"
	"path"
)

const (
	rcFname            = "rc.txt"
	blockedFname       = "auto-blocked.txt"
	directFname        = "auto-direct.txt"
	alwaysBlockedFname = "blocked.txt"
	alwaysDirectFname  = "direct.txt"
	chouFname          = "chou.txt"

	newLine = "\r\n"
)

func initConfigDir() {
	// On windows, put the configuration file in the same directory of cow executable
	home := path.Base(os.Args[0])
	if home == os.Args[0] {
		// Invoked in the current directory
		home = ""
	}
	dsFile.dir = home
}
