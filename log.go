package main

// This trick is learnt from a post by Rob Pike
// https://groups.google.com/d/msg/golang-nuts/gU7oQGoCkmg/j3nNxuS2O_sJ

// For error message, use log pkg directly

import (
	"log"
	"os"
)

const info infoLogging = true
const debug debugLogging = true
const errl errorLogging = true

const dbgRq requestLogging = true
const dbgRep responseLogging = true

// Currently only controls whether request/response should be all printed
const verbose = false

// info logging
type infoLogging bool

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

// debug logging
type debugLogging bool

var debugLog = log.New(os.Stderr, "\033[34m[DEBUG   ", log.LstdFlags)

func (d debugLogging) Printf(format string, args ...interface{}) {
	if d {
		debugLog.Printf("]\033[0m "+format, args...)
	}
}

// error logging
type errorLogging bool

var errorLog = log.New(os.Stderr, "\033[31m[ERROR ", log.LstdFlags)

func (d errorLogging) Printf(format string, args ...interface{}) {
	if d {
		errorLog.Printf("]\033[0m "+format, args...)
	}
}

// request logging
type requestLogging bool

var requestLog = log.New(os.Stderr, "\033[32m[Request ", log.LstdFlags)

func (d requestLogging) Printf(format string, args ...interface{}) {
	if d {
		requestLog.Printf("]\033[0m "+format, args...)
	}
}

// response logging
type responseLogging bool

var responseLog = log.New(os.Stderr, "\033[33m[Reponse ", log.LstdFlags)

func (d responseLogging) Printf(format string, args ...interface{}) {
	if d {
		responseLog.Printf("]\033[0m "+format, args...)
	}
}
