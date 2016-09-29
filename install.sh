#!/bin/bash

version=1.5

arch=`uname -m`
case $arch in
    "x86_64")
        arch="amd64"
        ;;
    "i386" | "i586" | "i486" | "i686")
        arch="386"
        ;;
    "armv5tel" | "armv6l" | "armv7l")
        arch="arm"
        ;;
    *)
        echo "$arch currently has no precompiled binary"
        ;;
esac

os=`uname -s`
case $os in
    "Darwin")
        os="darwin"
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
    if [[ -n $MEOW_INSTALLDIR && -d $MEOW_INSTALLDIR ]]; then
        install_dir=$MEOW_INSTALLDIR
        break
    fi

    # Get installation directory from user
    echo -n "Install MEOW binary to which directory (absolute path, defaults to current dir): "
    read install_dir </dev/tty
    if [ -z $install_dir ]; then
        echo "No installation directory given, assuming current directory"
        install_dir=`pwd`
        break
    fi
    if [ ! -d "$install_dir" ]; then
        echo "Directory $install_dir does not exists"
    else
        break
    fi
done

# Ask OS X user whehter to start MEOW upon login
start_on_login="n"
if [ $os == "darwin" ]; then
    while true; do
        echo -n "Start MEOW upon login? (If yes, download a plist file to ~/Library/LaunchAgents) [Y/n] "
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

# Download MEOW binary
bin=MEOW-$os-$arch-$version
tmpdir=`mktemp -d /tmp/MEOW.XXXXXX`
tmpbin=$tmpdir/MEOW
binary_url="https://github.com/renzhn/MEOW/raw/gh-pages/dist/$bin.gz"
echo "Downloading MEOW binary $binary_url to $tmpbin.gz"
curl -L "$binary_url" -o $tmpbin.gz || \
    exit_on_fail "Downloading MEOW binary failed"
gunzip $tmpbin.gz || exit_on_fail "gunzip $tmpbin.gz failed"
chmod +x $tmpbin ||
    exit_on_fail "Can't chmod for $tmpbin"

# Download sample config file if no configuration directory present
doc_base="https://raw.github.com/renzhn/MEOW/master/doc"
config_dir="$HOME/.meow"
is_update=true
if [ ! -e $config_dir ]; then
    is_update=false
    sample_config_base="${doc_base}/sample-config"
    echo "Downloading sample config file to $config_dir"
    mkdir -p $config_dir || exit_on_fail "Can't create $config_dir directory"
    for f in rc rc-full direct proxy reject; do
        echo "Downloading $sample_config_base/$f to $config_dir/$f"
        curl -L "$sample_config_base/$f" -o $config_dir/$f || \
            exit_on_fail "Downloading sample config file $f failed"
    done
fi

# Download startup plist file
if [ $start_on_login == "y" ]; then
    la_dir="$HOME/Library/LaunchAgents"
    plist="net.ohrz.meow.plist"
    plist_url="$doc_base/osx/$plist"
    mkdir -p $la_dir && exit_on_fail "Can't create directory $la_dir"
    echo "Downloading $plist_url to $la_dir/$plist"
    curl -L "$plist_url" | \
        sed -e "s,MEOWBINARY,$install_dir/MEOW," > $la_dir/$plist || \
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
    echo 'After that, execute "MEOW &" to start MEOW and run in background.'
fi

