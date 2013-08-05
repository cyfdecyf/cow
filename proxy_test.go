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
		{"0\r\n\r\n", ""},
		/*
			{"0\n\r\n", "0\r\n\r\n"}, // test for buggy web servers
			{"1a; ignore-stuff-here\nabcdefghijklmnopqrstuvwxyz\r\n10\n1234567890abcdef\n0\n\n",
				// COW will only sanitize CRLF at chunk ending
				"1a; ignore-stuff-here\nabcdefghijklmnopqrstuvwxyz\r\n10\n1234567890abcdef\r\n0\r\n\r\n"},
		*/
	}

	// supress error log when finding chunk extension
	errl = false
	defer func() {
		errl = true
	}()
	// use different reader buffer size to test for both all buffered and partially buffered chunk
	sizeArr := []int{32, 64, 128}
	for _, size := range sizeArr {
		for _, td := range testData {
			r := bufio.NewReaderSize(strings.NewReader(td.raw), size)
			var w bytes.Buffer

			if err := sendBodyChunked(r, &w, size); err != nil {
				t.Fatalf("sent data %q err: %v\n", w.Bytes(), err)
			}
			if td.want == "" {
				if w.String() != td.raw {
					t.Errorf("sendBodyChunked wrong with buf size %d, raw data is:\n%q\ngot:\n%q\n",
						size, td.raw, w.String())
				}
			} else {
				if w.String() != td.want {
					t.Errorf("sendBodyChunked wrong with buf sizwe %d, raw data is:\n%q\nwant:\n%q\ngot :\n%q\n",
						size, td.raw, td.want, w.String())
				}
			}
		}
	}
}
