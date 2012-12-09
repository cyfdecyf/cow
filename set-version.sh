#!/bin/bash

if [ $# != 1 ]; then
    echo "Usage: $0 <version>"
    exit 1
fi

version=$1
#echo $version

sed -i -e "s/version = .*\$/version = \"$version\"/" config.go
sed -i -e "s/version=.*\$/version=$version/" install-cow.sh

