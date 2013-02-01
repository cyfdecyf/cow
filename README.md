# COW (Climb Over the Wall) proxy

**COW 是一个 HTTP 代理服务器，它的目标是自动化翻墙**，你可以将系统自动代理配置成 COW 提供的 PAC url，之后就无需在多个代理配置间切换，也无需编辑被墙网站列表。

## 功能

- **自动检测网站是否被墙，仅对被墙网站使用二级代理**
  - 支持 SOCKS5 和 shadowsocks 作为二级代理
  - 对未知网站，先尝试直接连接，失败后使用二级代理重试请求，2 分钟后再尝试直接
  - 内置常见被墙网站，减少检测被墙所需时间，也可手工添加被墙网站
- **提供 PAC 文件**
  - PAC 文件含可直连网站，让浏览器绕过 COW 直接访问得到最好的性能
  - 内置常见可直连网站，如国内社交、视频、银行、电商等网站，也可手工添加
- **支持将 SOCKS5 代理转为 HTTP 代理**
  - 同时可以帮助执行 ssh 命令创建 SOCKS5 代理，断开后重连（需要公钥认证）
- **支持 shadowsocks**
  - COW 直接作为 shadowsocks client 提供 HTTP 代理
  - 可选择任意 shadowsocks 实现，如 [shadowsocks-go](https://github.com/shadowsocks/shadowsocks-go/), [shadowsocks-nodejs](https://github.com/clowwindy/shadowsocks-nodejs/), [shadowsocks-libuv](https://github.com/dndx/shadowsocks-libuv/)

## 限制

- COW 没有提供 cache
- 被墙网站检测在恶劣的网络连接环境下不可靠

# 安装

## 二进制文件

目前为运行在 x86 处理器上的的 OS X, Linux, Windows 提供二进制文件。二进制文件发布在 [Google Code](http://code.google.com/p/cow-proxy/downloads/list)。

OS X 和 Linux 上，使用下面的命令来下载二进制文件和样例配置（也可用来更新）：

    curl -s -L https://github.com/cyfdecyf/cow/raw/master/install-cow.sh | bash

## 从源码安装

安装 Go，设置好 `GOPATH`，执行以下命令（添加 `-u` 选项来更新）：

    go get github.com/cyfdecyf/cow

# 使用说明

配置文件在 Unix 系统上为 `~/.cow/rc`，Windows 上为 COW 所在目录的 `rc` 文件。 **[配置样例](https://github.com/cyfdecyf/cow/blob/master/doc/sample-config/rc) 包含了所有选项以及详细的说明**，建议下载然后修改。

启动 COW：

- Unix 系统在命令行上执行 `cow`
- Windows 上双击即可

PAC url 为 `http://<listen address>/pac`。

命令行选项可以覆盖配置文件中的选项，执行 `cow -h` 来获取更多信息。

## 手动指定被墙和直连网站

`~/.cow/blocked` 和 `~/.cow/direct` 可以用来指定被墙和直连网站：

- 每行一个域名或者主机名（COW 会先检查主机名是否在列表中，再检查域名）
- 可以使用类似 `google.com.hk` 这样的域名

如果想记录经常访问的网站来提高性能，请参考[样例配置](https://github.com/cyfdecyf/cow/blob/master/doc/sample-config/rc)中高级选项部分关于 `updateBlocked` 和 `updateDirect` 选项的说明。

## OS X 登录时启动 COW

1. 下载 [`info.chenyufei.cow.plist`](https://github.com/cyfdecyf/cow/blob/master/doc/osx/info.chenyufei.cow.plist) 这个文件放到 `~/Library/LaunchAgents` 目录
2. 修改其中的 `COWBINARY` 为 COW 的路径

# COW 如何检测被墙网站

COW 将以下错误认为是墙在作怪：

  - 服务器连接被重置 (connection reset)
  - 创建连接超时
  - 服务器读操作超时

无论是普通的 HTTP GET 等请求还是 CONNECT 请求，失败后 COW 都会自动重试请求。（如果已经有内容发送回 client 则不会重试而是直接断开连接。）

用连接被重置来判断被墙通常来说比较可靠，超时则不可靠。COW 每隔 30 秒会尝试估算合适的超时间隔，避免在网络连接差的情况下把直连网站由于超时也当成被墙。
COW 默认配置下检测到被墙后，过两分钟再次尝试直连也是为了避免误判。

如果超时自动重试给你造成了问题，请参考[样例配置](https://github.com/cyfdecyf/cow/blob/master/doc/sample-config/rc)高级选项中的 `readTimeout`, `dialTimeout` 以及 `autoRetry` 选项。
