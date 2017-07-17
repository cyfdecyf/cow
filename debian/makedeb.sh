#!/bin/sh

version=0.9.8
os="linux"

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
        exit 1
        ;;
esac

BASEDIR=$(dirname "$0")
tmpdir=`mktemp -d /tmp/cow.XXXXXX`
configdir=$tmpdir/usr/share/doc/cow-proxy
bindir=$tmpdir/usr/bin/
mkdir -m 755 -p $bindir
cp -R $BASEDIR/lib $tmpdir
cp -R $BASEDIR/DEBIAN $tmpdir
tmpbin=$bindir/cow-proxy

clean_tmp()
{
    echo "Cleaning the directories like /tmp/cow.*"
    cmd="rm -rf /tmp/cow.*"
    echo ${cmd}
    ${cmd}
}

exit_on_fail() {
    if [ $? != 0 ]; then
        echo $1
        clean_tmp
        exit 1
    fi
}

__download()
{
    curl -L $1 -o $2 || \exit_on_fail $3
    #wget $1 -O $2 || \exit_on_fail $3
}

download_bin()
{
    bin=cow-$os$arch-$version
    binary_url="https://github.com/cyfdecyf/cow/releases/download/$version/$bin.gz"

    echo "Downloading cow binary $binary_url to $tmpbin.gz"
    __download "$binary_url" $tmpbin.gz "Downloading cow binary failed"
    gunzip $tmpbin.gz || exit_on_fail "gunzip $tmpbin.gz failed"
    chmod +x $tmpbin ||
        exit_on_fail "Can't chmod for $tmpbin"
}

download_config()
{
    doc_base="https://raw.github.com/cyfdecyf/cow/$version/doc"
    is_update=true
    if [ ! -e $configdir ]; then
        is_update=false
        sample_config_base="${doc_base}/sample-config"
        echo "Downloading sample config file to $configdir"
        mkdir -m 755 -p $configdir || exit_on_fail "Can't create $configdir directory"
        for f in rc; do
            echo "Downloading $sample_config_base/$f to $configdir/$f"
            __download "$sample_config_base/$f" $configdir/$f "Downloading sample config file $f failed"
        done
    fi
}

make_deb()
{
    size=`du $tmpdir/lib $tmpdir/usr -ca |tail -1 | awk '{print $1}'`
    arch=`dpkg --print-architecture`
    sed -i "s/__version__/$version/" $tmpdir/DEBIAN/control
    sed -i "s/__arch__/$arch/" $tmpdir/DEBIAN/control
    sed -i "s/__size__/$size/" $tmpdir/DEBIAN/control
    chmod -R 755 $tmpdir/
    chmod 644 $tmpdir/lib/systemd/system/*
    chmod 644 $tmpdir/usr/share/doc/cow-proxy/*
    dpkg -b $tmpdir /tmp/ || \
        exit_on_fail "making deb failed."
}

download_bin
download_config
make_deb
clean_tmp

echo ""
echo "cow-proxy_${arch}_${version}.deb is made."
echo "Please run the following command to install it."
echo "    sudo dpkg -i /tmp/cow-proxy_${arch}_${version}.deb'"
