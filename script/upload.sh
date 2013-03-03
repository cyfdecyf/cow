#!/bin/bash

cd "$( dirname "${BASH_SOURCE[0]}" )/.."

if [[ $# != 2 ]]; then
    echo "upload.sh <username> <passwd>"
    exit 1
fi

version=`grep '^version=' ./install-cow.sh | sed -s 's/version=//'`
username=$1
passwd=$2

upload() {
    summary=$1
    file=$2
    googlecode_upload.py -l Featured -u "$username" -w "$passwd" -s "$summary" -p cow-proxy "$file"
}

upload "$version for Linux 32bit" bin/cow-linux32-$version.gz
upload "$version for Linux 64bit" bin/cow-linux64-$version.gz
upload "$version for Windows 64bit" bin/cow-win64-$version.zip
upload "$version for Windows 32bit" bin/cow-win32-$version.zip
upload "$version for OS X 64bit" bin/cow-mac64-$version.gz
