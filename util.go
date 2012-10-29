package main

import (
	"bufio"
	"strings"
)

// Almost same with net/textproto/reader.go ReadLine
func ReadLine(r *bufio.Reader) (string, error) {
	var line []byte
	for {
		l, more, err := r.ReadLine()
		if err != nil {
			return "", err
		}

		if line == nil && !more {
			return string(l), nil
		}
		line = append(line, l...)
		if !more {
			break
		}
	}
	return string(line), nil
}

func IsDigit(b byte) bool {
	return '0' <= b && b <= '9'
}

func host2Domain(h string) (domain string) {
	host, _ := splitHostPort(h)
	dotPos := strings.LastIndex(host, ".")
	if dotPos == -1 {
		return host // simple host name
	}
	// Find the 2nd last dot
	dotPos = strings.LastIndex(host[:dotPos], ".")
	if dotPos == -1 {
		return host
	}
	return host[dotPos+1:]
}
