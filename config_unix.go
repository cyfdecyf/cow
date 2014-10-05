// +build darwin freebsd linux netbsd openbsd

package main

import (
	"path"
)

const (
	rcFname            = "rc"
	alwaysBlockedFname = "blocked"
	alwaysDirectFname  = "direct"
	statFname          = "stat"

	newLine = "\n"
)

func initConfigDir() {
	home := getUserHomeDir()
	configPath.dir = path.Join(home, ".meow")
}
