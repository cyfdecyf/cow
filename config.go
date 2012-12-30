package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"reflect"
	"strconv"
	"strings"
)

var (
	selfURL127 string // 127.0.0.1:listenAddr
	selfURLLH  string // localhost:listenAddr
)

const (
	version           = "0.3.5"
	defaultListenAddr = "127.0.0.1:7777"
)

type Config struct {
	RcFile        string // config file
	ListenAddr    string
	SocksAddr     string
	Core          int
	SshServer     string
	UpdateBlocked bool
	UpdateDirect  bool
	AutoRetry     bool
	DetectSSLErr  bool
	LogFile       string
	AlwaysProxy   bool
	ShadowSocks   string
	ShadowPasswd  string

	// not configurable in config file
	PrintVer bool
}

var config Config

var dsFile struct {
	dir           string // directory containing config file and blocked site list
	blocked       string // contains blocked domains
	direct        string // contains sites that can be directly accessed
	alwaysDirect  string
	alwaysBlocked string
	chou          string // chou feng, sites which will be temporary blocked
}

func printVersion() {
	fmt.Println("cow version", version)
}

func init() {
	initConfigDir()
	// fmt.Println("home dir:", homeDir)

	dsFile.blocked = path.Join(dsFile.dir, blockedFname)
	dsFile.direct = path.Join(dsFile.dir, directFname)
	dsFile.alwaysBlocked = path.Join(dsFile.dir, alwaysBlockedFname)
	dsFile.alwaysDirect = path.Join(dsFile.dir, alwaysDirectFname)
	dsFile.chou = path.Join(dsFile.dir, chouFname)

	config.UpdateBlocked = false
	config.UpdateDirect = false
	config.AutoRetry = true
	config.DetectSSLErr = true
	config.AlwaysProxy = false
}

func parseCmdLineConfig() *Config {
	var c Config
	flag.StringVar(&c.RcFile, "rc", path.Join(dsFile.dir, rcFname), "configuration file")
	flag.StringVar(&c.ListenAddr, "listen", "", "proxy server listen address, default to "+defaultListenAddr)
	flag.StringVar(&c.SocksAddr, "socks", "", "socks proxy address")
	flag.IntVar(&c.Core, "core", 2, "number of cores to use")
	flag.StringVar(&c.SshServer, "sshServer", "", "remote server which will ssh to and provide sock server")
	flag.StringVar(&c.LogFile, "logFile", "", "write output to file, empty means stdout")
	flag.StringVar(&c.ShadowSocks, "shadowSocks", "", "shadowsocks server address")
	flag.StringVar(&c.ShadowPasswd, "shadowPasswd", "", "shadowsocks password")
	flag.BoolVar(&c.PrintVer, "version", false, "print version")

	// Bool options can't be specified on command line because the flag
	// pacakge dosen't provide an easy way to detect if an option is actually
	// given on command line, so we don't know if the option in config file
	// should be override.

	// flag.BoolVar(&c.AutoRetry, "autoRetry", false, "automatically retry timeout requests using socks proxy")
	// flag.BoolVar(&c.UpdateBlocked, "updateBlocked", true, "update blocked site list")
	// flag.BoolVar(&c.UpdateDirect, "updateDirect", true, "update direct site list")
	// flag.BoolVar(&c.DetectSSLErr, "detectSSLErr", true, "detect SSL error based on how soon client closes connection")
	// flag.BoolVar(&c.AlwaysProxy, "alwaysProxy", false, "always use parent proxy")

	flag.Parse()
	return &c
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
	config.ListenAddr = val
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
	config.SocksAddr = val
}

func (p configParser) ParseCore(val string) {
	if val == "" {
		return
	}
	var err error
	config.Core, err = strconv.Atoi(val)
	if err != nil {
		fmt.Printf("Config error: core number %s %v", val, err)
		os.Exit(1)
	}
}

func (p configParser) ParseSshServer(val string) {
	config.SshServer = val
}

func (p configParser) ParseUpdateBlocked(val string) {
	config.UpdateBlocked = parseBool(val, "updateBlocked")
}

func (p configParser) ParseUpdateDirect(val string) {
	config.UpdateDirect = parseBool(val, "updateDirect")
}

func (p configParser) ParseAutoRetry(val string) {
	config.AutoRetry = parseBool(val, "autoRetry")
}

func (p configParser) ParseDetectSSLErr(val string) {
	config.DetectSSLErr = parseBool(val, "detectSSLErr")
}

func (p configParser) ParseLogFile(val string) {
	config.LogFile = val
}

func (p configParser) ParseAlwaysProxy(val string) {
	config.AlwaysProxy = parseBool(val, "alwaysProxy")
}

func (p configParser) ParseShadowSocks(val string) {
	if !isServerAddrValid(val) {
		fmt.Println("shadowsocks server must have port specified")
		os.Exit(1)
	}
	config.ShadowSocks = val
}

func (p configParser) ParseShadowPasswd(val string) {
	config.ShadowPasswd = val
}

func parseConfig(path string) {
	// fmt.Println("rcFile:", path)
	f, err := os.Open(expandTilde(path))
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("Config file %s not found, using default options\n", path)
		} else {
			fmt.Println("Error opening config file:", err)
		}
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

func updateConfig(new *Config) {
	newVal := reflect.ValueOf(new).Elem()
	oldVal := reflect.ValueOf(&config).Elem()

	// typeOfT := newVal.Type()
	for i := 0; i < newVal.NumField(); i++ {
		newField := newVal.Field(i)
		oldField := oldVal.Field(i)
		// log.Printf("%d: %s %s = %v\n", i,
		// typeOfT.Field(i).Name, newField.Type(), newField.Interface())
		switch newField.Kind() {
		case reflect.String:
			s := newField.String()
			if s != "" {
				oldField.SetString(s)
			}
		case reflect.Int:
			i := newField.Int()
			if i != 0 {
				oldField.SetInt(i)
			}
		}
	}
	if config.ListenAddr == "" {
		config.ListenAddr = defaultListenAddr
	}
}

func setSelfURL() {
	_, port := splitHostPort(config.ListenAddr)
	selfURL127 = "127.0.0.1:" + port
	selfURLLH = "localhost:" + port
}
