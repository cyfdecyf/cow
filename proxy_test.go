package main

import (
	"bytes"
	"github.com/cyfdecyf/bufio"
	"strings"
	"testing"
)

func TestSendBodyChunked(t *testing.T) {
	testData := []struct {
		raw  string
		want string // empty means same as raw
	}{
		{"1a; ignore-stuff-here\r\nabcdefghijklmnopqrstuvwxyz\r\n10\r\n1234567890abcdef\r\n0\r\n\r\n", ""},
		{"1a; ignore-stuff-here\nabcdefghijklmnopqrstuvwxyz\r\n10\n1234567890abcdef\n0\n\n",
			// COW will only sanitize CRLF at chunk ending
			"1a; ignore-stuff-here\nabcdefghijklmnopqrstuvwxyz\r\n10\n1234567890abcdef\r\n0\r\n\r\n"},
		{"0\r\n\r\n", ""},
		{"0\n\r\n", "0\r\n\r\n"}, // test for buggy web servers
	}

	buf := make([]byte, bufSize)
	for _, td := range testData {
		r := bufio.NewReaderSize(strings.NewReader(td.raw), bufSize)
		var w bytes.Buffer

		if err := sendBodyChunked(buf, r, &w); err != nil {
			t.Fatal("err:", err)
		}
		if td.want == "" {
			if w.String() != td.raw {
				t.Errorf("sendBodyChunked wrong, raw data is:\n%q\ngot:\n%q\n", td.raw, w.String())
			}
		} else {
			if w.String() != td.want {
				t.Errorf("sendBodyChunked wrong, raw data is:\n%q\nwant:\n%q\ngot :\n%q\n",
					td.raw, td.want, w.String())
			}
		}
	}
}
