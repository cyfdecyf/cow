// +build darwin freebsd linux netbsd openbsd

package main

import (
	"path"
)

const (
	rcFname            = "rc"
	blockedFname       = "auto-blocked"
	directFname        = "auto-direct"
	alwaysBlockedFname = "blocked"
	alwaysDirectFname  = "direct"
	chouFname          = "chou"

	newLine = "\n"
)

func initConfigDir() {
	home := getUserHomeDir()
	dsFile.dir = path.Join(home, ".cow")
}
