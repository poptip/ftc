#!/bin/bash

set -e
# go test -c -race
go test -c
PKG=$(basename $(pwd))

while true ; do
        export GOMAXPROCS=$[ 1 + $[ RANDOM % 128 ]]
        GOTRACEBACK=2 ./$PKG.test $@ 2>&1
done
