#!/bin/bash

version=0.9.8

arch=`uname -m`
case $arch in
    "x86_64")
        arch="64"
        ;;
    "i386" | "i586" | "i486" | "i686")
        arch="32"
        ;;
    "armv5tel" | "armv6l" | "armv7l")
        features=`cat /proc/cpuinfo | grep Features`
        if [[ ! "$features" =~ "vfp" ]]; then
            #arm without vfp must use GOARM=5 binary
            #see https://github.com/golang/go/wiki/GoArm
            arch="-armv5tel"
        else
            arch="-$arch"
        fi
        ;;
    *)
        echo "$arch currently has no precompiled binary"
        ;;
esac

os=`uname -s`
case $os in
    "Darwin")
        os="mac"
        ;;
    "Linux")
        os="linux"
        ;;
    *)
        echo "$os currently has no precompiled binary"
        exit 1
esac

exit_on_fail() {
    if [ $? != 0 ]; then
        echo $1
        exit 1
    fi
}

while true; do
    # Get install directory from environment variable.
    if [[ -n $COW_INSTALLDIR && -d $COW_INSTALLDIR ]]; then
        install_dir=$COW_INSTALLDIR
        break
    fi

    # Get installation directory from user
    echo -n "Install cow binary to which directory (absolute path, defaults to current dir): "
    read install_dir </dev/tty
    if [ -z $install_dir ]; then
        echo "No installation directory given, assuming current directory"
        install_dir=`pwd`
        break
    fi
    if [ ! -d $install_dir ]; then
        echo "Directory $install_dir does not exists"
    else
        break
    fi
done

# Ask OS X user whehter to start COW upon login
start_on_login="n"
if [ $os == "mac" ]; then
    while true; do
        echo -n "Start COW upon login? (If yes, download a plist file to ~/Library/LaunchAgents) [Y/n] "
        read start_on_login </dev/tty
        case $start_on_login in
        "Y" | "y" | "")
            start_on_login="y"
            break
            ;;
        "N" | "n")
            start_on_login="n"
            break
            ;;
        esac
    done
fi

# Download COW binary
bin=cow-$os$arch-$version
tmpdir=`mktemp -d /tmp/cow.XXXXXX`
tmpbin=$tmpdir/cow
binary_url="https://github.com/cyfdecyf/cow/releases/download/$version/$bin.gz"
echo "Downloading cow binary $binary_url to $tmpbin.gz"
curl -L "$binary_url" -o $tmpbin.gz || \
    exit_on_fail "Downloading cow binary failed"
gunzip $tmpbin.gz || exit_on_fail "gunzip $tmpbin.gz failed"
chmod +x $tmpbin ||
    exit_on_fail "Can't chmod for $tmpbin"

# Download sample config file if no configuration directory present
doc_base="https://raw.github.com/cyfdecyf/cow/$version/doc"
config_dir="$HOME/.cow"
is_update=true
if [ ! -e $config_dir ]; then
    is_update=false
    sample_config_base="${doc_base}/sample-config"
    echo "Downloading sample config file to $config_dir"
    mkdir -p $config_dir || exit_on_fail "Can't create $config_dir directory"
    for f in rc; do
        echo "Downloading $sample_config_base/$f to $config_dir/$f"
        curl -L "$sample_config_base/$f" -o $config_dir/$f || \
            exit_on_fail "Downloading sample config file $f failed"
    done
fi

# Download startup plist file
if [ $start_on_login == "y" ]; then
    la_dir="$HOME/Library/LaunchAgents"
    plist="info.chenyufei.cow.plist"
    plist_url="$doc_base/osx/$plist"
    mkdir -p $la_dir && exit_on_fail "Can't create directory $la_dir"
    echo "Downloading $plist_url to $la_dir/$plist"
    curl -L "$plist_url" | \
        sed -e "s,COWBINARY,$install_dir/cow," > $la_dir/$plist || \
        exit_on_fail "Download startup plist file to $la_dir failed"
fi

# Move binary to install directory
echo "Move $tmpbin to $install_dir (will run sudo if no write permission to install directory)"
if [ -w $install_dir ]; then
    mv $tmpbin $install_dir
else
    sudo mv $tmpbin $install_dir
fi
exit_on_fail "Failed to move $tmpbin to $install_dir"
rmdir $tmpdir

# Done
echo
if $is_update; then
    echo "Update finished."
else
    echo "Installation finished."
    echo "Please edit $config_dir/rc according to your own settings."
    echo 'After that, execute "cow &" to start cow and run in background.'
fi

