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

# Limitations #

- Designed to run on your own computer
- No caching, COW just passes traffic between clients and web servers
  - For web browsing, browsers have their own cache
- Beta quality now
  - I'm using COW as system wide proxy on OS X 10.8 everyday
  - Issue reporting is welcomed

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

- The PAC file can be access at proxy listen address
  - For the above example, access `http://127.0.0.1:7777/anypath` will get the generated PAC file
- Command line options can override options in the configuration file. For more details, see the output of `cow -h`

## Blocked and directly accessible sites list

Blocked and directly accessible web sites are specified using their domain names.

- You can manually specify blocked and directly accessible domains. Just edit `~/.cow/blocked` and `~/.cow/direct`
  - One line for each domain
  - You can use domains like `google.com.hk`
- When update blocked/direct domains is enabled (default behavior), COW will update `~/.cow/auto-blocked` and `~/.cow/auto-direct` on exit
  - They will only contain domains which you visit
  - Generated PAC file will contain domains in both `direct` and `auto-direct`
- For domains which will be temporarily blocked, put them in `~/.cow/chou`. (They will always go through COW. If you are Chinese, chou stands for 抽风)
- Domains appear in `blocked/direct/chou` will not be modified by COW, and will be automatically removed from `auto-blocked` and `auto-direct`
  - Domains appear in both `blocked` and `direct` are taken as blocked, COW will output an error message for such domains
  - You'd better maintain consistency of `blocked/direct/chou`

# OS X: Start COW upon login

1. Put `doc/osx/info.chenyufei.cow.plist` into `~/Library/LaunchAgents`
2. Edit this plist file, change the COW executable path to the one on your system

After this, COW will be started when you login. It will also be restarted upon exit by `launchd` (if network is avaiable).
