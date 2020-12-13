module action

go 1.15

replace nflib => ./nflib

replace handlers => ./handlers

replace utils => ./utils

replace natdb => ./natdb

require nflib v0.0.0-00010101000000-000000000000

require handlers v0.0.0-00010101000000-000000000000

require utils v0.0.0-00010101000000-000000000000

require (
	github.com/google/netstack v0.0.0-20191123085552-55fcc16cd0eb
	github.com/piaohao/godis v0.0.18
	natdb v0.0.0-00010101000000-000000000000
)
