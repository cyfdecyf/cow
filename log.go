package main

import "log"

type loglevel bool

const debug loglevel = true
const info loglevel = true

func (d loglevel) Printf(format string, args ...interface{}) {
	if d {
		log.Printf(format, args...)
	}
}

func (d loglevel) Println(args ...interface{}) {
	if d {
		log.Println(args...)
	}
}
