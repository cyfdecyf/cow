package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"os/user"
	"path"
	"reflect"
	"strconv"
	"strings"
)

var (
	homeDir    string
	selfURL127 string // 127.0.0.1:listenAddr
	selfURLLH  string // localhost:listenAddr

	printVer bool
)

const (
	dotDir             = ".cow"
	blockedFname       = "auto-blocked"
	directFname        = "auto-direct"
	alwaysBlockedFname = "blocked"
	alwaysDirectFname  = "direct"
	chouFname          = "chou"
	rcFname            = "rc"

	version = "0.2.3"
)

var config struct {
	listenAddr    string
	socksAddr     string
	numProc       int
	sshServer     string
	updateBlocked bool
	updateDirect  bool
	autoRetry     bool
	logFile       string

	// These are for internal use
	dir               string // directory containing config file and blocked site list
	blockedFile       string // contains blocked domains
	directFile        string // contains sites that can be directly accessed
	alwaysDirectFile  string
	alwaysBlockedFile string
	chouFile          string // chou feng, sites which will be temporary blocked
	rcFile            string
}

func printVersion() {
	fmt.Println("cow-proxy version", version)
}

func init() {
	u, err := user.Current()
	if err != nil {
		errl.Printf("Can't get user information %v", err)
		os.Exit(1)
	}
	homeDir = u.HomeDir

	flag.BoolVar(&printVer, "version", false, "print version")
	flag.StringVar(&config.listenAddr, "listen", "127.0.0.1:7777", "proxy server listen address")
	flag.StringVar(&config.socksAddr, "socks", "127.0.0.1:1080", "socks server address")
	flag.IntVar(&config.numProc, "core", 2, "number of cores to use")
	flag.StringVar(&config.sshServer, "sshServer", "", "remote server which will ssh to and provide sock server")
	flag.BoolVar(&config.updateBlocked, "updateBlocked", true, "update blocked site list")
	flag.BoolVar(&config.updateDirect, "updateDirect", true, "update direct site list")
	flag.BoolVar(&config.autoRetry, "autoRetry", true, "automatically retry blocked requests")
	flag.StringVar(&config.logFile, "logFile", "", "write output to file, empty means stdout")

	config.dir = path.Join(homeDir, dotDir)
	config.blockedFile = path.Join(config.dir, blockedFname)
	config.directFile = path.Join(config.dir, directFname)
	config.alwaysBlockedFile = path.Join(config.dir, alwaysBlockedFname)
	config.alwaysDirectFile = path.Join(config.dir, alwaysDirectFname)
	config.chouFile = path.Join(config.dir, chouFname)
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

func parseBool(v string, msg string) bool {
	switch v {
	case "true":
		return true
	case "false":
		return false
	default:
		fmt.Printf("%s should be true or false\n", msg)
		os.Exit(1)
	}
	return false
}

type configParser struct{}

func (p configParser) ParseListen(val string) {
	config.listenAddr = val
}

func (p configParser) ParseSocks(val string) {
	_, port := splitHostPort(val)
	if port == "" {
		fmt.Println("socks server must have port specified")
		os.Exit(1)
	}
	config.socksAddr = val
}

func (p configParser) ParseCore(val string) {
	var err error
	config.numProc, err = strconv.Atoi(val)
	if err != nil {
		fmt.Printf("Config error: core number %s %v", val, err)
		os.Exit(1)
	}
}

func (p configParser) ParseSshServer(val string) {
	config.sshServer = val
}

func (p configParser) ParseUpdateBlocked(val string) {
	config.updateBlocked = parseBool(val, "updateBlocked")
}

func (p configParser) ParseUpdateDirect(val string) {
	config.updateDirect = parseBool(val, "updateDirect")
}

func (p configParser) ParseAutoRetry(val string) {
	config.autoRetry = parseBool(val, "autoRetry")
}

func (p configParser) ParseLogFile(val string) {
	config.logFile = val
}

func parseConfig() {
	f, err := openFile(config.rcFile)
	if f == nil || err != nil {
		return
	}
	defer f.Close()

	fr := bufio.NewReader(f)

	parser := reflect.ValueOf(configParser{})
	zeroMethod := reflect.Value{}

	var line string
	var n int
	for {
		n++
		line, err = ReadLine(fr)
		if err == io.EOF {
			return
		} else if err != nil {
			errl.Printf("Error reading rc file: %v\n", err)
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

		methodName := "Parse" + strings.ToUpper(key[0:1]) + key[1:]
		method := parser.MethodByName(methodName)
		if method == zeroMethod {
			fmt.Printf("Config error: no such option \"%s\"\n", key)
			os.Exit(1)
		}
		args := []reflect.Value{reflect.ValueOf(val)}
		method.Call(args)
	}
}

func loadConfig() {
	parseConfig()

	loadDomainSet()

	_, port := splitHostPort(config.listenAddr)
	selfURL127 = "127.0.0.1:" + port
	selfURLLH = "localhost:" + port
}
