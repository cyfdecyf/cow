package main

import (
	"net"
	"os/exec"
	"time"
)

func SshRunning() bool {
	c, err := net.Dial("tcp", config.SocksAddr)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

func runSSH() {
	if config.SshServer == "" {
		return
	}
	if config.SocksAddr == "" {
		errl.Println("Missing option: ssh server given without socks address")
		return
	}

	_, port := splitHostPort(config.SocksAddr)
	sshServer, sshPort := splitHostPort(config.SshServer)
	if sshPort == "" {
		sshPort = "22"
	}
	alreadyRunPrinted := false

	for {
		if SshRunning() {
			if !alreadyRunPrinted {
				debug.Println("ssh socks server maybe already running, as cow can connect to",
					config.SocksAddr)
				alreadyRunPrinted = true
			}
			// check server liveness in 1 minute
			time.Sleep(60 * time.Second)
			continue
		}

		// -n redirects stdin from /dev/null
		// -N do not execute remote command
		cmd := exec.Command("ssh", "-n", "-N", "-D", port, "-p", sshPort, sshServer)
		if err := cmd.Run(); err != nil {
			debug.Println("ssh:", err)
		}
		// debug.Println("ssh exited, reconnect")
		time.Sleep(5 * time.Second)
		alreadyRunPrinted = false
	}
}
