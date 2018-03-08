// +build darwin freebsd linux netbsd openbsd

package main

import (
	"path"
)

const (
	rcFname        = "rc"
	blockedFname   = "blocked"
	directFname    = "direct"
	statFname      = "stat"
	autoProxyFname = "autoproxy.pac"

	newLine = "\n"
)

func getDefaultRcFile() string {
	return path.Join(path.Join(getUserHomeDir(), ".cow", rcFname))
}
