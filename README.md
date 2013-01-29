# COW (Climb Over the Wall) proxy

COW is a HTTP proxy that tries to automatically identify blocked websites and use a parent proxy when visiting those sites. For non-blocked sites, COW will use direct access.

If you are tired of switching proxy configuration or editing blocked site list, you can give COW a try.

For quick start and Chinese use guide, visite [COW's project page](TODO).

## Features

- **Automatically identify blocked websites**
  - Use parent proxy when blockage detected, try direct access after 2 minutes
    - Also works well for temporarily blocked site like Google
  - Builtin list of common blocked sites to reduce block detection time
  - You can also manually specify blocked/non-blocked sites
- **Generate and serve PAC file**
  - Contain domains that can be directly accessed
- **Convert socks proxy to HTTP proxy**
  - Can start socks proxy server by ssh, requires public key authentication
- **Support [shadowsocks](https://github.com/shadowsocks/shadowsocks-go/)**
  - COW itself can act as shadowsocks client, but provides HTTP proxy
  - Choose any shadowsock implementation, like [shadowsocks-go](https://github.com/shadowsocks/shadowsocks-go/), [shadowsocks-nodejs](https://github.com/clowwindy/shadowsocks-nodejs/) or [shadowsocks-libuv](https://github.com/dndx/shadowsocks-libuv/)

# Installation

## Pre-compiled binary

Pre-compiled binaries are provided for OS X, Linux and Windows running on Intel processors.

For OS X and Linux, run the following command to download the pre-compiled binary and sample config file (re-run to update)

    curl -s -L https://github.com/cyfdecyf/cow/raw/master/install-cow.sh | bash

Windows binary is also provided, look at the [downloads page on Google Code](http://code.google.com/p/cow-proxy/downloads/list).

The install script will do the following:

1. Ask you the installation directory
2. Download the matching binary
   - Will run sudo if no write permission to the installation directory
3. If `~/.cow` does not exist, it will create that directory and download sample configuration files
4. On OS X, if you confirmed to start COW upon login, it will also install a plist file into `~/Library/LaunchAgents`

## From source

    go get github.com/cyfdecyf/cow

# Usage

Configuration file is located at `~/.cow/rc`. **The [sample config](TODO) contains complete example with comments**, you can simply copy it to `~/.cow` and modify it according to your settings.

Here's an example with the most important options:

    # proxy listen address
    listen = 127.0.0.1:7777
    # parent socks proxy address
    socks = 127.0.0.1:1080
    # ssh to the server to start socks proxy (requires public key authentication)
    # If option is not empty, COW will run the following command:
    # "ssh -n -N -D <port in socks option> <sshServer>"
    sshServer =
    # empty path means stdout, use /dev/null to disable output
    logFile = ~/.cow/log

To start cow, just execute `cow` on the command line.

- The PAC file can be accessed at `http://<listen>/pac`
  - For the above example: `http://127.0.0.1:7777/pac`
- Command line options can override options in the configuration file
  - For more details, see the output of `cow -h`

## OS X: Start COW on login

1. Put `doc/osx/info.chenyufei.cow.plist` in `~/Library/LaunchAgents` directory
2. Edit this plist file, change `COWBINARY` to where cow is installed

After this, COW will be started when you login. It will also be restarted upon exit by `launchd` (if network is available).

## Blocked and directly accessible sites list

**Please refer to comments in the advanced configuration section in the [sample config](TODO) for recommend usage.**

Blocked and directly accessible web sites are specified using their domain names. **COW can't always reliably detect blocked or directly accessible web sites, so you may need to edit these domain list files manually.**

- You can manually specify blocked and directly accessible domains. Just edit `~/.cow/blocked` and `~/.cow/direct`. **You can put sites that will be incorrectly identified as blocked or directly accessible into these files**.
  - One line for each domain
  - You can use domains like `google.com.hk`
- When `updateBlocked` and `updateDirect` option is enabled (default disabled), COW will update `~/.cow/auto-blocked` and `~/.cow/auto-direct` on exit
  - They will only contain domains which you visit
  - Generated PAC file will contain domains in both `direct` and `auto-direct`
- **For domains which will be temporarily blocked, put them in `~/.cow/chou`**. (They will always go through COW, and COW will decide whether to use parent socks server. If you are Chinese, chou stands for 抽风)
  - `doc/sample-config/chou` contains several such sites
- Domains appear in `blocked/direct/chou` will not be modified by COW, and will be automatically removed from `auto-blocked` and `auto-direct`
  - Domains appear in both `blocked` and `direct` are taken as blocked, COW will output an error message for such domains
  - You'd better maintain consistency of `blocked/direct/chou` yourself

# How does COW detect blocked sites

Upon the following error, one domain is considered to be blocked
  - Server connection reset
  - Connection to server timeout
  - Read from server timeout

Server connection reset is usually reliable in detecting blocked sites. But timeout is not. **When network condition is bad, connecting to or reading from directly accessible sites may also timeout even if it's not blocked**. Because of this, COW treats connection reset and timeout differently:

- For connection reset, COW will add the domain into blocked domain list and retry HTTP request if no response has been sent to client
- For timeout error, COW will send back an error page. That page will let the user decide whether the domain should be added to blocked list or direct list
  - **If parts of a web page contains elements from a blocked sites, the browser may not display the error page.** In that case, user won't have the chance to add domain to blocked list. Enabling auto retry upon timeout would solve this problem

**You can let COW retry HTTP request upon timeout error by setting the `autoRetry` option to true**. But don't enable this if you would use COW in a non-reliable network.

## Detecting blocked SSL connection

Browsers send HTTP CONNECT method to proxy to create SSL connection to server. As a proxy only passes network traffic between the client and server after the connection is created, it does not know what happens in the connection.

- Upon server connection reset or timeout for HTTP CONNECT request, if the server has never sent any response to the client, COW will retry the request using socks server
- One unreliable mechanism used by COW to detect SSL error is based on the following observation: **upon SSL error, the client will close the connection immediately**. If COW notices such situation, it will consider the requested host as blocked. When the client retry the request later, COW will use socks server to create connection to the server

Because COW can not send back error page for HTTP CONNECT method after connection is established, it can't let the user decide whether a domain should be added to blocked list. So when detected blocked site, COW will directly add it to blocked list regardless of the `autoRetry` option.

# Limitations

- No caching, COW just passes traffic between clients and web servers
  - For web browsing, browsers have their own cache
- Blocked site detection is not always reliable
