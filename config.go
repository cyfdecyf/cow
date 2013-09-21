package main

import (
	"flag"
	"fmt"
	"github.com/cyfdecyf/bufio"
	"net"
	"os"
	"path"
	"reflect"
	"strconv"
	"strings"
	"time"
)

const (
	version           = "0.8"
	defaultListenAddr = "127.0.0.1:7777"
)

type LoadBalanceMode byte

const (
	loadBalanceBackup LoadBalanceMode = iota
	loadBalanceHash
)

// allow the same tunnel ports as polipo
var defaultTunnelAllowedPort = []string{
	"22", "80", "443", // ssh, http, https
	"873",                      // rsync
	"143", "220", "585", "993", // imap, imap3, imap4-ssl, imaps
	"109", "110", "473", "995", // pop2, pop3, hybrid-pop, pop3s
	"5222", "5269", // jabber-client, jabber-server
	"2401", "3690", "9418", // cvspserver, svn, git
}

type Config struct {
	RcFile      string // config file
	ListenAddr  []string
	LogFile     string
	AlwaysProxy bool
	LoadBalance LoadBalanceMode

	TunnelAllowedPort map[string]bool // allowed ports to create tunnel

	SshServer []string

	// authenticate client
	UserPasswd     string
	UserPasswdFile string // file that contains user:passwd:[port] pairs
	AllowedClient  string
	AuthTimeout    time.Duration

	// advanced options
	DialTimeout time.Duration
	ReadTimeout time.Duration

	Core         int
	AddrInPAC    []string
	DetectSSLErr bool

	// not configurable in config file
	PrintVer        bool
	EstimateTimeout bool // if run estimateTimeout()

	hasHttpParent bool // not config option
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

	config.TunnelAllowedPort = make(map[string]bool)
	for _, port := range defaultTunnelAllowedPort {
		config.TunnelAllowedPort[port] = true
	}
}

// Whether command line options specifies listen addr
var cmdHasListenAddr bool

