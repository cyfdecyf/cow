package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/user"
	"path"
	// "reflect"
	"strconv"
	"strings"
)

var (
	homeDir    string
	selfURL127 string // 127.0.0.1:listenAddr
	selfURLLH  string // localhost:listenAddr
)

const (
	dotDir       = ".cow-proxy"
	blockedFname = "blocked"
	directFname  = "direct"
	rcFname      = "rc"
)

var config struct {
	listenAddr string // server listen address
	socksAddr  string
	numProc    int    // max number of cores to use
	sshServer  string // ssh to the given server to start socks proxy

	dir         string // directory containing config file and blocked site list
	blockedFile string // contains blocked domains
	directFile  string // contains sites that can be directly accessed
	rcFile      string
}

func init() {
	u, err := user.Current()
	if err != nil {
		errl.Printf("Can't get user information %v", err)
		os.Exit(1)
	}
	homeDir = u.HomeDir

	config.listenAddr = "127.0.0.1:7777"
	config.numProc = 2
	config.socksAddr = "127.0.0.1:1080"

	config.dir = path.Join(homeDir, dotDir)
	config.blockedFile = path.Join(config.dir, blockedFname)
	config.directFile = path.Join(config.dir, directFname)
	config.rcFile = path.Join(config.dir, rcFname)
}

// Tries to open a file, if file not exist, return both nil for os.File and
// err
func openFile(path string) (f *os.File, err error) {
	if f, err = os.Open(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		errl.Println("Error opening file:", path, err)
		return nil, err
	}
	return
}

func loadDomainList(fpath string) (lst []string, err error) {
	f, err := openFile(fpath)
	if f == nil || err != nil {
		return
	}
	defer f.Close()

	fr := bufio.NewReader(f)
	lst = make([]string, 0)
	var domain string
	for {
		domain, err = ReadLine(fr)
		if err == io.EOF {
			return lst, nil
		} else if err != nil {
			errl.Println("Error reading domain list from:", fpath, err)
			return
		}
		if domain == "" {
			continue
		}
		lst = append(lst, strings.TrimSpace(domain))
	}
	return
}

func parseConfig() {
	f, err := openFile(config.rcFile)
	if f == nil || err != nil {
		return
	}
	defer f.Close()

	fr := bufio.NewReader(f)

	var line string
	var n int
	for {
		n++
		line, err = ReadLine(fr)
		if err == io.EOF {
			return
		} else if err != nil {
			errl.Println("Error reading rc file:", err)
			errl.Println("Exit")
			os.Exit(1)
		}

		if line == "" {
			continue
		}

		line = strings.TrimSpace(line)
		// Ignore comment
		if line[0] == '#' {
			continue
		}

		v := strings.Split(line, "=")
		if len(v) != 2 {
			fmt.Println("Config error: syntax error on line", n)
			os.Exit(1)
		}
		key, val := strings.TrimSpace(v[0]), strings.TrimSpace(v[1])

		switch {
		case key == "listen":
			config.listenAddr = val
		case key == "core":
			config.numProc, err = strconv.Atoi(val)
			if err != nil {
				fmt.Printf("Config error: core number %d %v", n, err)
				os.Exit(1)
			}
		case key == "socks":
			_, port := splitHostPort(val)
			if port == "" {
				fmt.Println("socks server must have port specified")
				os.Exit(1)
			}
			config.socksAddr = val
		case key == "blocked":
			config.blockedFile = val
		case key == "ssh_server":
			config.sshServer = val
		default:
			fmt.Println("Config error: no such option", key)
			os.Exit(1)
		}
	}
}

func loadConfig() {
	parseConfig()
	loadBlocked()
	genPAC()

	_, port := splitHostPort(config.listenAddr)
	selfURL127 = "127.0.0.1:" + port
	selfURLLH = "localhost:" + port
}
