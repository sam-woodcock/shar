default: clean proto server

configure:
	@echo "\033[92mConfigure\033[0m"
	go version
	go get -d google.golang.org/protobuf/cmd/protoc-gen-go@v1.27.1
	go get -d google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.2
	go get -d github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.27.1
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.2
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

all: proto server

proto: .FORCE
	@echo "\033[92mBuild proto\033[0m"
	#protoc -Iproto --go-grpc_out=paths=source_relative:./goproto $(shell find proto -iname "*.proto");
	protoc -Iproto --go_out=paths=source_relative:./model $(shell find proto -iname "*.proto");
	protoc -Iproto --go-grpc_out=paths=source_relative:./model $(shell find proto -iname "*.proto");

server: .FORCE
	mkdir -p build/server
	cd server/cmd/shar; go build
	cp server/cmd/shar/shar build/server/
	cp server/Dockerfile build/server/

docker: clean proto server .FORCE
	cd build/server; docker build -t shar .
clean: .FORCE
	cd server/cmd/shar; go clean
	rm -f server/cmd/shar/shar
	rm -f model/*.pb.go

.FORCE:
