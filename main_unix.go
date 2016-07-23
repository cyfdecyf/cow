// +build darwin freebsd linux netbsd openbsd

package main

import (
	"os"
	"os/signal"
	"syscall"
	"time"
	"sync"
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

func restartDeamon(pid int, wg *sync.WaitGroup, quit <-chan struct{}) {
	defer func() {
		wg.Done()
	}()

	duration := int(config.RestartInterval.Seconds())
	interval := 0
	debug.Println("Pid: ", pid, "restart interval: ", duration)
	for {
		select {
		case <- quit:
			debug.Println("exit the restart deamon")
			return
		default:
			time.Sleep(time.Second)
			interval += 1
			if (interval > duration) {
				info.Println("Restart proxy now!")
				// connPool.CloseAll()
				syscall.Kill(pid, syscall.SIGUSR1)
				return
			}
		}
	}


}
