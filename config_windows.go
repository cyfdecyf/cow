package main

import (
	"os"
	"path"
)

const (
	rcFname      = "rc.txt"
	directFname  = "direct.txt"
	proxyFname   = "proxy.txt"

	newLine = "\r\n"
)

func getDefaultRcFile() string {
	// On windows, put the configuration file in the same directory of meow executable
	// This is not a reliable way to detect binary directory, but it works for double click and run
	return path.Join(path.Dir(os.Args[0]), rcFname)
}
