package main

import (
	"bytes"
	"testing"
)

func TestIsDigit(t *testing.T) {
	for i := 0; i < 10; i++ {
		digit := '0' + byte(i)
		letter := 'a' + byte(i)

		if IsDigit(digit) != true {
			t.Errorf("%c should return true", digit)
		}

		if IsDigit(letter) == true {
			t.Errorf("%c should return false", letter)
		}
	}
}

func TestCopyN(t *testing.T) {
	testStr := "hello world"
	src := bytes.NewBufferString(testStr)
	dst := new(bytes.Buffer)
	buf := make([]byte, 5)

	copyN(src, dst, nil, len(testStr), buf, nil, nil)
	if dst.String() != "hello world" {
		t.Error("copy without pre and end failed, got:", dst.String())
	}

	src.Reset()
	dst.Reset()
	src.WriteString(testStr)
	copyN(src, dst, nil, len(testStr), buf, []byte("by cyf "), nil)
	if dst.String() != "by cyf hello world" {
		t.Error("copy with pre no end failed, got:", dst.String())
	}

	src.Reset()
	dst.Reset()
	src.WriteString(testStr)
	copyN(src, dst, nil, len(testStr), buf, []byte("by cyf "), []byte(" welcome"))
	if dst.String() != "by cyf hello world welcome" {
		t.Error("copy with both pre and end failed, got:", dst.String())
	}

	src.Reset()
	dst.Reset()
	src.WriteString(testStr)
	copyN(src, dst, nil, len(testStr), buf, []byte("pre longer then buffer "), []byte(" welcome"))
	if dst.String() != "pre longer then buffer hello world welcome" {
		t.Error("copy with long pre failed, got:", dst.String())
	}

	src.Reset()
	dst.Reset()
	testStr = "34"
	src.WriteString(testStr)
	copyN(src, dst, nil, len(testStr), buf, []byte("12"), []byte(" welcome"))
	if dst.String() != "1234 welcome" {
		t.Error("copy len(pre)+size<bufLen failed, got:", dst.String())
	}

	src.Reset()
	dst.Reset()
	testStr = "2"
	src.WriteString(testStr)
	copyN(src, dst, nil, len(testStr), buf, []byte("1"), []byte("34"))
	if dst.String() != "1234" {
		t.Error("copy len(pre)+size+len(end)<bufLen failed, got:", dst.String())
	}
}
