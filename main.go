package main

import (
	// "flag"
	"os"
	"os/signal"
	"runtime"
	// "runtime/pprof"
	"sync"
	"syscall"
)

// var cpuprofile = flag.String("cpuprofile", "", "write cpu profile to file")

func sigHandler() {
	// TODO On Windows, these signals will not be triggered on closing cmd
	// window. How to detect this?
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM,
		syscall.SIGHUP)

	for sig := range sigChan {
		// May handle other signals in the future.
		info.Printf("%v caught, exit\n", sig)
		storeSiteStat(siteStatExit)
		break
	}
	/*
		if *cpuprofile != "" {
			pprof.StopCPUProfile()
		}
	*/
	os.Exit(0)
}

func main() {
	// Parse flags after load config to allow override options in config
	cmdLineConfig := parseCmdLineConfig()
	if cmdLineConfig.PrintVer {
		printVersion()
		os.Exit(0)
	}

	parseConfig(cmdLineConfig.RcFile, cmdLineConfig)

	initSelfListenAddr()
	initLog()
	initAuth()
	initSiteStat()
	initPAC() // initPAC uses siteStat, so must init after site stat

	initStat()

	if !hasParentProxy() {
		info.Println("no parent proxy server")
	}
	if debug {
		printParentProxy()
	}

	/*
		if *cpuprofile != "" {
			f, err := os.Create(*cpuprofile)
			if err != nil {
				Fatal(err)
			}
			pprof.StartCPUProfile(f)
		}
	*/

	if config.Core > 0 {
		runtime.GOMAXPROCS(config.Core)
	}

	go sigHandler()
	go runSSH()
	if config.EstimateTimeout {
		go runEstimateTimeout()
	} else {
		info.Println("timeout estimation disabled")
	}

	var wg sync.WaitGroup
	wg.Add(len(listenProxy))
	for _, proxy := range listenProxy {
		go proxy.Serve(&wg)
	}
	wg.Wait()
}
