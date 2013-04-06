# COW (Climb Over the Wall) proxy

COW 是一个利用二级代理帮助自动化翻墙的 HTTP 代理服务器。它能自动检测被墙网站，且仅对被墙网站使用二级代理。

当前版本：0.6.1
[![Build Status](https://travis-ci.org/cyfdecyf/cow.png?branch=develop)](https://travis-ci.org/cyfdecyf/cow)

## 功能

- 支持 HTTP, SOCKS5 和 [shadowsocks](https://github.com/shadowsocks/shadowsocks-go/) 作为二级代理
- 自动检测网站是否被墙，仅对被墙网站使用二级代理
  - 对未知网站，先尝试直接连接，失败后使用二级代理重试请求，2 分钟后再尝试直接
  - 内置[常见被墙网站](site_blocked.go)，减少检测被墙所需时间（可手工添加）
- 自动记录经常访问网站是否被墙
- 提供 PAC 文件，直连网站绕过 COW
  - 内置[常见可直连网站](site_direct.go)，如国内社交、视频、银行、电商等网站（可手工添加）

# 安装

## 二进制文件

提供 x86 上 OS X, Linux, Windows 的二进制文件，可从 [Google Code](http://code.google.com/p/cow-proxy/downloads/list) 下载。

OS X 和 Linux 上，推荐使用下面的命令来下载二进制文件和样例配置（也可用来更新）：

    curl -s -L https://github.com/cyfdecyf/cow/raw/master/install-cow.sh | bash

该脚本在 OS X 上会帮助将 COW 设置为登录时启动。

## 从源码安装

仅推荐熟悉 Go 的用户从源码安装。安装 Go，设置好 `GOPATH`，执行以下命令（`go get -u` 来更新）：

    go get github.com/cyfdecyf/cow

# 使用说明

配置文件在 Unix 系统上为 `~/.cow/rc`，Windows 上为 COW 所在目录的 `rc.txt` 文件。 **[样例配置](doc/sample-config/rc) 包含了所有选项以及详细的说明**，建议下载然后修改。

启动 COW：

- Unix 系统在命令行上执行 `cow`
  - [Linux 启动脚本](doc/init.d/cow) 在 Debian 上测试过，其他 Linux 发行版应该也可用
- Windows 上双击 `cow.exe` 执行即可，或者使用 [`cow-taskbar.exe`](script/cow-taskbar.exe) (可隐藏窗口到系统托盘)

PAC url 为 `http://<listen address>/pac`，也可设置 `listen address` 为 HTTP/HTTPS 代理。

命令行选项可以覆盖部分配置文件中的选项、打开 debug 日志，执行 `cow -h` 来获取更多信息。

## 手动指定被墙和直连网站

**COW 的目标是自动化翻墙，一般情况下无需手工指定被墙和直连网站，该功能只是是为了处理特殊情况和性能优化。**

`~/.cow/blocked` 和 `~/.cow/direct` 可指定被墙和直连网站：

- 每行一个域名或者主机名（COW 会先检查主机名是否在列表中，再检查域名）
  - 二级域名如 `google.com` 相当于 `*.google.com`
  - `com.hk`, `edu.cn` 等二级域名下的三级域名，作为二级域名处理。如 `google.com.hk` 相当于 `*.google.com.hk`
  - 其他三级及以上域名/主机名做精确匹配，例如 `plus.google.com`

注意：对 IPv4 地址及 simple host name，COW 总是直接连接，生成的 PAC 也让浏览器直接访问。（因此开发者访问 localhost 和局域网内机器会绕过 COW。）

# 限制

COW 不提供 cache, 目前不支持 HTTP pipeline

## Chrome 用户请注意

Chrome 为了优化网络性能，在代理服务器使用上有一些特殊行为。目前已知的问题：COW 需要等待超时来发现某些网站被墙，而 Chrome 在等待代理服务器返回响应时间过长时会直接连接，这导致有时 Chrome+COW 还是无法访问某些被墙网站。

问题原因我不完全确定，如果你了解 Chrome 的工作机制，欢迎提供建议。

# 技术细节

## 访问网站记录

COW 在 `~/.cow/stat` json 文件中记录经常访问网站被墙和直连访问的次数。

- 直连访问成功一定次数后相应的 host 会包含到 PAC 文件
  - 使用 PAC 可以获得更好的性能， **但若某网站变成被封网站，浏览器会依然尝试直连**。遇到这种情况可以暂时总是使用 COW 代理，让 COW 学习到新的被封网站
- host 被墙一定次数后会直接用二级代理访问
  - 为避免误判，会以一定概率再次尝试直连访问
- host 若一段时间没有访问会自动被删除
- 内置网站列表和用户指定的网站不会出现在统计文件中

## COW 如何检测被墙网站

COW 将以下错误认为是墙在作怪：

- 服务器连接被重置 (connection reset)
- 创建连接超时
- 服务器读操作超时

无论是普通的 HTTP GET 等请求还是 CONNECT 请求，失败后 COW 都会自动重试请求。（如果已经有内容发送回 client 则不会重试而是直接断开连接。）

用连接被重置来判断被墙通常来说比较可靠，超时则不可靠。COW 每隔一分钟会尝试估算合适的超时间隔，避免在网络连接差的情况下把直连网站由于超时也当成被墙。
COW 默认配置下检测到被墙后，过两分钟再次尝试直连也是为了避免误判。

如果超时自动重试给你造成了问题，请参考[样例配置](doc/sample-config/rc)高级选项中的 `readTimeout`, `dialTimeout` 选项。

# 致谢

贡献代码：

@tevino: http parent proxy basic authentication

Bug report:

@glacjay, @fantasticfears, @hieixu, @Blaskyy, @lucifer9, @zellux

@glacjay 对 0.3 版本的 COW 提出了让它更加自动化的建议，使我重新考虑 COW 的设计目标并且改进成 0.5 版本中的工作方式。
