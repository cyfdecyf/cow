package main

import (
	"bufio"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"runtime"
	"strconv"
	"strings"
)

type notification chan byte

func newNotification() notification {
	// Notification channle has size 1, so sending a single one will not block
	return make(chan byte, 1)
}

func (n notification) notify() {
	n <- 1
}

func (n notification) hasNotified() bool {
	select {
	case <-n:
		return true
	default:
		return false
	}
	return false
}

// ReadLine read till '\n' is found or encounter error. The returned line does
// not include ending '\r' and '\n'. If returns err != nil if and only if
// len(line) == 0.
func ReadLine(r *bufio.Reader) (string, error) {
	l, err := ReadLineBytes(r)
	return string(l), err
}

// ReadLineBytes read till '\n' is found or encounter error. The returned line
// does not include ending '\r\n' or '\n'. Returns err != nil if and only if
// len(line) == 0. Note the returned byte should not be used for append and
// maybe overwritten by next I/O operation. Copied code of readLineSlice from
// $GOROOT/src/pkg/net/textproto/reader.go
func ReadLineBytes(r *bufio.Reader) (line []byte, err error) {
	for {
		l, more, err := r.ReadLine()
		if err != nil {
			return nil, err
		}
		// Avoid the copy if the first call produced a full line.
		if line == nil && !more {
			return l, nil
		}
		line = append(line, l...)
		if !more {
			break
		}
	}
	return line, nil
}

func ASCIIToUpperInplace(b []byte) {
	for i := 0; i < len(b); i++ {
		if 97 <= b[i] && b[i] <= 122 {
			b[i] -= 32
		}
	}
}

func ASCIIToUpper(b []byte) []byte {
	buf := make([]byte, len(b))
	copy(buf, b)
	ASCIIToUpperInplace(buf)
	return buf
}

func ASCIIToLowerInplace(b []byte) {
	for i := 0; i < len(b); i++ {
		if 65 <= b[i] && b[i] <= 90 {
			b[i] += 32
		}
	}
}

func ASCIIToLower(b []byte) []byte {
	buf := make([]byte, len(b))
	copy(buf, b)
	ASCIIToLowerInplace(buf)
	return buf
}

func IsDigit(b byte) bool {
	return '0' <= b && b <= '9'
}

var spaceTbl = [...]bool{
	9:  true, // ht
	10: true, // lf
	13: true, // cr
	32: true, // sp
}

func IsSpace(b byte) bool {
	if 9 <= b && b <= 32 {
		return spaceTbl[b]
	}
	return false
}

func TrimSpace(s []byte) []byte {
	if len(s) == 0 {
		return s
	}
	st := 0
	end := len(s) - 1
	for ; st < len(s) && IsSpace(s[st]); st++ {
	}
	if st == len(s) {
		return s[:0]
	}
	for ; end >= 0 && IsSpace(s[end]); end-- {
	}
	return s[st : end+1]
}

// ParseIntFromBytes parse hexidecimal number from given bytes.
// No prefix (e.g. 0xdeadbeef) should given.
// base can only be 10 or 16.
func ParseIntFromBytes(b []byte, base int) (n int64, err error) {
	// Currently, one have to convert []byte to string to use strconv
	// Refer to: http://code.google.com/p/go/issues/detail?id=2632
	// That's why I created this function.
	if base != 10 && base != 16 {
		err = errors.New(fmt.Sprintf("Invalid base: %d\n", base))
		return
	}
	if len(b) == 0 {
		err = errors.New("Parse int from empty string")
		return
	}

	neg := false
	if b[0] == '+' {
		b = b[1:]
	} else if b[0] == '-' {
		b = b[1:]
		neg = true
	}

	for _, d := range b {
		var v byte
		switch {
		case '0' <= d && d <= '9':
			v += d - '0'
		case 'a' <= d && d <= 'f':
			v += d - 'a' + 10
		case 'A' <= d && d <= 'F':
			v += d - 'A' + 10
		default:
			n = 0
			err = errors.New(fmt.Sprintf("Invalid number: %s", b))
			return
		}
		if int(v) >= base {
			n = 0
			err = errors.New(fmt.Sprintf("Invalid base %d number: %s", base, b))
			return
		}
		n *= int64(base)
		n += int64(v)
	}
	if neg {
		n = -n
	}
	return
}

func isWindows() bool {
	return runtime.GOOS == "windows"
}

