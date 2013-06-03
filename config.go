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
)

const (
	version           = "0.6.3"
	defaultListenAddr = "127.0.0.1:7777"
)

type LoadBalanceMode byte

const (
	loadBalanceBackup LoadBalanceMode = iota
	loadBalanceHash
)

type Config struct {
	RcFile      string // config file
	ListenAddr  []string
	LogFile     string
	AlwaysProxy bool
	LoadBalance LoadBalanceMode

	SshServer string // TODO support multiple ssh server options

	// http parent proxy
	hasHttpParent bool

	// authenticate client
	UserPasswd    string
	AllowedClient string
	AuthTimeout   time.Duration

	// advanced options
	DialTimeout time.Duration
	ReadTimeout time.Duration

	Core         int
	AddrInPAC    []string
	DetectSSLErr bool

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
	flag.IntVar(&c.Core, "core", 2, "number of cores to use")
	flag.StringVar(&c.LogFile, "logFile", "", "write output to file")
	flag.BoolVar(&c.PrintVer, "version", false, "print version")

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
		Fatalf("%s should be true or false\n", msg)
	}
	return false
}

func parseInt(val, msg string) (i int) {
	var err error
	if i, err = strconv.Atoi(val); err != nil {
		Fatalf("%s should be an integer\n", msg)
	}
	return
}

func parseDuration(val, msg string) (d time.Duration) {
	var err error
	if d, err = time.ParseDuration(val); err != nil {
		Fatalf("%s %v\n", msg, err)
	}
	return
}

func hasPort(val string) bool {
	_, port := splitHostPort(val)
	if port == "" {
		return false
	}
	return true
}

func isUserPasswdValid(val string) bool {
	arr := strings.SplitN(val, ":", 2)
	if len(arr) != 2 || arr[0] == "" || arr[1] == "" {
		return false
	}
	return true
}

type configParser struct{}

func (p configParser) ParseLogFile(val string) {
	config.LogFile = val
}

