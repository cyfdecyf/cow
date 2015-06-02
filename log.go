package main

// This logging trick is learnt from a post by Rob Pike
// https://groups.google.com/d/msg/golang-nuts/gU7oQGoCkmg/j3nNxuS2O_sJ

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/cyfdecyf/color"
)

type infoLogging bool
type debugLogging bool
type errorLogging bool
type requestLogging bool
type responseLogging bool

var (
	info   infoLogging
	debug  debugLogging
	errl   errorLogging
	dbgRq  requestLogging
	dbgRep responseLogging

	logFile io.Writer

	// make sure logger can be called before initLog
	errorLog    = log.New(os.Stdout, "[ERROR] ", log.LstdFlags)
	debugLog    = log.New(os.Stdout, "[DEBUG] ", log.LstdFlags)
	requestLog  = log.New(os.Stdout, "[>>>>>] ", log.LstdFlags)
	responseLog = log.New(os.Stdout, "[<<<<<] ", log.LstdFlags)

	verbose  bool
	colorize bool
)

func init() {
	flag.BoolVar((*bool)(&info), "info", true, "info log")
	flag.BoolVar((*bool)(&debug), "debug", false, "debug log, with this option, log goes to stdout with color")
	flag.BoolVar((*bool)(&errl), "err", true, "error log")
	flag.BoolVar((*bool)(&dbgRq), "request", false, "request log")
	flag.BoolVar((*bool)(&dbgRep), "reply", false, "reply log")
	flag.BoolVar(&verbose, "v", false, "more info in request/response logging")
	flag.BoolVar(&colorize, "color", false, "colorize log output")
}

func initLog() {
	logFile = os.Stdout
	if config.LogFile != "" {
		if f, err := os.OpenFile(expandTilde(config.LogFile),
			os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600); err != nil {
			fmt.Printf("Can't open log file, logging to stdout: %v\n", err)
		} else {
			logFile = f
		}
	}
	log.SetOutput(logFile)
	if colorize {
		color.SetDefaultColor(color.ANSI)
	} else {
		color.SetDefaultColor(color.NoColor)
	}
	errorLog = log.New(logFile, color.Red("[ERROR] "), log.LstdFlags)
	debugLog = log.New(logFile, color.Blue("[DEBUG] "), log.LstdFlags)
	requestLog = log.New(logFile, color.Green("[>>>>>] "), log.LstdFlags)
	responseLog = log.New(logFile, color.Yellow("[<<<<<] "), log.LstdFlags)
}

func (d infoLogging) Printf(format string, args ...interface{}) {
	if d {
		log.Printf(format, args...)
	}
}

func (d infoLogging) Println(args ...interface{}) {
	if d {
		log.Println(args...)
	}
}

func (d debugLogging) Printf(format string, args ...interface{}) {
	if d {
		debugLog.Printf(format, args...)
	}
}

func (d debugLogging) Println(args ...interface{}) {
	if d {
		debugLog.Println(args...)
	}
}

func (d errorLogging) Printf(format string, args ...interface{}) {
	if d {
		errorLog.Printf(format, args...)
	}
}

func (d errorLogging) Println(args ...interface{}) {
	if d {
		errorLog.Println(args...)
	}
}

func (d requestLogging) Printf(format string, args ...interface{}) {
	if d {
		requestLog.Printf(format, args...)
	}
}

func (d responseLogging) Printf(format string, args ...interface{}) {
	if d {
		responseLog.Printf(format, args...)
	}
}

func Fatal(args ...interface{}) {
	fmt.Println(args...)
	os.Exit(1)
}

func Fatalf(format string, args ...interface{}) {
	fmt.Printf(format, args...)
	os.Exit(1)
}
