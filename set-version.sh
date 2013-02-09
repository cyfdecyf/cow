#!/bin/bash

if [ $# != 1 ]; then
    echo "Usage: $0 <version>"
    exit 1
fi

version=$1
#echo $version

sed -i -e "s,\(\tversion \+= \)\".*\"$,\1\"$version\"," config.go
sed -i -e "s/version=.*$/version=$version/" install-cow.sh
sed -i -e "s/当前版本：.*\$/当前版本：$version/" README.md

