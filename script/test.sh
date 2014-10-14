#!/bin/bash

cd "$( dirname "${BASH_SOURCE[0]}" )/.."

if ! go build; then
    echo "build failed"
    exit 1
fi

PROXY_ADDR=127.0.0.1:7788
MEOW_ADDR=127.0.0.1:8899

if [[ -z "$TRAVIS" ]]; then
    RCDIR=~/.meow/
else # on travis
    RCDIR=./script/
fi

./MEOW -request=false -reply=false -rc $RCDIR/debugrc -listen=meow://aes-128-cfb:foobar@$MEOW_ADDR &
parent_pid=$!
./MEOW -request=false -reply=false -rc ./script/httprc -listen=http://$PROXY_ADDR &
meow_pid=$!

stop_meow() {
    kill -SIGTERM $parent_pid
    kill -SIGTERM $meow_pid
}
trap 'stop_meow' TERM INT

sleep 1

test_get() {
    local url
    url=$1
    target=$2
    noproxy=$3
    code=$4

    echo -n "GET $url "
    if [[ -z $code ]]; then
        code="200"
    fi

    # -s silent to disable progress meter, but enable --show-error
    # -i to include http header
    # -L to follow redirect so we should always get HTTP 200
    if [[ -n $noproxy ]]; then
        cont=`curl -s --show-error -i -L $url 2>&1`
    else
        cont=`curl -s --show-error -i -L -x $PROXY_ADDR $url 2>&1`
    fi
    ok=`echo $cont | grep -E -o "HTTP/1\.1 +$code"`
    html=`echo $cont | grep -E -o -i "$target"`
    if [[ -z $ok || -z $html ]] ; then
        echo "=============================="
        echo "GET $url FAILED!!!"
        echo "$ok"
        echo "$html"
        echo $cont
        echo "=============================="
        kill -SIGTERM $meow_pid
        exit 1
    fi
    sleep 0.3
    echo "passed"
}

test_get $PROXY_ADDR/pac "proxy-autoconfig" "noproxy" # test for pac
test_get google.com "<html" # 301 redirect
test_get www.google.com "<html" # 302 redirect , chunked encoding
test_get www.reddit.com "<html" # chunked encoding
test_get openvpn.net "</html>" # blocked site, all kinds of block method
test_get https://google.com "<html" # test for HTTP connect
test_get https://www.google.com "<html"
test_get https://www.twitter.com "</html>"

# Sites that may timeout on travis.
if [[ -z $TRAVIS ]]; then
    test_get plan9.bell-labs.com/magic/man2html/1/2l "<head>" "" "404" # single LF in response header
    test_get www.wpxap.com "<html" # HTTP 1.0 server
    test_get youku.com "<html" # 302 redirect
    test_get douban.com "</html>" # 301 redirect
    test_get www.taobao.com "<html>" # chunked encoding, weird can't tests for </html> in script
    test_get https://www.alipay.com "<html>"
fi

stop_meow
exit 0
