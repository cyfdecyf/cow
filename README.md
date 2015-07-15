# MEOW Proxy

当前版本：1.2 [CHANGELOG](CHANGELOG)
[![Build Status](https://travis-ci.org/renzhn/MEOW.png?branch=master)](https://travis-ci.org/renzhn/MEOW)

<pre>
       /\
   )  ( ')     MEOW 是 [COW](https://github.com/cyfdecyf/cow) 的一个派生版本
  (  /  )      MEOW 与 COW 最大的不同之处在于，COW 采用黑名单模式， 而 MEOW 采用白名单模式
   \(__)|      国内网站直接连接，其他的网站使用代理连接
</pre>

如果你的代理有充足的流量和比直接连接国外网站更快的速度，为和不将国外网站统统用代理来连接呢？麻麻再也不用担心网站被墙了！发挥出你goagent和shadowsocks更大的潜力吧！

## 获取

- **OS X, Linux:** 执行以下命令（也可用于更新）

        curl -L git.io/meowproxy | bash

  环境变量 `MEOW_INSTALLDIR` 可以指定安装的路径，若该环境变量不是目录则询问用户
- **Windows:** [下载地址](http://meowproxy.me/dist/windows/)
- **从源码安装:** 安装 [Go](http://golang.org/doc/install)，然后 `go get github.com/renzhn/MEOW`

## 配置

编辑 `~/.meow/rc` (OS X, Linux) 或 `rc.txt` (Windows)，例子：

    # 监听地址，设为0.0.0.0可以监听所有端口，共享给局域网使用
    listen = http://127.0.0.1:4411
    # 至少指定一个上级代理
    # SOCKS5 二级代理
    proxy = socks5://127.0.0.1:1080
    # HTTP 二级代理
    proxy = http://127.0.0.1:8087
    # shadowsocks 二级代理
    proxy = ss://aes-128-cfb:password@example.server.com:25

## 工作方式

当通过 MEOW 访问一个网站时，MEOW 会：

- 检查域名是否在直连列表中
- **检查域名的IP是否为国内IP**

如果有一条满足要求，MEOW 会直接连接。否则 MEOW 会通过代理连接。

## 直连列表

直接连接的域名列表保存在 `~/.meow/direct` (OS X, Linux) 或 `direct.txt` (Windows)，例子：

-  `baidu.com` => `*.baidu.com`
-  `com.cn` => `*.com.cn`
-  `edu.cn` => `*.edu.cn`
-  `music.163.com` => `music.163.com`

一般是**确定**要直接连接的网站

## 与 COW 相比的修改

- 通过IP判断国内网站
- 修改了判断域名的方式，只匹配句号分隔的后两部分
- 移除了`blocked`、`sitestat`文件及相关的功能

## 一些细节

- 程序的输出结果：DIRECT表示直连，PROXY表示通过代理连接；GET ... 200 OK表示成功获取数据，以此类推
- 如果检查到域名的IP是国内的IP（当然是不在直连列表里的域名），MEOW 会将此域名缓存到内存中的直连列表。PAC 文件中包含了从`direct`文件中读取和内存中缓存的域名直连列表的定义，浏览器设置为PAC后会对这些域名直接连接否则使用 MEOW 代理。
- MEOW 判断是否该直连的效率很高。判断直连域名用Map，判断国内IP用二分查找并且缓存，因此不用担心判断域名导致网速变慢。甚至去掉`direct`文件 MEOW 也可以工作。
- 以`-h`运行查看 MEOW 的命令行参数

## 致谢

- @cyfdecyf - COW author
- Github - Github Student Pack
- https://www.pandafan.org/pac/index.html - Domain White List
- https://github.com/Leask/Flora_Pac - CN IP Data
