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
	"time"
)

const (
	version           = "0.3.5"
	defaultListenAddr = "127.0.0.1:7777"
)

type Config struct {
	RcFile        string // config file
	ListenAddr    []string
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
	ShadowMethod  string // shadowsocks encryption method
	UserPasswd    string
	AllowedClient string
	AuthTimeout   time.Duration
	DialTimeout   time.Duration
	ReadTimeout   time.Duration

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

	config.UpdateBlocked = false
	config.UpdateDirect = false
	config.AutoRetry = true
	config.DetectSSLErr = false
	config.AlwaysProxy = false
}

func parseCmdLineConfig() *Config {
	var c Config
	var listenAddr string
	flag.StringVar(&c.RcFile, "rc", path.Join(dsFile.dir, rcFname), "configuration file")
	// Specifying listen default value to StringVar would override config file options
	flag.StringVar(&listenAddr, "listen", "", "proxy server listen address, default to "+defaultListenAddr)
	flag.StringVar(&c.SocksAddr, "socks", "", "socks proxy address")
	flag.IntVar(&c.Core, "core", 2, "number of cores to use")
	flag.StringVar(&c.SshServer, "sshServer", "", "remote server which will ssh to and provide sock server")
	flag.StringVar(&c.LogFile, "logFile", "", "write output to file")
	flag.StringVar(&c.ShadowSocks, "shadowSocks", "", "shadowsocks server address")
	flag.StringVar(&c.ShadowPasswd, "shadowPasswd", "", "shadowsocks password")
	flag.StringVar(&c.ShadowMethod, "shadowMethod", "", "shadowsocks encryption method, empty string or rc4")
	flag.StringVar(&c.UserPasswd, "userPasswd", "", "user name and password for authentication")
	flag.StringVar(&c.AllowedClient, "allowedClient", "", "clients that need no authentication, list of IP address or IPv4 subnet")
	flag.DurationVar(&c.AuthTimeout, "authTimeout", 2*time.Hour, "authentication time out,")
	flag.DurationVar(&c.ReadTimeout, "readTimeout", 5*time.Second, "minimum read time out")
	flag.DurationVar(&c.DialTimeout, "dialTimeout", 5*time.Second, "minimum dial time out")
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
	if listenAddr != "" {
		c.ListenAddr = []string{listenAddr}
	}
	return &c
}

func parseBool(v, msg string) bool {
	switch v {
	case "true":
		return true
	case "false":
		return false
	default:
		fmt.Printf("Config error: %s should be true or false\n", msg)
		os.Exit(1)
	}
	return false
}

func parseInt(val, msg string) (i int) {
	var err error
	if i, err = strconv.Atoi(val); err != nil {
		fmt.Printf("Config error: %s should be an integer\n", msg)
		os.Exit(1)
	}
	return
}

func parseDuration(val, msg string) (d time.Duration) {
	var err error
	if d, err = time.ParseDuration(val); err != nil {
		fmt.Printf("Config error: %s %v\n", msg, err)
		os.Exit(1)
	}
	return
}

type configParser struct{}

func (p configParser) ParseListen(val string) {
	arr := strings.Split(val, ",")
	config.ListenAddr = make([]string, len(arr))
	for i, s := range arr {
		s = strings.TrimSpace(s)
		host, port := splitHostPort(s)
		if port == "" {
			fmt.Printf("listen address %s has no port\n", s)
			os.Exit(1)
		}
		if host == "" || host == "0.0.0.0" {
			if len(arr) > 1 {
				fmt.Printf("Too much listen addresses: "+
					"%s represents all ip addresses on this host.\n", s)
				os.Exit(1)
			}
		}
		config.ListenAddr[i] = s
	}
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
	config.Core = parseInt(val, "core")
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

func (p configParser) ParseShadowMethod(val string) {
	config.ShadowMethod = val
}

// Put actual authentication related config parsing in auth.go, so config.go
// doesn't need to know the details of authentication implementation.

func (p configParser) ParseUserPasswd(val string) {
	config.UserPasswd = val
}

func (p configParser) ParseAllowedClient(val string) {
	config.AllowedClient = val
}

func (p configParser) ParseAuthTimeout(val string) {
	config.AuthTimeout = parseDuration(val, "authTimeout")
}

func (p configParser) ParseReadTimeout(val string) {
	config.ReadTimeout = parseDuration(val, "readTimeout")
}

func (p configParser) ParseDialTimeout(val string) {
	config.DialTimeout = parseDuration(val, "dialTimeout")
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

		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}

		v := strings.Split(line, "=")
		if len(v) != 2 {
			fmt.Println("Config error: syntax error on line", n)
			os.Exit(1)
		}
		key, val := strings.TrimSpace(v[0]), strings.TrimSpace(v[1])
		if val == "" {
			continue
		}

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
	if new.ListenAddr != nil {
		config.ListenAddr = new.ListenAddr
	}
	if config.ListenAddr == nil {
		config.ListenAddr = []string{defaultListenAddr}
	}
}