func (p configParser) ParseListen(val string) {
	// Command line options has already specified listenAddr
	if config.ListenAddr != nil {
		return
	}
	arr := strings.Split(val, ",")
	config.ListenAddr = make([]string, len(arr))
	for i, s := range arr {
		s = strings.TrimSpace(s)
		host, port := splitHostPort(s)
		if port == "" {
			Fatalf("listen address %s has no port\n", s)
		}
		if host == "" || host == "0.0.0.0" {
			if len(arr) > 1 {
				Fatalf("too much listen addresses: "+
					"%s represents all ip addresses on this host.\n", s)
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
			Fatalf("proxy address in PAC %s has no port\n", s)
		}
		if host == "0.0.0.0" {
			Fatal("can't use 0.0.0.0 as proxy address in PAC")
		}
		config.AddrInPAC[i] = s
	}
}

func (p configParser) ParseSocks(val string) {
	fmt.Println("socks option is going to be renamed to socksParent in the future, please change it")
	p.ParseSocksParent(val)
}

// error checking is done in check config

func (p configParser) ParseSocksParent(val string) {
	if !hasPort(val) {
		Fatal("parent socks server must have port specified")
	}
	addParentProxy(newSocksParent(val))
}

func (p configParser) ParseSshServer(val string) {
	config.SshServer = val
}

var http struct {
	parent    *httpParent
	serverCnt int
	passwdCnt int
}

func (p configParser) ParseHttpParent(val string) {
	if !hasPort(val) {
		Fatal("parent http server must have port specified")
	}
	config.hasHttpParent = true
	http.parent = newHttpParent(val)
	addParentProxy(http.parent)
	http.serverCnt++
}

func (p configParser) ParseHttpUserPasswd(val string) {
	if !isUserPasswdValid(val) {
		Fatal("httpUserPassword syntax wrong, should be in the form of user:passwd")
	}
	if http.passwdCnt >= http.serverCnt {
		Fatal("must specify httpParent before corresponding httpUserPasswd")
	}
	http.parent.initAuth(val)
	http.passwdCnt++
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

func (p configParser) ParseAlwaysProxy(val string) {
	config.AlwaysProxy = parseBool(val, "alwaysProxy")
}

func (p configParser) ParseLoadBalance(val string) {
	switch val {
	case "backup":
		config.LoadBalance = loadBalanceBackup
	case "hash":
		config.LoadBalance = loadBalanceHash
	default:
		Fatalf("invalid loadBalance mode: %s\n", val)
	}
}

var shadow struct {
	parent *shadowsocksParent
	passwd string
	method string

	serverCnt int
	passwdCnt int
	methodCnt int
}

func (p configParser) ParseShadowSocks(val string) {
	if !hasPort(val) {
		Fatal("shadowsocks server must have port specified")
	}
	if shadow.serverCnt-shadow.passwdCnt > 1 {
		Fatal("must specify shadowPasswd for every shadowSocks server")
	}
	// create new shadowsocks parent if both server and password are given
	// previously
	if shadow.parent != nil && shadow.serverCnt == shadow.passwdCnt {
		if shadow.methodCnt < shadow.serverCnt {
			shadow.method = ""
			shadow.methodCnt = shadow.serverCnt
		}
		shadow.parent.initCipher(shadow.passwd, shadow.method)
	}
	shadow.parent = newShadowsocksParent(val)
	addParentProxy(shadow.parent)
	shadow.serverCnt++
}

func (p configParser) ParseShadowPasswd(val string) {
	if shadow.passwdCnt >= shadow.serverCnt {
		Fatal("must specify shadowSocks before corresponding shadowPasswd")
	}
	if shadow.passwdCnt+1 != shadow.serverCnt {
		Fatal("must specify shadowPasswd for every shadowSocks")
	}
	shadow.passwd = val
	shadow.passwdCnt++
}

func (p configParser) ParseShadowMethod(val string) {
	if shadow.methodCnt >= shadow.serverCnt {
		Fatal("must specify shadowSocks before corresponding shadowMethod")
	}
	// shadowMethod is optional
	shadow.method = val
	shadow.methodCnt++
}

func checkShadowsocks() {
	if shadow.serverCnt != shadow.passwdCnt {
		Fatal("number of shadowsocks server and password does not match")
	}
	// parse the last shadowSocks option again to initialize the last
	// shadowsocks server
	parser := configParser{}
	parser.ParseShadowSocks("dummyshadowsocks:1234")
}

// Put actual authentication related config parsing in auth.go, so config.go
// doesn't need to know the details of authentication implementation.

func (p configParser) ParseUserPasswd(val string) {
	config.UserPasswd = val
	if !isUserPasswdValid(config.UserPasswd) {
		Fatal("userPassword syntax wrong, should be in the form of user:passwd")
	}
}

func (p configParser) ParseAllowedClient(val string) {
	config.AllowedClient = val
}

func (p configParser) ParseAuthTimeout(val string) {
	config.AuthTimeout = parseDuration(val, "authTimeout")
}

func (p configParser) ParseCore(val string) {
	config.Core = parseInt(val, "core")
}

func (p configParser) ParseReadTimeout(val string) {
	config.ReadTimeout = parseDuration(val, "readTimeout")
}

func (p configParser) ParseDialTimeout(val string) {
	config.DialTimeout = parseDuration(val, "dialTimeout")
}

func (p configParser) ParseDetectSSLErr(val string) {
	config.DetectSSLErr = parseBool(val, "detectSSLErr")
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
			Fatalf("Error reading rc file: %v\n", err)
		}

		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}

		v := strings.Split(line, "=")
		if len(v) != 2 {
			Fatal("config syntax error on line", n)
		}
		key, val := strings.TrimSpace(v[0]), strings.TrimSpace(v[1])

		methodName := "Parse" + strings.ToUpper(key[0:1]) + key[1:]
		method := parser.MethodByName(methodName)
		if method == zeroMethod {
			Fatalf("no such option \"%s\"\n", key)
		}
		// for backward compatibility, allow empty string in shadowMethod and logFile
		if val == "" && key != "shadowMethod" && key != "logFile" {
			Fatalf("empty %s, please comment or remove unused option\n", key)
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
}

// Must call checkConfig before using config.
func checkConfig() {
	checkShadowsocks()
	// listenAddr must be handled first, as addrInPAC dependends on this.
	if config.ListenAddr == nil {
		config.ListenAddr = []string{defaultListenAddr}
	}
	if config.AddrInPAC != nil {
		if len(config.AddrInPAC) != len(config.ListenAddr) {
			Fatal("Number of listen addresses and addr in PAC not match.")
		}
	} else {
		// empty string in addrInPac means same as listenAddr
		config.AddrInPAC = make([]string, len(config.ListenAddr))
	}
	if len(parentProxy) <= 1 {
		config.LoadBalance = loadBalanceBackup
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
