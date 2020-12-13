#!/bin/bash

go build pingpong.go

sshpass -p 'MagillaGorilla89' scp pingpong abanfi@192.168.1.160:/home/abanfi/Documents/private/openwhisk/examples/.

