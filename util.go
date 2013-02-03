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
func ReadLine(r *bufio.Reader) (line string, err error) {
	line, err = r.ReadString('\n')
	n := len(line)
	if n > 0 && (err == nil || err == io.EOF) {
		id := n - 1
		if line[id] == '\n' {
			id--
		}
		for ; id >= 0 && line[id] == '\r'; id-- {
		}
		return line[:id+1], nil
	}
	return
}

func IsDigit(b byte) bool {
	return '0' <= b && b <= '9'
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
// end are written to w before and after the n bytes. contBuf is used to store
// the content that's written for later reuse. copyN will try to minimize
// number of writes.
func copyN(r io.Reader, w, contBuf io.Writer, n int, buf, pre, end []byte) (err error) {
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
		if contBuf != nil {
			contBuf.Write(buf[:nn])
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

func hostIsIP(host string) bool {
	return net.ParseIP(host) != nil
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