func isFileExists(path string) (bool, error) {
	stat, err := os.Stat(path)
	if err == nil {
		if stat.Mode()&os.ModeType == 0 {
			return true, nil
		}
		return false, errors.New(path + " exists but is not regular file")
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func isDirExists(path string) (bool, error) {
	stat, err := os.Stat(path)
	if err == nil {
		if stat.IsDir() {
			return true, nil
		}
		return false, errors.New(path + " exists but is not directory")
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// Get host IP address
func hostIP() (addrs []string, err error) {
	name, err := os.Hostname()
	if err != nil {
		fmt.Printf("Error get host name: %v\n", err)
		return
	}

	addrs, err = net.LookupHost(name)
	if err != nil {
		fmt.Printf("Error getting host IP address: %v\n", err)
		return
	}
	return
}

func trimLastDot(s string) string {
	if len(s) > 0 && s[len(s)-1] == '.' {
		return s[:len(s)-1]
	}
	return s
}

func getUserHomeDir() string {
	home := os.Getenv("HOME")
	if home == "" {
		fmt.Println("HOME environment variable is empty")
	}
	return home
}

func expandTilde(pth string) string {
	if len(pth) > 0 && pth[0] == '~' {
		home := getUserHomeDir()
		return path.Join(home, pth[1:])
	}
	return pth
}

// copyN copys N bytes from r to w, using the specified buf as buffer. pre and
// end are written to w before and after the n bytes. copyN will try to
// minimize number of writes.
func copyN(r io.Reader, w io.Writer, n int, buf, pre, end []byte) (err error) {
	// XXX well, this is complicated in order to save writes
	var nn int
	bufLen := len(buf)
	var b []byte
	for n != 0 {
		if pre != nil {
			if len(pre) >= bufLen {
				// pre is larger than bufLen, can't save write operation here
				if _, err = w.Write(pre); err != nil {
					return
				}
				pre = nil
				continue
			}
			// append pre to buf to save one write
			copy(buf, pre)
			if len(pre)+n < bufLen {
				// only need to read n bytes
				b = buf[len(pre) : len(pre)+n]
			} else {
				b = buf[len(pre):]
			}
		} else {
			if n < bufLen {
				b = buf[:n]
			} else {
				b = buf
			}
		}
		if nn, err = r.Read(b); err != nil {
			return
		}
		n -= nn
		if pre != nil {
			// nn is how much we need to write next
			nn += len(pre)
			pre = nil
		}
		// see if we can append end in buffer to save one write
		if n == 0 && end != nil && nn+len(end) <= bufLen {
			copy(buf[nn:], end)
			nn += len(end)
			end = nil
		}
		if _, err = w.Write(buf[:nn]); err != nil {
			return
		}
	}
	if end != nil {
		if _, err = w.Write(end); err != nil {
			return
		}
	}
	return
}

func md5sum(ss ...string) string {
	h := md5.New()
	for _, s := range ss {
		io.WriteString(h, s)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// only handles IPv4 address now
func hostIsIP(host string) bool {
	parts := strings.Split(host, ".")
	if len(parts) != 4 {
		return false
	}
	for _, i := range parts {
		if len(i) == 0 || len(i) > 3 {
			return false
		}
		n, err := strconv.Atoi(i)
		if err != nil || n < 0 || n > 255 {
			return false
		}
	}
	return true
}

// NetNbitIPv4Mask returns a IPMask with highest n bit set.
func NewNbitIPv4Mask(n int) net.IPMask {
	if n > 32 {
		panic("NewNbitIPv4Mask: bit number > 32")
	}
	mask := []byte{0, 0, 0, 0}
	for id := 0; id < 4; id++ {
		if n >= 8 {
			mask[id] = 0xff
		} else {
			mask[id] = ^byte(1<<(uint8(8-n)) - 1)
			break
		}
		n -= 8
	}
	return net.IPMask(mask)
}

var topLevelDomain = map[string]bool{
	"ac":  true,
	"co":  true,
	"org": true,
	"com": true,
	"net": true,
	"edu": true,
}

// host2Domain returns the domain of a host. It will recognize domains like
// google.com.hk. Returns empty string for simple host.
func host2Domain(host string) (domain string) {
	host, _ = splitHostPort(host)
	if hostIsIP(host) {
		return ""
	}
	host = trimLastDot(host)
	lastDot := strings.LastIndex(host, ".")
	if lastDot == -1 {
		return ""
	}
	// Find the 2nd last dot
	dot2ndLast := strings.LastIndex(host[:lastDot], ".")
	if dot2ndLast == -1 {
		return host
	}

	part := host[dot2ndLast+1 : lastDot]
	// If the 2nd last part of a domain name equals to a top level
	// domain, search for the 3rd part in the host name.
	// So domains like bbc.co.uk will not be recorded as co.uk
	if topLevelDomain[part] {
		dot3rdLast := strings.LastIndex(host[:dot2ndLast], ".")
		if dot3rdLast == -1 {
			return host
		}
		return host[dot3rdLast+1:]
	}
	return host[dot2ndLast+1:]
}
