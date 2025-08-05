
set -e

rm -r build
mkdir build

cd cmd/master/
env CGO_ENABLED=0 go build -trimpath
cp ./master ../../build


cd ../slave/
env CGO_ENABLED=0 go build -trimpath
cp ./slave ../../build/

cd ../..

go build -o ./build/db-inspect ./tools/db-inspect.go
go build -o ./build/db-reset-balances ./tools/db-reset-balances.go  
go build -o ./build/db-set-balance ./tools/db-set-balance.go
go build -o ./build/debug_deserialize ./tools/debug_deserialize.go
go build -o ./build/debug_withdraw_cursor ./tools/debug_withdraw_cursor.go
go build -o ./build/debug_config ./tools/debug_config.go
go build -o ./build/db-clear-pending ./tools/db-clear-pending.go

#cp -r ./docs ./build/
cp config.json ./build/

cd build
tar cvfz ~/pool.tgz .

