#!/bin/bash

version=`grep '^version=' ./install-cow.sh | sed -s 's/version=//'`
echo "creating cow binary version $version"

export CGO_ENABLED=0

mkdir -p bin
build() {
    local name
    local GOOS
    local GOARCH

    name=cow-$3-$version
    echo "building $name"
    GOOS=$1 GOARCH=$2 go build -a || exit 1
    if [[ $1 == "windows" ]]; then
        zip $name.zip cow.exe
        rm -f cow.exe
        mv $name.zip bin/
    else 
        mv cow bin/$name
        gzip -f bin/$name
    fi
}

build darwin amd64 mac64
build linux amd64 linux64
build linux 386 linux32
build windows amd64 win64
build windows 386 win32

