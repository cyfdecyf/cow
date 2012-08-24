package main

// This trick is learnt from a post by Rob Pike
// https://groups.google.com/d/msg/golang-nuts/gU7oQGoCkmg/j3nNxuS2O_sJ

import (
	"log"
	"os"
)

type infoLogging bool
const info infoLogging = true

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
const debug debugLogging = true

var debugLog = log.New(os.Stderr, "\033[34m[DEBUG ", log.LstdFlags)

func (d debugLogging) Printf(format string, args ...interface{}) {
	if d {
		debugLog.Printf("]\033[0m "+format, args...)
	}
}

