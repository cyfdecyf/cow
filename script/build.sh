#!/bin/bash

cd "$( dirname "${BASH_SOURCE[0]}" )/.."

version=`grep '^version=' ./install.sh | sed -s 's/version=//'`
echo "creating MEOW binary version $version"

mkdir -p bin/windows

gox -output="bin/{{.Dir}}-{{.OS}}-{{.Arch}}-$version" -os="darwin linux windows"

pack() {
    local goos
    local goarch
    local name

    goos=$1
    goarch=$2
    name=MEOW-$goos-$goarch-$version

    echo "packing $goos $goarch"
    if [[ $1 == "windows" ]]; then
        mv bin/$name.exe script/proxy.exe
        pushd script
        sed -e 's/$/\r/' ../doc/sample-config/rc > rc.txt
        sed -e 's/$/\r/' ../doc/sample-config/rc-full > rc-full.txt
        sed -e 's/$/\r/' ../doc/sample-config/direct > direct.txt
        mv meow-taskbar.exe MEOW.exe
        zip $name.zip proxy.exe MEOW.exe rc.txt rc-full.txt direct.txt
        rm -f proxy.exe rc.txt rc-full.txt direct.txt
        mv $name.zip ../bin/
        mv MEOW.exe meow-taskbar.exe
        popd
        if [[ $2 == "386" ]]; then
            mv bin/$name.zip bin/windows/MEOW-x86-$version.zip
        fi
        if [[ $2 == "amd64" ]]; then
            mv bin/$name.zip bin/windows/MEOW-x64-$version.zip
        fi
    else
        gzip -f bin/$name
    fi
}

pack darwin amd64
pack darwin 386
pack linux amd64
pack linux 386
pack linux arm
pack windows amd64
pack windows 386
