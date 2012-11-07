package main

// This trick is learnt from a post by Rob Pike
// https://groups.google.com/d/msg/golang-nuts/gU7oQGoCkmg/j3nNxuS2O_sJ

// For error message, use log pkg directly

import (
	"flag"
	"log"
	"os"
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

	verbose  bool
	colorize bool
)

var (
	errorLog    = log.New(os.Stderr, "\033[31m[Error]\033[0m ", log.LstdFlags)
	debugLog    = log.New(os.Stderr, "\033[34m[Debug]\033[0m ", log.LstdFlags)
	requestLog  = log.New(os.Stderr, "\033[32m[>>>>>]\033[0m ", log.LstdFlags)
	responseLog = log.New(os.Stderr, "\033[33m[<<<<<]\033[0m ", log.LstdFlags)
)

func init() {
	flag.BoolVar((*bool)(&info), "info", true, "info log")
	flag.BoolVar((*bool)(&debug), "debug", true, "debug log")
	flag.BoolVar((*bool)(&errl), "err", true, "error log")
	flag.BoolVar((*bool)(&dbgRq), "reqest", true, "request log")
	flag.BoolVar((*bool)(&dbgRep), "reply", true, "reply log")

	flag.BoolVar(&verbose, "v", false, "More info in request/response logging")
	flag.BoolVar(&colorize, "c", true, "Colorize log output")
}

func initLog() {
	if !colorize {
		errorLog = log.New(os.Stderr, "[ERROR ] ", log.LstdFlags)
		debugLog = log.New(os.Stderr, "[DEBUG ] ", log.LstdFlags)
		requestLog = log.New(os.Stderr, "[Rqst  ] ", log.LstdFlags)
		responseLog = log.New(os.Stderr, "[Rpns  ] ", log.LstdFlags)
	}
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
