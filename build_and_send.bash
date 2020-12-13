#!/bin/bash

tmpDir=$(mktemp -d)
currDir=$(pwd)
cp -rp * $tmpDir/

cd $tmpDir
zip nat-src.zip -qr *

docker run -i openwhisk/action-golang-v1.15 -compile main <nat-src.zip >nat-bin.zip

#sshpass -p #password# scp nat-bin.zip #destination#

cd $currDir

if [[ ! -z $tmpDir && $tmpDir != "/" ]]; then
	rm -rf $tmpDir
fi
