# COW (Climb Over the Wall) proxy

COW 是一个帮助自动化翻墙的 HTTP 代理服务器。

## 功能

- 支持 SOCKS5 和 [shadowsocks](https://github.com/shadowsocks/shadowsocks-go/) 作为二级代理
  - 可以帮助执行 ssh 命令创建 SOCKS5 代理，断开后重连（需要公钥认证）
  - 无需安装 shadowsocks client，提供 HTTP 代理
- 自动检测网站是否被墙，仅对被墙网站使用二级代理
  - 对未知网站，先尝试直接连接，失败后使用二级代理重试请求，2 分钟后再尝试直接
  - 内置[常见被墙网站](https://github.com/cyfdecyf/cow/blob/master/site_blocked.go)，减少检测被墙所需时间，也可手工添加被墙网站
- 自动记录经常访问网站是否被墙
- 提供 PAC 文件，直连网站绕过 COW
  - 内置[常见可直连网站](https://github.com/cyfdecyf/cow/blob/master/site_direct.go)，如国内社交、视频、银行、电商等网站，也可手工添加

## 限制

- COW 没有提供 cache
- 被墙网站检测在糟糕的网络连接环境下不可靠

# 安装

## 二进制文件

目前为运行在 x86 处理器上的 OS X, Linux, Windows 提供二进制文件。二进制文件发布在 [Google Code](http://code.google.com/p/cow-proxy/downloads/list)。

OS X 和 Linux 上，推荐使用下面的命令来下载二进制文件和样例配置（也可用来更新）：

    curl -s -L https://github.com/cyfdecyf/cow/raw/master/install-cow.sh | bash

该脚本在 OS X 上会帮助将 COW 设置为登录时启动。

## 从源码安装

安装 Go，设置好 `GOPATH`，执行以下命令（`go get -u` 来更新）：

    go get github.com/cyfdecyf/cow

# 使用说明

配置文件在 Unix 系统上为 `~/.cow/rc`，Windows 上为 COW 所在目录的 `rc` 文件。 **[样例配置](https://github.com/cyfdecyf/cow/blob/master/doc/sample-config/rc) 包含了所有选项以及详细的说明**，建议下载然后修改。

启动 COW：

- Unix 系统在命令行上执行 `cow`
- Windows 上双击 `cow.exe` 执行即可

PAC url 为 `http://<listen address>/pac`。

命令行选项可以覆盖配置文件中的选项，执行 `cow -h` 来获取更多信息。

## 手动指定被墙和直连网站

`~/.cow/blocked` 和 `~/.cow/direct` 可指定被墙和直连网站：

- 每行一个域名或者主机名（COW 会先检查主机名是否在列表中，再检查域名）
- 可以使用类似 `google.com.hk` 这样的域名

注意：对 IPv4 地址，COW 默认尝试直接连接，生成的 PAC 也让浏览器直接访问 IPv4 url。(这个功能的设计初衷是开发人员经常会访问本地或局域网 IP 地址。)

# 访问网站记录

COW 在 `~/.cow/stat` json 文件中记录经常访问网站被墙和直连访问的次数。

- 直连访问成功一定次数后相应的 host 会包含到 PAC 文件
  - 使用 PAC 可以获得更好的性能， **但若某网站变成被封网站，浏览器会依然尝试直连**。遇到这种情况可以暂时总是使用 COW 代理，让 COW 学习到新的被封网站
- host 被墙一定次数后会直接用二级代理访问
  - 为避免误判，会以一定概率再次尝试直连访问
- host 若一段时间没有访问会自动被删除
- 内置网站列表和用户指定的网站不会出现在统计文件中

# COW 如何检测被墙网站

COW 将以下错误认为是墙在作怪：

- 服务器连接被重置 (connection reset)
- 创建连接超时
- 服务器读操作超时

无论是普通的 HTTP GET 等请求还是 CONNECT 请求，失败后 COW 都会自动重试请求。（如果已经有内容发送回 client 则不会重试而是直接断开连接。）

用连接被重置来判断被墙通常来说比较可靠，超时则不可靠。COW 每隔一分钟会尝试估算合适的超时间隔，避免在网络连接差的情况下把直连网站由于超时也当成被墙。
COW 默认配置下检测到被墙后，过两分钟再次尝试直连也是为了避免误判。

如果超时自动重试给你造成了问题，请参考[样例配置](https://github.com/cyfdecyf/cow/blob/master/doc/sample-config/rc)高级选项中的 `readTimeout`, `dialTimeout` 选项。
