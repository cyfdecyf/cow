package main

import (
	"bytes"
	"errors"
	"github.com/cyfdecyf/bufio"
	"strings"
	"testing"
)

func TestASCIIToUpper(t *testing.T) {
	testData := []struct {
		raw   []byte
		upper []byte
	}{
		{[]byte("foobar"), []byte("FOOBAR")},
		{[]byte("fOoBAr"), []byte("FOOBAR")},
		{[]byte("..fOoBAr\n"), []byte("..FOOBAR\n")},
	}
	for _, td := range testData {
		up := ASCIIToUpper(td.raw)
		if !bytes.Equal(up, td.upper) {
			t.Errorf("raw: %s, upper: %s\n", td.raw, up)
		}
	}
}

func TestASCIIToLower(t *testing.T) {
	testData := []struct {
		raw   []byte
		lower []byte
	}{
		{[]byte("FOOBAR"), []byte("foobar")},
		{[]byte("fOoBAr"), []byte("foobar")},
		{[]byte("..fOoBAr\n"), []byte("..foobar\n")},
	}
	for _, td := range testData {
		low := ASCIIToLower(td.raw)
		if !bytes.Equal(low, td.lower) {
			t.Errorf("raw: %s, lower: %s\n", td.raw, low)
		}
	}
}

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

func TestIsSpace(t *testing.T) {
	testData := []struct {
		c  byte
		is bool
	}{
		{'a', false},
		{'B', false},
		{'z', false},
		{'(', false},
		{'}', false},
		{' ', true},
		{'\r', true},
		{'\t', true},
		{'\n', true},
	}
	for _, td := range testData {
		if IsSpace(td.c) != td.is {
			t.Errorf("%v isspace wrong", rune(td.c))
		}
	}
}

func TestTrimSpace(t *testing.T) {
	testData := []struct {
		old    string
		trimed string
	}{
		{"hello", "hello"},
		{" hello", "hello"},
		{"  hello\r\n ", "hello"},
		{"  hello \t  ", "hello"},
		{"", ""},
		{"\r\n", ""},
	}
	for _, td := range testData {
		trimed := string(TrimSpace([]byte(td.old)))
		if trimed != td.trimed {
			t.Errorf("%s trimmed to %s, wrong", td.old, trimed)
		}
	}
}

func TestTrimTrailingSpace(t *testing.T) {
	testData := []struct {
		old    string
		trimed string
	}{
		{"hello", "hello"},
		{" hello", " hello"},
		{"  hello\r\n ", "  hello"},
		{"  hello \t  ", "  hello"},
		{"", ""},
		{"\r\n", ""},
	}
	for _, td := range testData {
		trimed := string(TrimTrailingSpace([]byte(td.old)))
		if trimed != td.trimed {
			t.Errorf("%s trimmed to %s, should be %s\n", td.old, trimed, td.trimed)
		}
	}
}

func TestFieldsN(t *testing.T) {
	testData := []struct {
		raw string
		n   int
		arr []string
	}{
		{"", 2, nil}, // this should not crash
		{"hello world", -1, nil},
		{"hello \t world welcome", 1, []string{"hello \t world welcome"}},
		{"   hello \t world welcome ", 1, []string{"hello \t world welcome"}},
		{"hello world", 2, []string{"hello", "world"}},
		{"  hello\tworld  ", 2, []string{"hello", "world"}},
		// note \r\n in the middle of a string will be considered as a field
		{"  hello  world  \r\n", 4, []string{"hello", "world"}},
		{" hello \t world welcome\r\n", 2, []string{"hello", "world welcome"}},
		{" hello \t world welcome \t ", 2, []string{"hello", "world welcome"}},
	}

	for _, td := range testData {
		arr := FieldsN([]byte(td.raw), td.n)
		if len(arr) != len(td.arr) {
			t.Fatalf("%q want %d fields, got %d\n", td.raw, len(td.arr), len(arr))
		}
		for i := 0; i < len(arr); i++ {
			if string(arr[i]) != td.arr[i] {
				t.Errorf("%q %d item, want %q, got %q\n", td.raw, i, td.arr[i], arr[i])
			}
		}
	}
}

