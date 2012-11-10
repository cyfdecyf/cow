# COW (Climb Over the Wall) proxy  #

COW is a proxy that tries to **automatically identify blocked web sites and use a parent proxy when visiting those sites**.

# Features #

- **Automatically identify blocked websites**
- **Record which sites are blocked, which can be directly accessed**
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
    # Nubmer of cores to use
    core = 2
    # parent socks proxy address
    socks = 127.0.0.1:1080
    # ssh to the given server to start socks proxy
    ssh_server = gfw

To start cow, just execute `cow` on the command line.

- The PAC file can be access at the same address as proxy listen address
  - For the above example, accessing `http://127.0.0.1:7777/anypath` will get the generated PAC file

- Blocked and direct accessable sites list are stored in `~/.cow/blocked` and `~/.cow/direct`
  - One line for each domain
  - These 2 files will be updated when cow exits

- For sites which will be temporarily blocked, they should always go through cow. Put them in `~/.cow/chou`. (If you are Chinese, this stands for 抽风.)

- Command line options can override options in configuration file. For more details, see the output of `cow -h`
