#!/bin/bash

cd "$( dirname "${BASH_SOURCE[0]}" )/.."

version=`grep '^version=' ./install-cow.sh | sed -s 's/version=//'`
echo "creating cow binary version $version"

mkdir -p bin
build() {
    local name
    local goos
    local goarch
    local goarm
    local cgo

    goos="GOOS=$1"
    goarch="GOARCH=$2"
    if [[ $3 == "linux-armv5" ]]; then
        goarm="GOARM=5"
    fi

    if [[ $1 == "darwin" ]]; then
        # Enable CGO for OS X so change network location will not cause problem.
        cgo="CGO_ENABLED=1"
    else
        cgo="CGO_ENABLED=0"
    fi

    name=cow-$3-$version
    echo "building $name"
    eval $cgo $goos $goarch $goarm go build || exit 1
    if [[ $1 == "windows" ]]; then
        mv cow.exe script
        pushd script
        sed -e 's/$/\r/' ../doc/sample-config/rc > sample-rc.txt
        zip $name.zip cow.exe cow-taskbar.exe cow-hide.exe sample-rc.txt
        rm -f cow.exe sample-rc.txt
        mv $name.zip ../bin/
        popd
    else
        mv cow bin/$name
        gzip -f bin/$name
    fi
}

build darwin amd64 mac64
build darwin 386 mac32
build linux amd64 linux64
build linux 386 linux32
build linux arm linux-armv6
build linux arm linux-armv5
build windows amd64 win64
build windows 386 win32