func TestParseIntFromBytes(t *testing.T) {
	errDummy := errors.New("dummy error")
	testData := []struct {
		raw  []byte
		base int
		err  error
		val  int64
	}{
		{[]byte("123"), 10, nil, 123},
		{[]byte("+123"), 10, nil, 123},
		{[]byte("-123"), 10, nil, -123},
		{[]byte("0"), 10, nil, 0},
		{[]byte("a"), 10, errDummy, 0},
		{[]byte("aBc"), 16, nil, 0xabc},
		{[]byte("+aBc"), 16, nil, 0xabc},
		{[]byte("-aBc"), 16, nil, -0xabc},
		{[]byte("213e"), 16, nil, 0x213e},
		{[]byte("12deadbeef"), 16, nil, 0x12deadbeef},
		{[]byte("213n"), 16, errDummy, 0},
	}
	for _, td := range testData {
		val, err := ParseIntFromBytes(td.raw, td.base)
		if err != nil && td.err == nil {
			t.Errorf("%s base %d should NOT return error: %v\n", td.raw, td.base, err)
		}
		if err == nil && td.err != nil {
			t.Errorf("%s base %d should return error\n", td.raw, td.base)
		}
		if val != td.val {
			t.Errorf("%s base %d got wrong value: %d\n", td.raw, td.base, val)
		}
	}
}

func TestCopyN(t *testing.T) {
	testStr := "go is really a nice language"
	for _, step := range []int{4, 9, 17, 32} {
		src := bufio.NewReader(strings.NewReader(testStr))
		dst := new(bytes.Buffer)

		err := copyN(dst, src, len(testStr), step)
		if err != nil {
			t.Error("unexpected err:", err)
			break
		}
		if dst.String() != testStr {
			t.Errorf("step %d want %q, got: %q\n", step, testStr, dst.Bytes())
		}
	}
}

func TestCopyNWithBuf(t *testing.T) {
	testStr := "hello world"
	src := bytes.NewBufferString(testStr)
	dst := new(bytes.Buffer)
	buf := make([]byte, 5)

	copyNWithBuf(dst, src, len(testStr), buf, nil, nil)
	if dst.String() != "hello world" {
		t.Error("copy without pre and end failed, got:", dst.String())
	}

	src.Reset()
	dst.Reset()
	src.WriteString(testStr)
	copyNWithBuf(dst, src, len(testStr), buf, []byte("by cyf "), nil)
	if dst.String() != "by cyf hello world" {
		t.Error("copy with pre no end failed, got:", dst.String())
	}

	src.Reset()
	dst.Reset()
	src.WriteString(testStr)
	copyNWithBuf(dst, src, len(testStr), buf, []byte("by cyf "), []byte(" welcome"))
	if dst.String() != "by cyf hello world welcome" {
		t.Error("copy with both pre and end failed, got:", dst.String())
	}

	src.Reset()
	dst.Reset()
	src.WriteString(testStr)
	copyNWithBuf(dst, src, len(testStr), buf, []byte("pre longer then buffer "), []byte(" welcome"))
	if dst.String() != "pre longer then buffer hello world welcome" {
		t.Error("copy with long pre failed, got:", dst.String())
	}

	src.Reset()
	dst.Reset()
	testStr = "34"
	src.WriteString(testStr)
	copyNWithBuf(dst, src, len(testStr), buf, []byte("12"), []byte(" welcome"))
	if dst.String() != "1234 welcome" {
		t.Error("copy len(pre)+size<bufLen failed, got:", dst.String())
	}

	src.Reset()
	dst.Reset()
	testStr = "2"
	src.WriteString(testStr)
	copyNWithBuf(dst, src, len(testStr), buf, []byte("1"), []byte("34"))
	if dst.String() != "1234" {
		t.Error("copy len(pre)+size+len(end)<bufLen failed, got:", dst.String())
	}
}

