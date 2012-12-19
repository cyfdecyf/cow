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
	// This is not a reliable way to detect binary directory, but it works for double click and run
	dsFile.dir = path.Dir(os.Args[0])
}
