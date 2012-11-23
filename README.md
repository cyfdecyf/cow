# COW (Climb Over the Wall) proxy  #

COW is a HTTP proxy that tries to **automatically identify blocked websites and use a parent proxy when visiting those sites**. For directly accessible sites, COW will use direct access.

# Features #

- **Automatically identify blocked websites**
- **Record which sites are blocked, which can be directly accessed**
  - Can also manually specify blocked and directly accessible sites
- **Generate and serve PAC file**
  - The PAC file tells the client to use direct connection for directly accessible sites
  - Visiting those sites will not have performance overhead of using a proxy
- Convert socks proxy to HTTP proxy
  - Can start socks proxy server through ssh, requires public key authentication

# Installation #

Install [go](http://golang.org/doc/install), then run

    go get github.com/cyfdecyf/cow

# Usage #

Configuration file is located at `~/.cow/rc`. Here's an example with the most important options (all options are given default value, for a complete example, refer to `doc/sample-config/rc`):

    # proxy listen address
    listen = 127.0.0.1:7777
    # parent socks proxy address
    socks = 127.0.0.1:1080
    # ssh to the given server to start socks proxy
    sshServer =
    # empty path means stdout, use /dev/null to disable output
    logFile = ~/.cow/log

To start cow, just execute `cow` on the command line.

- The PAC file can be access at `http://<proxy listen address>/pac`
  - For the above example: `http://127.0.0.1:7777/pac`
- Command line options can override options in the configuration file. For more details, see the output of `cow -h`

## OS X: Start COW on login ##

1. Put `doc/osx/info.chenyufei.cow.plist` in `~/Library/LaunchAgents` directory
2. Edit this plist file, change the COW executable path to the one on your system

After this, COW will be started when you login. It will also be restarted upon exit by `launchd` (if network is avaiable).

## Blocked and directly accessible sites list ##

Blocked and directly accessible web sites are specified using their domain names. **COW can't always reliably detect blocked or directly accessible web sites, so you may need to edit those domain list file manually.**

- You can manually specify blocked and directly accessible domains. Just edit `~/.cow/blocked` and `~/.cow/direct`. **You can put sites that will be error identified as blocked or directly accessible into these files**.
  - One line for each domain
  - You can use domains like `google.com.hk`
- When update blocked/direct domains is enabled (default behavior), COW will update `~/.cow/auto-blocked` and `~/.cow/auto-direct` on exit
  - They will only contain domains which you visit
  - Generated PAC file will contain domains in both `direct` and `auto-direct`
- **For domains which will be temporarily blocked, put them in `~/.cow/chou`**. (They will always go through COW. If you are Chinese, chou stands for 抽风)
  - `doc/sample-config/chou` contains several such sites
- Domains appear in `blocked/direct/chou` will not be modified by COW, and will be automatically removed from `auto-blocked` and `auto-direct`
  - Domains appear in both `blocked` and `direct` are taken as blocked, COW will output an error message for such domains
  - You'd better maintain consistency of `blocked/direct/chou`

# How does COW detect blocked sites

Upon the following error, one domain is considered to be blocked
  - Server connection reset
  - Connection to server timeout
  - Read from server timeout

Server connection reset is usually reliable in detecting blocked sites. But timeout is not. **When network condition is bad, connecting to or reading from directly accessible sites may also timeout even if it's not blocked**. Because of this, COW treats connection reset and timeout differently:

- For connection reset, COW will add the domain into blocked domain list and retry HTTP request if possible
- For timeout error, COW will send back an error page. That page will let user decide whether the domain should be added to blocked list or direct list
  - For HTTP CONNECT method, COW can't send back error page back to client, so it will also add the domain to blocked list in case of read timeout. This may incorrectly add directly accessible sites to blocked ones
  - **If parts of a web page contains elements from a blocked sites, the browser may not display the error page.** In that case, user won't have the chance to add domain to blocked list. Enabling auto retry upon timeout would solve this problem

You can let COW retry HTTP request upon tiemout error by setting the `autoRetry` option to true. But don't enable this if you would use COW in a non-reliable network.

# Limitations #

- Designed to run on your own computer
- No caching, COW just passes traffic between clients and web servers
  - For web browsing, browsers have their own cache
- Blocked site detection is not always reliable
- Beta quality now
  - Stable enough for myself. I'm using COW as system wide proxy on OS X 10.8 everyday
  - Issue reporting is welcomed
