package main

import (
	"flag"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	// "syscall"
	// "runtime/pprof"
)

var sigChan = make(chan os.Signal, 1)

// var cpuprofile = flag.String("cpuprofile", "", "write cpu profile to file")

func doExit() {
	writeBlocked()
}

func sigHandler() {
	for sig := range sigChan {
		info.Printf("%v caught, exit\n", sig)
		doExit()
		break
	}
	os.Exit(0)
}

func main() {
	// Parse flags after load config to allow override options in config
	loadConfig()
	flag.Parse()

	if printVer {
		printVersion()
		os.Exit(0)
	}

	/*
		if *cpuprofile != "" {
			f, err := os.Create(*cpuprofile)
			if err != nil {
				info.Println(err)
				os.Exit(1)
			}
			pprof.StartCPUProfile(f)
			signal.Notify(c, os.Interrupt)
			go func() {
				for sig := range c {
					info.Printf("captured %v, stopping profiler and exiting..", sig)
					pprof.StopCPUProfile()
					os.Exit(0)
				}
			}()
		}
	*/

	runtime.GOMAXPROCS(config.numProc)

	signal.Notify(sigChan, syscall.SIGINT)
	signal.Notify(sigChan, syscall.SIGTERM)
	go sigHandler()

	go runSSH()

	py := NewProxy(config.listenAddr)
	py.Serve()
}
