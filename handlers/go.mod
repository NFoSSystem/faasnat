module handlers

go 1.15

replace natdb => ../natdb

replace utils => ../utils

replace nflib => ../nflib

require (
	github.com/google/btree v1.0.0 // indirect
	github.com/google/netstack v0.0.0-20191123085552-55fcc16cd0eb
	github.com/howeyc/crc16 v0.0.0-20171223171357-2b2a61e366a6
	github.com/willf/bitset v1.1.11
	natdb v0.0.0-00010101000000-000000000000
	nflib v0.0.0-00010101000000-000000000000
	utils v0.0.0-00010101000000-000000000000
)
