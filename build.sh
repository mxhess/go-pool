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

#cp -r ./docs ./build/
cp config.json ./build/

