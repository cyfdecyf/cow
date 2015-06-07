// +build darwin freebsd linux netbsd openbsd

package main

import (
	"os"
	"os/signal"
	"syscall"
)

func sigHandler() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR1)

	for sig := range sigChan {
		// May handle other signals in the future.
		info.Printf("%v caught, exit\n", sig)
		storeSiteStat(siteStatExit)
		if sig == syscall.SIGUSR1 {
			relaunch = true
		}
		close(quit)
		break
	}
	/*
		if *cpuprofile != "" {
			pprof.StopCPUProfile()
		}
	*/
}
