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
    GOOS=$1 GOARCH=$2 go build -a -ldflags "-s" || exit 1
    if [[ $1 == "windows" ]]; then
        mv cow.exe bin/$name.exe
        zip bin/$name.zip bin/$name.exe
    else 
        mv cow bin/$name
        gzip bin/$name
    fi
}

build linux amd64 linux64
build linux 386 linux32
build darwin amd64 mac64

