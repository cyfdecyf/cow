package main

import (
	"fmt"
	"os"
	"runtime"
	"sync"
)

func main() {
	// Parse flags after load config to allow override options in config
	cmdLineConfig := parseCmdLineConfig()
	if cmdLineConfig.PrintVer {
		printVersion()
		os.Exit(0)
	}

	fmt.Printf(`
       /\
   )  ( ')     MEOW Proxy %s
  (  /  )      http://meowproxy.me
   \(__)|      
	`, version)
	fmt.Println()

	parseConfig(cmdLineConfig.RcFile, cmdLineConfig)

	initSelfListenAddr()
	initLog()
	initAuth()
	initSiteStat()
	initPAC() // initPAC uses siteStat, so must init after site stat

	initStat()

	initParentPool()

	if config.DialTimeout > 0 {
		dialTimeout = config.DialTimeout
	}

	if config.ReadTimeout > 0 {
		readTimeout = config.ReadTimeout
	}

	if config.Core > 0 {
		runtime.GOMAXPROCS(config.Core)
	}

	go runSSH()

	var wg sync.WaitGroup
	wg.Add(len(listenProxy))
	for _, proxy := range listenProxy {
		go proxy.Serve(&wg)
	}
	wg.Wait()
}
