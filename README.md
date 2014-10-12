# MEOW Proxy

<pre>
       /\
   )  ( ')     MEOW 是 [COW](https://github.com/cyfdecyf/cow) 的一个派生版本
  (  /  )      MEOW 与 COW 最大的不同之处在于，COW 采用黑名单模式， 而 MEOW 采用白名单模式
   \(__)|      国内网站直接连接，其他的网站使用代理连接
</pre>

如果你的代理有充足的流量和比直接连接国外网站更快的速度，为和不将国外网站统统用代理来连接呢？麻麻再也不用担心网站被墙了！发挥出你goagent和shadowsocks更大的潜力吧！

## 获取

- **Windows:** [下载地址](http://meowproxy.me/)
- **OS X, Linux:** 待添加
- **从源码安装:** 安装 [Go](http://golang.org/doc/install)，然后 `go get github.com/renzhn/meow`

## 配置

编辑 `rc.txt` (Windows) 或 `~/.meow/rc` (其他)，例子：

    # 监听地址，设为0.0.0.0可以监听所有端口，共享给局域网使用
    listen = http://127.0.0.1:4411
    # 可以指定多个上级代理
    # SOCKS5 二级代理
    proxy = socks5://127.0.0.1:1080
    # HTTP 二级代理
    proxy = http://127.0.0.1:8087
    # shadowsocks 二级代理
    proxy = ss://aes-128-cfb:password@example.server.com:25

## 直连列表

直接连接的域名列表保存在 `direct.txt` (Windows) 或 `~/.meow/direct` (其他)，例子：

-  `baidu.com` => `*.baidu.com`
-  `com.cn` => `*.com.cn`
-  `edu.cn` => `*.edu.cn`
-  `music.163.com` => `music.163.com`

不在直连列表的网站将使用代理连接

## TODO

- Windows wrapper app
- 完善PAC
- 通过IP判断国内网站
- Linux & OS X release