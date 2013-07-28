#!/bin/bash

if [[ $# != 1 ]]; then
    echo "Usage: $0 <log file>"
    exit 1
fi

log=$1

#clients=`egrep 'cli\([^)]+\) connected, total' $log | cut -d ' ' -f 4`

#for c in $clients; do
    #echo $c
#done

sort --stable --key 4,4 --key 3,3 $log | sed -e "/closed, total/s,\$,\n\n," > $log-grouped

