package main

import (
	"bytes"
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

	"github.com/cyfdecyf/bufio"
)

const isWindows = runtime.GOOS == "windows"

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
	for i := 0; i < len(b); i++ {
		if 97 <= b[i] && b[i] <= 122 {
			buf[i] = b[i] - 32
		} else {
			buf[i] = b[i]
		}
	}
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
	for i := 0; i < len(b); i++ {
		if 65 <= b[i] && b[i] <= 90 {
			buf[i] = b[i] + 32
		} else {
			buf[i] = b[i]
		}
	}
	return buf
}

func IsDigit(b byte) bool {
	return '0' <= b && b <= '9'
}

var spaceTbl = [256]bool{
	'\t': true, // ht
	'\n': true, // lf
	'\r': true, // cr
	' ':  true, // sp
}

func IsSpace(b byte) bool {
	return spaceTbl[b]
}

func TrimSpace(s []byte) []byte {
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

func TrimTrailingSpace(s []byte) []byte {
	end := len(s) - 1
	for ; end >= 0 && IsSpace(s[end]); end-- {
	}
	return s[:end+1]
}

// FieldsN is simliar with bytes.Fields, but only consider space and '\t' as
// space, and will include all content in the final slice with ending white
// space characters trimmed. bytes.Split can't split on both space and '\t',
// and considers two separator as an empty item. bytes.FieldsFunc can't
// specify how much fields we need, which is required for parsing response
// status line. Returns nil if n < 0.
func FieldsN(s []byte, n int) [][]byte {
	if n <= 0 {
		return nil
	}
	res := make([][]byte, n)
	na := 0
	fieldStart := -1
	var i int
	for ; i < len(s); i++ {
		issep := s[i] == ' ' || s[i] == '\t'
		if fieldStart < 0 && !issep {
			fieldStart = i
		}
		if fieldStart >= 0 && issep {
			if na == n-1 {
				break
			}
			res[na] = s[fieldStart:i]
			na++
			fieldStart = -1
		}
	}
	if fieldStart >= 0 { // must have na <= n-1 here
		res[na] = TrimSpace(s[fieldStart:])
		if len(res[na]) != 0 { // do not consider ending space as a field
			na++
		}
	}
	return res[:na]
}

var digitTbl = [256]int8{
	-1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1,
	-1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1,
	-1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1,
	0, 1, 2, 3, 4, 5, 6, 7, 8, 9, -1, -1, -1, -1, -1, -1,
	-1, 10, 11, 12, 13, 14, 15, -1, -1, -1, -1, -1, -1, -1, -1, -1,
	-1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1,
	-1, 10, 11, 12, 13, 14, 15, -1, -1, -1, -1, -1, -1, -1, -1, -1,
	-1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1,
}

// ParseIntFromBytes parse hexidecimal number from given bytes.
// No prefix (e.g. 0xdeadbeef) should given.
// base can only be 10 or 16.
func ParseIntFromBytes(b []byte, base int) (n int64, err error) {
	// Currently, we have to convert []byte to string to use strconv
	// Refer to: http://code.google.com/p/go/issues/detail?id=2632
	// That's why I created this function.
	if base != 10 && base != 16 {
		err = errors.New(fmt.Sprintf("invalid base: %d", base))
		return
	}
	if len(b) == 0 {
		err = errors.New("parse int from empty bytes")
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
		v := digitTbl[d]
		if v == -1 {
			n = 0
			err = errors.New(fmt.Sprintf("invalid number: %s", b))
			return
		}
		if int(v) >= base {
			n = 0
			err = errors.New(fmt.Sprintf("invalid base %d number: %s", base, b))
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

func isFileExists(path string) error {
	stat, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !stat.Mode().IsRegular() {
		return fmt.Errorf("%s is not regular file", path)
	}
	return nil
}

func isDirExists(path string) error {
	stat, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !stat.IsDir() {
		return fmt.Errorf("%s is not directory", path)
	}
	return nil
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

// copyN copys N bytes from src to dst, reading at most rdSize for each read.
// rdSize should <= buffer size of the buffered reader.
// Returns any encountered error.
func copyN(dst io.Writer, src *bufio.Reader, n, rdSize int) (err error) {
	// Most of the copy is copied from io.Copy
	for n > 0 {
		var b []byte
		var er error
		if n > rdSize {
			b, er = src.ReadN(rdSize)
		} else {
			b, er = src.ReadN(n)
		}
		nr := len(b)
		n -= nr
		if nr > 0 {
			nw, ew := dst.Write(b)
			if ew != nil {
				err = ew
				break
			}
			if nr != nw {
				err = io.ErrShortWrite
				break
			}
		}
		if er == io.EOF {
			break
		}
		if er != nil {
			err = er
			break
		}
	}
	return err
}

func md5sum(ss ...string) string {
	h := md5.New()
	for _, s := range ss {
		io.WriteString(h, s)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// hostIsIP determines whether a host address is an IP address and whether
// it is private. Currenly only handles IPv4 addresses.
func hostIsIP(host string) (isIP, isPrivate bool) {
	part := strings.Split(host, ".")
	if len(part) != 4 {
		return false, false
	}
	for _, i := range part {
		if len(i) == 0 || len(i) > 3 {
			return false, false
		}
		n, err := strconv.Atoi(i)
		if err != nil || n < 0 || n > 255 {
			return false, false
		}
	}
	if part[0] == "127" || part[0] == "10" || (part[0] == "192" && part[1] == "168") {
		return true, true
	}
	if part[0] == "172" {
		n, _ := strconv.Atoi(part[1])
		if 16 <= n && n <= 31 {
			return true, true
		}
	}
	return true, false
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
	"com": true,
	"edu": true,
	"gov": true,
	"net": true,
	"org": true,
}

func trimLastDot(s string) string {
	if len(s) > 0 && s[len(s)-1] == '.' {
		return s[:len(s)-1]
	}
	return s
}

// host2Domain returns the domain of a host. It will recognize domains like
// google.com.hk. Returns empty string for simple host and internal IP.
func host2Domain(host string) (domain string) {
	isIP, isPrivate := hostIsIP(host)
	if isPrivate {
		return ""
	}
	if isIP {
		return host
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

// IgnoreUTF8BOM consumes UTF-8 encoded BOM character if present in the file.
func IgnoreUTF8BOM(f *os.File) error {
	bom := make([]byte, 3)
	n, err := f.Read(bom)
	if err != nil {
		return err
	}
	if n != 3 {
		return nil
	}
	if bytes.Equal(bom, []byte{0xEF, 0xBB, 0xBF}) {
		debug.Println("UTF-8 BOM found")
		return nil
	}
	// No BOM found, seek back
	_, err = f.Seek(-3, 1)
	return err
}

// Return all host IP addresses.
func hostAddr() (addr []string) {
	allAddr, err := net.InterfaceAddrs()
	if err != nil {
		Fatal("error getting host address", err)
	}
	for _, ad := range allAddr {
		ads := ad.String()
		id := strings.Index(ads, "/")
		if id == -1 {
			// On windows, no network mask.
			id = len(ads)
		}
		addr = append(addr, ads[:id])
	}
	return addr
}
