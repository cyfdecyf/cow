package main

import (
	"os/exec"
	"time"
)

func runSSH() {
	if config.sshServer == "" {
		return
	}

	_, port := splitHostPort(config.socksAddr)
	// -n redirects stdin from /dev/null
	// -N do not execute remote command
	cmd := exec.Command("ssh", "-n", "-N", "-D", port, config.sshServer)

	for {
		if err := cmd.Run(); err != nil {
			errl.Println("ssh:", err)
		}
		info.Println("ssh exited, reconnect")
		time.Sleep(time.Second)
	}
}
