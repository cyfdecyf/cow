package main

// This trick is learnt from a post by Rob Pike
// https://groups.google.com/d/msg/golang-nuts/gU7oQGoCkmg/j3nNxuS2O_sJ

// For error message, use log pkg directly

import (
	"log"
	"os"
)

// Currently only controls whether request/response should be all printed
const (
	verbose  = false
	colorize = true
)

type infoLogging bool
type debugLogging bool
type errorLogging bool
type requestLogging bool
type responseLogging bool

const (
	info   infoLogging     = true
	debug  debugLogging    = true
	errl   errorLogging    = true
	dbgRq  requestLogging  = true
	dbgRep responseLogging = true
)

var (
	errorLog    = log.New(os.Stderr, "\033[31m[Error ]\033[0m ", log.LstdFlags)
	debugLog    = log.New(os.Stderr, "\033[34m[Debug ]\033[0m ", log.LstdFlags)
	requestLog  = log.New(os.Stderr, "\033[32m[Rqst  ]\033[0m ", log.LstdFlags)
	responseLog = log.New(os.Stderr, "\033[33m[Rpns  ]\033[0m ", log.LstdFlags)
)

func init() {
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
