# COW (Climb Over the Wall) proxy  #

COW is a HTTP proxy that tries to **automatically identify blocked web sites and use a parent proxy when visiting those sites**. For non-blocked sites, COW will use direct access. COW can also generate PAC file to tell the client use direct access for non-blocked sites.

# Features #

- **Automatically identify blocked websites**
- **Record which sites are blocked, which can be directly accessed**
  - Can also manually specify blocked and direct sites
- **Generate and serve PAC file**
  - The PAC file tells the browser to use direct connection for non-blocked sites
- Convert socks proxy to HTTP proxy
  - Can start socks proxy through ssh, requires public key authentication

# Limitations #

- Designed to run on your own computer
- No caching, cow just passes traffic between the browser and web servers
  - Browsers have cache
- Alpha quality now

# Installation #

Install [go](http://golang.org/doc/install), then run

    go get github.com/cyfdecyf/cow

# Usage #

Configuration file is located at `~/.cow/rc`. Here's an example:

    # proxy listen address
    listen = 127.0.0.1:7777
    # parent socks proxy address
    socks = 127.0.0.1:1080
    # Nubmer of cores to use
    core = 2
    # ssh to the given server to start socks proxy
    sshServer = gfw
    # Update blocked site list (~/.cow/auto-blocked)
    updateBlocked = true
    # Update direct accessable site list (~/.cow/auto-direct)
    updateDirect = true
    # empty path means stdout, use /dev/null to disable output
    logFile = ~/.cow/log

To start cow, just execute `cow` on the command line.

- The PAC file can be access at the same address as proxy listen address
  - For the above example, accessing `http://127.0.0.1:7777/anypath` will get the generated PAC file
- You can manually specify blocked and direct accessable sites. Just edit `~/.cow/blocked` and `~/.cow/direct`
  - One line for each domain
- When update blocked/direct sites is enabled, cow will update `~/.cow/auto-blocked` and `~/.cow/auto-direct`
- For sites which will be temporarily blocked, they should always go through cow and thus should not appear in blocked or direct site lists. Put them in `~/.cow/chou`. (If you are Chinese, this stands for 抽风.)
- Command line options can override options in configuration file. For more details, see the output of `cow -h`.
