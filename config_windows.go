package main

import (
	"os"
	"path"
)

const (
	rcFname      = "rc.txt"
	blockedFname = "blocked.txt"
	directFname  = "direct.txt"
	statFname    = "stat.txt"

	newLine = "\r\n"
)

func getDefaultRcFile() string {
	// On windows, put the configuration file in the same directory of cow executable
	// This is not a reliable way to detect binary directory, but it works for double click and run
	return path.Join(path.Dir(os.Args[0]), rcFname)
}