func TestIsFileExists(t *testing.T) {
	exists, err := isFileExists("testdata")
	if err == nil {
		t.Error("should return error is path is directory")
	}
	if exists {
		t.Error("directory should return false")
	}

	exists, err = isFileExists("testdata/none")
	if exists {
		t.Error("BOOM! You've found a non-existing file!")
	}
	if err != nil {
		t.Error("Not existing file should just return false, on error")
	}

	exists, err = isFileExists("testdata/file")
	if !exists {
		t.Error("testdata/file exists, but returns false")
	}
	if err != nil {
		t.Error("Why error for existing file?")
	}
}

func TestNewNbitIPv4Mask(t *testing.T) {
	mask := []byte(NewNbitIPv4Mask(32))
	for i := 0; i < 4; i++ {
		if mask[i] != 0xff {
			t.Error("NewNbitIPv4Mask with 32 error")
		}
	}
	mask = []byte(NewNbitIPv4Mask(5))
	if mask[0] != 0xf8 || mask[1] != 0 || mask[2] != 0 {
		t.Error("NewNbitIPv4Mask with 5 error:", mask)
	}
	mask = []byte(NewNbitIPv4Mask(9))
	if mask[0] != 0xff || mask[1] != 0x80 || mask[2] != 0 {
		t.Error("NewNbitIPv4Mask with 9 error:", mask)
	}
	mask = []byte(NewNbitIPv4Mask(23))
	if mask[0] != 0xff || mask[1] != 0xff || mask[2] != 0xfe || mask[3] != 0 {
		t.Error("NewNbitIPv4Mask with 23 error:", mask)
	}
	mask = []byte(NewNbitIPv4Mask(28))
	if mask[0] != 0xff || mask[1] != 0xff || mask[2] != 0xff || mask[3] != 0xf0 {
		t.Error("NewNbitIPv4Mask with 28 error:", mask)
	}
}

func TestHost2Domain(t *testing.T) {
	var testData = []struct {
		host   string
		domain string
	}{
		{"www.google.com", "google.com"},
		{"google.com", "google.com"},
		{"com.cn", "com.cn"},
		{"sina.com.cn", "sina.com.cn"},
		{"www.bbc.co.uk", "bbc.co.uk"},
		{"apple.com.cn", "apple.com.cn"},
		{"simplehost", ""},
		{"192.168.1.1", ""},
		{"10.2.1.1", ""},
		{"123.45.67.89", "123.45.67.89"},
		{"172.65.43.21", "172.65.43.21"},
	}

	for _, td := range testData {
		dm := host2Domain(td.host)
		if dm != td.domain {
			t.Errorf("%s got domain %v should be %v", td.host, dm, td.domain)
		}
	}
}

func TestHostIsIP(t *testing.T) {
	var testData = []struct {
		host  string
		isIP  bool
		isPri bool
	}{
		{"127.0.0.1", true, true},
		{"127.2.1.1", true, true},
		{"192.168.1.1", true, true},
		{"10.2.3.4", true, true},
		{"172.16.5.3", true, true},
		{"172.20.5.3", true, true},
		{"172.31.5.3", true, true},
		{"172.15.1.1", true, false},
		{"123.45.67.89", true, false},
		{"foo.com", false, false},
		{"www.foo.com", false, false},
		{"www.bar.foo.com", false, false},
	}

	for _, td := range testData {
		isIP, isPri := hostIsIP(td.host)
		if isIP != td.isIP {
			if td.isIP {
				t.Error(td.host, "is IP address")
			} else {
				t.Error(td.host, "is NOT IP address")
			}
		}
		if isPri != td.isPri {
			if td.isPri {
				t.Error(td.host, "is private IP address")
			} else {
				t.Error(td.host, "is NOT private IP address")
			}
		}
	}
}
