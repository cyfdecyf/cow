package main

import (
	"os"
	"os/signal"
	"syscall"
)

func sigHandler() {
	// TODO On Windows, these signals will not be triggered on closing cmd
	// window. How to detect this?
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	for sig := range sigChan {
		// May handle other signals in the future.
		info.Printf("%v caught, exit\n", sig)
		storeSiteStat(siteStatExit)
		// Windows has no SIGUSR1 signal, so relaunching is not supported now.
		/*
			if sig == syscall.SIGUSR1 {
				relaunch = true
			}
		*/
		close(quit)
		break
	}
	/*
		if *cpuprofile != "" {
			pprof.StopCPUProfile()
		}
	*/
}
