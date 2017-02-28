# MEOW Proxy

当前版本：1.5 [CHANGELOG](CHANGELOG.md)
[![Build Status](https://travis-ci.org/renzhn/MEOW.png?branch=master)](https://travis-ci.org/renzhn/MEOW)

<pre>
       /\
   )  ( ')     MEOW是[COW](https://github.com/cyfdecyf/cow)的一个派生版本
  (  /  )      MEOW与COW最大的不同之处在于，COW采用黑名单模式，而MEOW采用白名单模式
   \(__)|      国内网站直接连接，其他的网站使用代理连接
</pre>

## MEOW 可以用来
- 作为全局HTTP代理（支持PAC），可以智能分流（直连国内网站、使用代理连接其他网站）
- 将SOCKS5等代理转换为HTTP代理，HTTP代理能最大程度兼容各种软件（可以设置为程序代理）和设备（设置为系统全局代理）
- 架设在内网（或者公网），为其他设备提供智能分流代理
- 编译成一个无需任何依赖的可执行文件运行，支持各种平台（Win/Linux/OS X），甚至是路由器或者树莓派（Linux ARM）

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

当MEOW启动时会从配置文件加载直连列表和强制使用代理列表，详见下面两节。

当通过MEOW访问一个网站时，MEOW会：

- 检查域名是否在直连列表中，如果在则直连
- 检查域名是否在强制使用代理列表中，如果在则通过代理连接
- **检查域名的 IP 是否为国内 IP**
    - 通过本地 DNS 解析域名，得到域名的 IP
    - 如果是国内 IP 则直连，否则通过代理连接
    - 将域名加入临时的直连或者强制使用代理列表（缓存），下次访问相同网站可以不用 DNS 解析直接判断域名是否直连

## 直连列表

直接连接的域名列表保存在 `~/.meow/direct` (OS X, Linux) 或 `direct.txt` (Windows)


匹配域名**按 . 分隔的后两部分**或者**整个域名**，例子：

-  `baidu.com` => `*.baidu.com`
-  `com.cn` => `*.com.cn`
-  `edu.cn` => `*.edu.cn`
-  `music.163.com` => `music.163.com`

一般是**确定**要直接连接的网站

## 与COW相比主要区别

- 所有国外网站走代理，加快访问速度
- 去掉了“先尝试直连，如果无法连接则走代理”的逻辑，避免尝试过程中浪费时间
- 无状态，不依赖用来做统计的stats文件（偶尔还会出现stats文件破损的情况）
- 增加reject列表可以用来屏蔽广告（感谢@wjchen）
- 增加了作为https代理以及支持上游https代理功能（感谢@wjchen）

## 强制使用代理列表

强制使用代理连接的域名列表保存在 `~/.meow/proxy` (OS X, Linux) 或 `proxy.txt` (Windows)，语法格式与直连列表相同
（注意：匹配的是域名**按 . 分隔的后两部分**或者**整个域名**）

## 致谢

- @cyfdecyf - COW author
- https://www.pandafan.org/pac/index.html - Domain White List
