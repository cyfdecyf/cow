#!/bin/bash

cd "$( dirname "${BASH_SOURCE[0]}" )/.."

PROXY_ADDR=127.0.0.1:8888

./cow -rc ~/.cow/debugrc -listen=$PROXY_ADDR &
cow_pid=$!
sleep 0.5

test_get() {
    local url
    url=$1

    # get 5 times
    for i in {1..2}; do
        if ! curl -L -s -x $PROXY_ADDR $SOCKS $url >/dev/null 2>&1; then
            echo "=============================="
            echo "GET $url FAILED!!!"
            echo "=============================="
            kill -SIGINT $server_pid
            kill -SIGINT $local_pid
            kill -SIGTERM $cow_pid
            exit 1
        fi
        sleep 0.5
    done
    echo "=============================="
    echo "GET $url passed"
    echo "=============================="
}

test_get baidu.com # baidu uses content-length
test_get youku.com # 302 redirect
test_get douban.com # 301 redirect
test_get www.taobao.com # chunked encoding
test_get https://www.twitter.com # builtin blocked site, 301 direct
test_get openvpn.net # blocked site, all kinds of block method

kill -SIGTERM $cow_pid
