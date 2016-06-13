# MEOW Proxy

当前版本：1.3.4
[![Build Status](https://travis-ci.org/renzhn/MEOW.png?branch=master)](https://travis-ci.org/renzhn/MEOW)

<pre>
       /\
   )  ( ')     MEOW 是 [COW](https://github.com/cyfdecyf/cow) 的一个派生版本
  (  /  )      MEOW 与 COW 最大的不同之处在于，COW 采用黑名单模式， 而 MEOW 采用白名单模式
   \(__)|      国内网站直接连接，其他的网站使用代理连接
</pre>

## MEOW 可以用来
- 作为全局 HTTP 代理（支持 PAC），可以智能分流（直连国内网站、使用代理连接其他网站）
- 将 SOCKS5 等代理转换为 HTTP 代理，HTTP 代理能最大程度兼容各种软件（可以设置为程序代理）和设备（设置为系统全局代理）
- 架设在内网（或者公网），为其他设备提供智能分流代理
- 编译成一个无需任何依赖的可执行文件运行，支持各种平台（Win / Linux / OS X），甚至是树莓派（Linux ARM）

## 更新说明
- 2016-02-18 Version 1.3.4

       * 使用 Go 1.6 编译，请重新下载
       
- 2015-12-03 Version 1.3.4

       * 修正客户端连接未正确关闭 bug
       * 修正对文件描述符过多错误的判断（too many open files）

- 2015-11-22 Version 1.3.3

       * 增加 `reject` 拒绝连接列表
       * 支持作为 HTTPS 代理服务器监听
       * 支持 HTTPS 代理服务器作为父代理
	
	
- 2015-10-09 Version 1.3.2

       * 完全托管在 github，不再使用 meowproxy.me 域名，[新的下载地址](https://github.com/renzhn/MEOW/tree/gh-pages/dist/)

- 2015-08-23 Version 1.3.1

       * 去除了端口限制
       * 使用最新的 Go 1.5 编译

- 2015-07-16 Version 1.3

       更新了默认的直连列表、加入了强制使用代理列表，强烈推荐旧版本用户更新 [direct](https://raw.githubusercontent.com/renzhn/MEOW/master/doc/sample-config/direct) 文件和下载 [proxy](https://raw.githubusercontent.com/renzhn/MEOW/master/doc/sample-config/proxy) 文件（或者重新安装）

## 获取

- **OS X, Linux:** 执行以下命令（也可用于更新）

        curl -L git.io/meowproxy | bash

  环境变量 `MEOW_INSTALLDIR` 可以指定安装的路径，若该环境变量不是目录则询问用户
- **Windows:** [下载地址](https://github.com/renzhn/MEOW/tree/gh-pages/dist/windows/)
- **从源码安装:** 安装 [Go](http://golang.org/doc/install)，然后 `go get github.com/renzhn/MEOW`

## 配置

编辑 `~/.meow/rc` (OS X, Linux) 或 `rc.txt` (Windows)，例子：

    # 监听地址，设为0.0.0.0可以监听所有端口，共享给局域网使用
    listen = http://127.0.0.1:4411
    # 至少指定一个上级代理
    # SOCKS5 上级代理
    # proxy = socks5://127.0.0.1:1080
    # HTTP 上级代理
    # proxy = http://127.0.0.1:8087
    # shadowsocks 上级代理
    # proxy = ss://aes-128-cfb:password@example.server.com:25
    # HTTPS 上级代理
    # proxy = https://user:password@example.server.com:port

## 工作方式

当 MEOW 启动时会从配置文件加载直连列表和强制使用代理列表，详见下面两节。

当通过 MEOW 访问一个网站时，MEOW 会：

- 检查域名是否在直连列表中，如果在则直连
- 检查域名是否在强制使用代理列表中，如果在则通过代理连接
- **检查域名的 IP 是否为国内 IP**
    - 通过本地 DNS 解析域名，得到域名的 IP
    - 如果是国内 IP 则直连，否则通过代理连接
    - 将域名加入临时的直连或者强制使用代理列表，下次可以不用 DNS 解析直接判断域名是否直连

## 直连列表

直接连接的域名列表保存在 `~/.meow/direct` (OS X, Linux) 或 `direct.txt` (Windows)


匹配域名**按 . 分隔的后两部分**或者**整个域名**，例子：

-  `baidu.com` => `*.baidu.com`
-  `com.cn` => `*.com.cn`
-  `edu.cn` => `*.edu.cn`
-  `music.163.com` => `music.163.com`

一般是**确定**要直接连接的网站

## 强制使用代理列表

强制使用代理连接的域名列表保存在 `~/.meow/proxy` (OS X, Linux) 或 `proxy.txt` (Windows)，语法格式与直连列表相同
（注意：匹配的是域名**按 . 分隔的后两部分**或者**整个域名**）

## 致谢

- @cyfdecyf - COW author
- Github - Github Student Pack
- https://www.pandafan.org/pac/index.html - Domain White List
- https://github.com/Leask/Flora_Pac - CN IP Data
