// +build darwin freebsd linux netbsd openbsd   

package main

import (
	"log"
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
	home, err := getUserHomeDir()
	if err != nil {
		log.Printf("initConfigDir can't get user home directory: %v", err)
		return
	}
	dsFile.dir = path.Join(home, ".cow")
}
