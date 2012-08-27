package main

import (
	"bufio"
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
