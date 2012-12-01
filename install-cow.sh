#!/bin/bash

cpu=`uname -m`
case $cpu in
    "x86_64")
        ;;
    *)
        echo "$cpu currently has no precompiled binary"
        ;;
esac

os=`uname -s`
case $os in
    "Darwin")
        binary="mac"
        ;;
    "Linux")
        binary="linux"
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

# Get installation directory from user
echo -n "Install cow binary to which directory (absolute path): "
read install_dir </dev/tty
if [ -z $install_dir ]; then
    echo "No installation directory given, assuming current directory"
    install_dir=`pwd`
fi
if [ ! -d $install_dir ]; then
    echo "Installation directory does not exists"
    exit 1
fi

# Ask OS X user whehter to start COW upon login
start_on_login="n"
if [ $os == "Darwin" ]; then
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
tmpbin=/tmp/cow
binary_base="https://github.com/downloads/cyfdecyf/cow"
binary="cow-$binary-$cpu"
echo "Downloading cow binary to $tmpbin"
curl -L "$binary_base/$binary" -o $tmpbin || \
    exit_on_fail "Downloading cow binary failed"
chmod +x $tmpbin ||
    exit_on_fail "Can't chmod for $tmpbin"

# Download sample config file if no configuration directory present
doc_base="https://raw.github.com/cyfdecyf/cow/master/doc"
config_dir="$HOME/.cow"
if [ ! -e $config_dir ]; then
    sample_config_base="${doc_base}/sample-config"
    echo "Downloading sample config file to $config_dir" 
    mkdir -p $config_dir || exit_on_fail "Can't create $config_dir directory"
    for f in rc blocked direct; do
        echo "Downloading $sample_config_base/$f to $config_dir/$f"
        curl -s -L "$sample_config_base/$f" -o $config_dir/$f || \
            exit_on_fail "Downloading sample config file $f failed"
    done
fi

# Download startup plist file
if [ $start_on_login == "y" ]; then
    la_dir="$HOME/Library/LaunchAgents"
    plist="info.chenyufei.cow.plist"
    mkdir -p $la_dir && exit_on_fail "Can't create directory $la_dir"
    echo "Downloading $doc_base/$plist to $la_dir/$plist"
    curl -s -L "$doc_base/$plist" | \
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

# Done
echo
echo "Installation finished."
echo "Please edit $config_dir/rc according to your own settings."
echo 'After that, execute "cow &" to start cow and run in background.'
