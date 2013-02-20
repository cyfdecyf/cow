package main

import (
	"bytes"
	"github.com/cyfdecyf/bufio"
	"strings"
	"testing"
)

func TestSendBodyChunked(t *testing.T) {
	testData := []struct {
		raw string
	}{
		{"1a; ignore-stuff-here\r\nabcdefghijklmnopqrstuvwxyz\r\n10\r\n1234567890abcdef\r\n0\r\n\r\n"},
		{"0\r\n\r\n"},
	}

	buf := make([]byte, bufSize)
	for _, td := range testData {
		r := bufio.NewReaderSize(strings.NewReader(td.raw), bufSize)
		var w bytes.Buffer

		if err := sendBodyChunked(buf, r, &w); err != nil {
			t.Fatal("err:", err)
		}
		if w.String() != td.raw {
			t.Errorf("sendBodyChunked wrong, raw data is:\n%q\ngot:\n%q\n", td.raw, w.String())
		}
	}
}
