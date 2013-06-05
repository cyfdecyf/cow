package main

import (
	// "flag"
	"os"
	"os/signal"
	"runtime"
	// "runtime/pprof"
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
		info.Printf("%v caught, exit\n", sig)
		storeSiteStat()
		break
	}
	/*
		if *cpuprofile != "" {
			pprof.StopCPUProfile()
		}
	*/
	os.Exit(0)
}

var hasParentProxy = false

func main() {
	// Parse flags after load config to allow override options in config
	cmdLineConfig := parseCmdLineConfig()
	if cmdLineConfig.PrintVer {
		printVersion()
		os.Exit(0)
	}

	parseConfig(cmdLineConfig.RcFile)
	updateConfig(cmdLineConfig)
	checkConfig()

	initLog()
	initAuth()
	initSiteStat()
	initPAC() // initPAC uses siteStat, so must init after site stat

	if len(parentProxy) == 0 {
		info.Println("no parent proxy server, can't handle blocked sites")
	} else {
		hasParentProxy = true
		if debug {
			printParentProxy()
		}
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
	// go runSSH()
	go runEstimateTimeout()

	done := make(chan byte, 1)
	// save 1 goroutine (a few KB) for the common case with only 1 listen address
	if len(config.ListenAddr) > 1 {
		for i, addr := range config.ListenAddr[1:] {
			go NewProxy(addr, config.AddrInPAC[i+1]).Serve(done)
		}
	}
	NewProxy(config.ListenAddr[0], config.AddrInPAC[0]).Serve(done)
	for i := 0; i < len(config.ListenAddr); i++ {
		<-done
	}
}
