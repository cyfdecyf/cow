package main

import (
	"net"
	"os/exec"
	"time"
)

func PlonkRunning(socksServer string) bool {
	c, err := net.Dial("tcp", socksServer)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

func runPlonk() {
	//host and listen_port is require
	if config.PlonkHost == "" || config.PlonkListenPort == "" {
		debug.Println("PlonkHost or PlonkListenPort, skip to run plonk")
		return
	}

	socks := "127.0.0.1:"+config.PlonkListenPort

	// parse args
	args := []string {}
	args = append(args, "-C", "-v", "-N")
	args = append(args, "-D", socks)
	args = append(args, config.PlonkHost)

	if config.PlonkPort != "" {
		args = append(args, "-P", config.PlonkPort)
	}
	if config.PlonkUsername != "" {
		args = append(args, "-l", config.PlonkUsername)
	}
	if config.PlonkOfcKeyword != "" {
		args = append(args, "-z", "-Z", config.PlonkOfcKeyword)
	}

	debug.Println("Plonk params: ", args)

	if config.PlonkPassword != "" {
		args = append(args, "-pw", config.PlonkPassword)
	}

	alreadyRunPrinted := false
	for {
		if PlonkRunning(socks) {
			if !alreadyRunPrinted {
				debug.Println("plonk socks server", socks, "maybe already running")
				alreadyRunPrinted = true
			}
			time.Sleep(30 * time.Second)
			continue
		}

		debug.Println("connecting to socks server", socks)
		cmd := exec.Command("plonk", args...)
		if err := cmd.Run(); err != nil {
			debug.Println("plonk:", err)
		}
		debug.Println("plonk listen on", socks, "exited, reconnect")
		time.Sleep(5 * time.Second)
		alreadyRunPrinted = false
	}
}
