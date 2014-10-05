#!/bin/bash

cd "$( dirname "${BASH_SOURCE[0]}" )/.."

version=`grep '^version=' ./install-meow.sh | sed -s 's/version=//'`
echo "creating meow binary version $version"

mkdir -p bin
build() {
    local name
    local goos
    local goarch
    local goarm
    local cgo
    local armv

    goos="GOOS=$1"
    goarch="GOARCH=$2"
    arch=$3
    if [[ $2 == "arm" ]]; then
        armv=`echo $arch | grep -o [0-9]`
        goarm="GOARM=$armv"
    fi

    if [[ $1 == "darwin" ]]; then
        # Enable CGO for OS X so change network location will not cause problem.
        cgo="CGO_ENABLED=1"
    else
        cgo="CGO_ENABLED=0"
    fi

    name=meow-$arch-$version
    echo "building $name"
    echo $cgo $goos $goarch $goarm go build
    eval $cgo $goos $goarch $goarm go build || exit 1
    if [[ $1 == "windows" ]]; then
        mv meow.exe script
        pushd script
        sed -e 's/$/\r/' ../doc/sample-config/rc > rc.txt
        zip $name.zip meow.exe meow-taskbar.exe meow-hide.exe rc.txt
        rm -f meow.exe rc.txt
        mv $name.zip ../bin/
        popd
    else
        mv meow bin/$name
        gzip -f bin/$name
    fi
}

build darwin amd64 mac64
build darwin 386 mac32
build linux amd64 linux64
build linux 386 linux32
build linux arm linux-armv5tel
build linux arm linux-armv6l
build linux arm linux-armv7l
build windows amd64 win64
build windows 386 win32
