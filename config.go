package main

import (
	"flag"
	"fmt"
	"github.com/cyfdecyf/bufio"
	"io"
	"os"
	"path"
	"reflect"
	"strconv"
	"strings"
	"time"
	"encoding/base64"
)

const (
	version           = "0.6"
	defaultListenAddr = "127.0.0.1:7777"
)

type Config struct {
	RcFile        string // config file
	ListenAddr    []string
	AddrInPAC     []string
	SocksParent   string
	HttpParent    string
	HttpUserPasswd string
	HttpAuthHeader []byte
	Core          int
	SshServer     string
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
	alwaysBlocked string // blocked sites specified by user
	alwaysDirect  string // direct sites specified by user
	stat          string // site visit statistics
}

func printVersion() {
	fmt.Println("cow version", version)
}

func init() {
	initConfigDir()
	// fmt.Println("home dir:", homeDir)

	dsFile.alwaysBlocked = path.Join(dsFile.dir, alwaysBlockedFname)
	dsFile.alwaysDirect = path.Join(dsFile.dir, alwaysDirectFname)
	dsFile.stat = path.Join(dsFile.dir, statFname)

	config.DetectSSLErr = false
	config.AlwaysProxy = false

	config.AuthTimeout = 2 * time.Hour
	config.DialTimeout = defaultDialTimeout
	config.ReadTimeout = defaultReadTimeout
}

func parseCmdLineConfig() *Config {
	var c Config
	var listenAddr string
	flag.StringVar(&c.RcFile, "rc", path.Join(dsFile.dir, rcFname), "configuration file")
	// Specifying listen default value to StringVar would override config file options
	flag.StringVar(&listenAddr, "listen", "", "proxy server listen address, default to "+defaultListenAddr)
	flag.StringVar(&c.SocksParent, "socksParent", "", "parent socks5 proxy address")
	flag.StringVar(&c.HttpParent, "httpParent", "", "parent http proxy address")
	flag.StringVar(&c.HttpUserPasswd, "httpUserPasswd", "", "user name and password for parent http proxy basic authentication")
	flag.IntVar(&c.Core, "core", 2, "number of cores to use")
	flag.StringVar(&c.SshServer, "sshServer", "", "remote server which will ssh to and provide socks server")
	flag.StringVar(&c.LogFile, "logFile", "", "write output to file")
	flag.StringVar(&c.ShadowSocks, "shadowSocks", "", "shadowsocks server address")
	flag.StringVar(&c.ShadowPasswd, "shadowPasswd", "", "shadowsocks password")
	flag.StringVar(&c.ShadowMethod, "shadowMethod", "", "shadowsocks encryption method, empty string or rc4")
	flag.StringVar(&c.UserPasswd, "userPasswd", "", "user name and password for authentication")
	flag.BoolVar(&c.PrintVer, "version", false, "print version")

	// Bool options can't be specified on command line because the flag
	// pacakge dosen't provide an easy way to detect if an option is actually
	// given on command line, so we don't know if the option in config file
	// should be override.

	// flag.BoolVar(&c.DetectSSLErr, "detectSSLErr", true, "detect SSL error based on how soon client closes connection")
	// flag.BoolVar(&c.AlwaysProxy, "alwaysProxy", false, "always use parent proxy")

	flag.Parse()
	if listenAddr != "" {
		configParser{}.ParseListen(listenAddr)
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
	// Has specified command line options
	if config.ListenAddr != nil {
		return
	}
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

func (p configParser) ParseAddrInPAC(val string) {
	arr := strings.Split(val, ",")
	config.AddrInPAC = make([]string, len(arr))
	for i, s := range arr {
		if s == "" {
			continue
		}
		s = strings.TrimSpace(s)
		host, port := splitHostPort(s)
		if port == "" {
			fmt.Printf("proxy address in PAC %s has no port\n", s)
			os.Exit(1)
		}
		if host == "0.0.0.0" {
			fmt.Println("Can't use 0.0.0.0 as proxy address in PAC")
			os.Exit(1)
		}
		config.AddrInPAC[i] = s
	}
}

func isServerAddrValid(val string) bool {
	if val == "" {
		return false
	}
	_, port := splitHostPort(val)
	if port == "" {
		return false
	}
	return true
}

var hasHttpParentProxy bool

func (p configParser) ParseSocks(val string) {
	fmt.Println("socks option is going to be renamed to socksParent in the future, please change it")
	p.ParseSocksParent(val)
}

func (p configParser) ParseSocksParent(val string) {
	if !isServerAddrValid(val) {
		fmt.Println("parent socks server must have port specified")
		os.Exit(1)
	}
	config.SocksParent = val
	parentProxyCreator = append(parentProxyCreator, createctSocksConnection)
}

func (p configParser) ParseHttpParent(val string) {
	if !isServerAddrValid(val) {
		fmt.Println("parent http server must have port specified")
		os.Exit(1)
	}
	config.HttpParent = val
	hasHttpParentProxy = true
	parentProxyCreator = append(parentProxyCreator, createHttpProxyConnection)
}

func (p configParser) ParseHttpUserPasswd(val string) {
	if val == "" {
		return
	}
	arr := strings.SplitN(val, ":", 2)
	if len(arr) != 2 || arr[0] == "" || arr[1] == "" {
		fmt.Println("Parent HTTP User password syntax wrong, should be in the form of user:passwd")
		os.Exit(1)
	}
	userPwd64 := base64.StdEncoding.EncodeToString([]byte(val))
	config.HttpAuthHeader = []byte(headerProxyAuthorization + ": Basic " + userPwd64 + CRLF)
}

func (p configParser) ParseCore(val string) {
	config.Core = parseInt(val, "core")
}

func (p configParser) ParseSshServer(val string) {
	config.SshServer = val
}

func (p configParser) ParseUpdateBlocked(val string) {
	// config.UpdateBlocked = parseBool(val, "updateBlocked")
	fmt.Println("updateBlocked option will be removed in future, please remove it")
}

func (p configParser) ParseUpdateDirect(val string) {
	// config.UpdateDirect = parseBool(val, "updateDirect")
	fmt.Println("updateDirect option will be removed in future, please remove it")
}

func (p configParser) ParseAutoRetry(val string) {
	// config.AutoRetry = parseBool(val, "autoRetry")
	fmt.Println("autoRetry option will be removed in future, please remove it")
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
	parentProxyCreator = append(parentProxyCreator, createShadowSocksConnection)
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

func updateConfig(nc *Config) {
	newVal := reflect.ValueOf(nc).Elem()
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
	if config.ListenAddr == nil {
		config.ListenAddr = []string{defaultListenAddr}
	}
	if config.AddrInPAC != nil {
		if len(config.AddrInPAC) != len(config.ListenAddr) {
			fmt.Println("Number of listen addresses and addr in PAC not match.")
			os.Exit(1)
		}
	} else {
		config.AddrInPAC = make([]string, len(config.ListenAddr))
	}
}

func mkConfigDir() (err error) {
	if dsFile.dir == "" {
		return os.ErrNotExist
	}
	exists, err := isDirExists(dsFile.dir)
	if err != nil {
		errl.Printf("Error checking config directory: %v\n", err)
		return
	}
	if exists {
		return
	}
	if err = os.Mkdir(dsFile.dir, 0755); err != nil {
		errl.Printf("Error create config directory %s: %v\n", dsFile.dir, err)
	}
	return
}
