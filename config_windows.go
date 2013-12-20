package main

import (
	"os"
	"path"
)

const (
	rcFname            = "rc.txt"
	alwaysBlockedFname = "blocked.txt"
	alwaysDirectFname  = "direct.txt"
	statFname          = "stat.txt"

	newLine = "\r\n"
)

func initConfigDir() {
	// On windows, put the configuration file in the same directory of cow executable
	// This is not a reliable way to detect binary directory, but it works for double click and run
	configPath.dir = path.Dir(os.Args[0])
}
