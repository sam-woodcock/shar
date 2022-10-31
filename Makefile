default: clean proto server tracing

configure:
	@echo "\033[92mConfigure\033[0m"
	go version
	go get -d google.golang.org/protobuf/cmd/protoc-gen-go@v1.28.1
	go get -d google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.2
	go get -d github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.28.1
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.2
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

all: proto server tracing

proto: .FORCE
	@echo "\033[92mBuild proto\033[0m"
	cd proto; protoc --experimental_allow_proto3_optional --go_opt=M=gitlab.com --go_out=../model shar-workflow/models.proto;
	mv model/gitlab.com/shar-workflow/shar/model/models.pb.go model/
	rm -rf model/gitlab.com
server: .FORCE
	mkdir -p build/server
	cd server/cmd/shar; go build
	cp server/cmd/shar/shar build/server/
	cp server/cmd/shar/shar server/
	cp server/Dockerfile build/server/
tracing: .FORCE
	mkdir -p build/telemetry
	cd telemetry/cmd/shar-telemetry; go build
	cp telemetry/cmd/shar-telemetry/shar-telemetry build/telemetry/
	cp telemetry/cmd/shar-telemetry/shar-telemetry server/
	cp telemetry/Dockerfile build/telemetry/

docker: clean proto server tracing .FORCE
	cd build/server; docker build -t shar .
	cd build/telemetry; docker build -t shar-telemetry .
clean: .FORCE
	cd server/cmd/shar; go clean
	rm -f server/cmd/shar/shar
	cd telemetry/cmd/shar-telemetry; go clean
	rm -f telemetry/cmd/shar-telemetry/shar-telemetry
	rm -f model/*.pb.go
test: proto server tracing .FORCE
	go test --short --race ./...
	golangci-lint cache clean
	golangci-lint run -v -E gosec -E ireturn --timeout 5m0s
.FORCE:
