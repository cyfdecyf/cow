// +build darwin freebsd linux netbsd openbsd

package main

import (
	"path"
)

const (
	rcFname      = "rc"
	directFname  = "direct"
	proxyFname   = "proxy"
	rejectFname  = "reject"

	newLine = "\n"
)

func getDefaultRcFile() string {
	return path.Join(path.Join(getUserHomeDir(), ".meow", rcFname))
}
