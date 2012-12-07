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
)

const (
	dotDir             = ".cow"
	blockedFname       = "auto-blocked"
	directFname        = "auto-direct"
	alwaysBlockedFname = "blocked"
	alwaysDirectFname  = "direct"
	chouFname          = "chou"
	rcFname            = "rc"

	version = "0.3.3"
)

var config struct {
	listenAddr    string
	socksAddr     string
	numProc       int
	sshServer     string
	updateBlocked bool
	updateDirect  bool
	autoRetry     bool
	detectSSLErr  bool
	logFile       string
	alwaysProxy   bool
	printVer      bool

	// For shadowsocks server
	shadowSocks  string
	shadowPasswd string

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

	flag.StringVar(&config.listenAddr, "listen", "127.0.0.1:7777", "proxy server listen address")
	flag.StringVar(&config.socksAddr, "socks", "127.0.0.1:1080", "socks proxy address")
	flag.IntVar(&config.numProc, "core", 2, "number of cores to use")
	flag.StringVar(&config.sshServer, "sshServer", "", "remote server which will ssh to and provide sock server")
	flag.BoolVar(&config.updateBlocked, "updateBlocked", true, "update blocked site list")
	flag.BoolVar(&config.updateDirect, "updateDirect", true, "update direct site list")
	flag.BoolVar(&config.autoRetry, "autoRetry", false, "automatically retry timeout requests using socks proxy")
	flag.BoolVar(&config.detectSSLErr, "detectSSLErr", true, "detect SSL error based on how soon client closes connection")
	flag.StringVar(&config.logFile, "logFile", "", "write output to file, empty means stdout")
	flag.BoolVar(&config.alwaysProxy, "alwaysProxy", false, "always use parent proxy")
	flag.BoolVar(&config.printVer, "version", false, "print version")

	flag.StringVar(&config.shadowSocks, "shadowSocks", "", "shadowsocks server address")
	flag.StringVar(&config.shadowPasswd, "shadowPasswd", "", "shadowsocks password")

	config.dir = path.Join(homeDir, dotDir)
	config.blockedFile = path.Join(config.dir, blockedFname)
	config.directFile = path.Join(config.dir, directFname)
	config.alwaysBlockedFile = path.Join(config.dir, alwaysBlockedFname)
	config.alwaysDirectFile = path.Join(config.dir, alwaysDirectFname)
	config.chouFile = path.Join(config.dir, chouFname)
	config.rcFile = path.Join(config.dir, rcFname)

	// Make it easy to find config directory on windows
	if isWindows() {
		fmt.Println("Config directory:", config.dir)
	}
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

func isServerAddrValid(val string) bool {
	if val == "" {
		return true
	}
	_, port := splitHostPort(val)
	if port == "" {
		return false
	}
	return true
}

func (p configParser) ParseSocks(val string) {
	if !isServerAddrValid(val) {
		fmt.Println("socks server must have port specified")
		os.Exit(1)
	}
	config.socksAddr = val
}

func (p configParser) ParseCore(val string) {
	if val == "" {
		return
	}
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

func (p configParser) ParseDetectSSLErr(val string) {
	config.detectSSLErr = parseBool(val, "detectSSLErr")
}

func (p configParser) ParseLogFile(val string) {
	config.logFile = val
}

func (p configParser) ParseAlwaysProxy(val string) {
	config.alwaysProxy = parseBool(val, "alwaysProxy")
}

func (p configParser) ParseShadowSocks(val string) {
	if !isServerAddrValid(val) {
		fmt.Println("shadowsocks server must have port specified")
		os.Exit(1)
	}
	config.shadowSocks = val
}

func (p configParser) ParseShadowPasswd(val string) {
	config.shadowPasswd = val
}

func loadConfig() {
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

func setSelfURL() {
	_, port := splitHostPort(config.listenAddr)
	selfURL127 = "127.0.0.1:" + port
	selfURLLH = "localhost:" + port
}