func parseCmdLineConfig() *Config {
	var c Config
	var listenAddr string
	flag.StringVar(&c.RcFile, "rc", path.Join(dsFile.dir, rcFname), "configuration file")
	// Specifying listen default value to StringVar would override config file options
	flag.StringVar(&listenAddr, "listen", "", "proxy server listen address, default to "+defaultListenAddr)
	flag.IntVar(&c.Core, "core", 2, "number of cores to use")
	flag.StringVar(&c.LogFile, "logFile", "", "write output to file")
	flag.BoolVar(&c.PrintVer, "version", false, "print version")
	flag.BoolVar(&c.EstimateTimeout, "estimate", true, "enable/disable estimate timeout")

	flag.Parse()
	if listenAddr != "" {
		configParser{}.ParseListen(listenAddr)
		cmdHasListenAddr = true // must come after ParseListen
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

func checkServerAddr(addr string) error {
	_, _, err := net.SplitHostPort(addr)
	return err
}

func isUserPasswdValid(val string) bool {
	arr := strings.SplitN(val, ":", 2)
	if len(arr) != 2 || arr[0] == "" || arr[1] == "" {
		return false
	}
	return true
}

// proxyParser provides functions to parse different types of parent proxy
type proxyParser struct{}

func (p proxyParser) ProxySocks5(val string) {
	if err := checkServerAddr(val); err != nil {
		Fatal("parent socks server", err)
	}
	addParentProxy(newSocksParent(val))
}

func (pp proxyParser) ProxyHttp(val string) {
	var userPasswd, server string

	arr := strings.Split(val, "@")
	if len(arr) == 1 {
		server = arr[0]
	} else if len(arr) == 2 {
		userPasswd = arr[0]
		server = arr[1]
	} else {
		Fatal("http parent proxy contains more than one @:", val)
	}

	if err := checkServerAddr(server); err != nil {
		Fatal("parent http server", err)
	}

	config.hasHttpParent = true

	parent := newHttpParent(server)
	parent.initAuth(userPasswd)
	addParentProxy(parent)
}

// parse shadowsocks proxy
func (pp proxyParser) ProxySs(val string) {
	arr := strings.Split(val, "@")
	if len(arr) < 2 {
		Fatal("shadowsocks proxy needs to method and password")
	} else if len(arr) > 2 {
		Fatal("shadowsocks proxy contains too many @")
	}

	methodPasswd := arr[0]
	server := arr[1]

	arr = strings.Split(methodPasswd, ":")
	if len(arr) != 2 {
		Fatal("shadowsocks proxy method password should separate by :")
	}
	method := arr[0]
	passwd := arr[1]

	parent := newShadowsocksParent(server)
	if err := parent.initCipher(method, passwd); err != nil {
		Fatal("create shadowsocks cipher:", err)
	}
	addParentProxy(parent)
}

// configParser provides functions to parse options in config file.
type configParser struct{}

func (p configParser) ParseProxy(val string) {
	parser := reflect.ValueOf(proxyParser{})
	zeroMethod := reflect.Value{}

	arr := strings.Split(val, "://")
	if len(arr) != 2 {
		Fatal("proxy has no protocol specified:", val)
	}
	protocol := arr[0]

	methodName := "Proxy" + strings.ToUpper(protocol[0:1]) + protocol[1:]
	method := parser.MethodByName(methodName)
	if method == zeroMethod {
		Fatalf("no such protocol \"%s\"\n", arr[0])
	}
	args := []reflect.Value{reflect.ValueOf(arr[1])}
	method.Call(args)
}

func (p configParser) ParseLogFile(val string) {
	config.LogFile = val
}

func (p configParser) ParseListen(val string) {
	if cmdHasListenAddr {
		return
	}
	arr := strings.Split(val, ",")
	for _, s := range arr {
		s = strings.TrimSpace(s)
		host, _, err := net.SplitHostPort(s)
		if err != nil {
			Fatal("listen address", err)
		}
		if host == "" || host == "0.0.0.0" {
			if len(arr) > 1 {
				Fatalf("too much listen addresses: "+
					"%s represents all ip addresses on this host.\n", s)
			}
		}
		config.ListenAddr = append(config.ListenAddr, s)
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
		host, _, err := net.SplitHostPort(s)
		if err != nil {
			Fatal("proxy address in PAC", err)
		}
		if host == "0.0.0.0" {
			Fatal("can't use 0.0.0.0 as proxy address in PAC")
		}
		config.AddrInPAC[i] = s
	}
}

func (p configParser) ParseTunnelAllowedPort(val string) {
	arr := strings.Split(val, ",")
	for _, s := range arr {
		s = strings.TrimSpace(s)
		if _, err := strconv.Atoi(s); err != nil {
			Fatal("tunnel allowed ports", err)
		}
		config.TunnelAllowedPort[s] = true
	}
}

func (p configParser) ParseSocksParent(val string) {
	var pp proxyParser
	pp.ProxySocks5(val)
}

func (p configParser) ParseSshServer(val string) {
	arr := strings.Split(val, ":")
	if len(arr) == 2 {
		val += ":22"
	} else if len(arr) == 3 {
		if arr[2] == "" {
			val += "22"
		}
	} else {
		Fatal("sshServer should be in the form of: user@server:local_socks_port[:server_ssh_port]")
	}
	// add created socks server
	p.ParseSocksParent("127.0.0.1:" + arr[1])
	config.SshServer = append(config.SshServer, val)
}

var http struct {
	parent    *httpParent
	serverCnt int
	passwdCnt int
}

func (p configParser) ParseHttpParent(val string) {
	if err := checkServerAddr(val); err != nil {
		Fatal("parent http server", err)
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
		shadow.parent.initCipher(shadow.method, shadow.passwd)
	}
	if val == "" { // the final call
		shadow.parent = nil
		return
	}
	if err := checkServerAddr(val); err != nil {
		Fatal("shadowsocks server", err)
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
	parser.ParseShadowSocks("")
}

// Put actual authentication related config parsing in auth.go, so config.go
// doesn't need to know the details of authentication implementation.

func (p configParser) ParseUserPasswd(val string) {
	config.UserPasswd = val
	if !isUserPasswdValid(config.UserPasswd) {
		Fatal("userPassword syntax wrong, should be in the form of user:passwd")
	}
}

func (p configParser) ParseUserPasswdFile(val string) {
	exist, err := isFileExists(val)
	if err != nil {
		Fatal("userPasswdFile error:", err)
	}
	if !exist {
		Fatal("userPasswdFile", val, "does not exist")
	}
	config.UserPasswdFile = val
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

	IgnoreUTF8BOM(f)

	scanner := bufio.NewScanner(f)

	parser := reflect.ValueOf(configParser{})
	zeroMethod := reflect.Value{}

	var n int
	for scanner.Scan() {
		n++
		line := strings.TrimSpace(scanner.Text())
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
	if scanner.Err() != nil {
		Fatalf("Error reading rc file: %v\n", scanner.Err())
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
