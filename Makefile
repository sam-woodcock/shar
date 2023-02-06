default: clean proto server tracing cli zen-shar

configure:
	@echo "\033[92mConfigure\033[0m"
	go version
	go get -d google.golang.org/protobuf/cmd/protoc-gen-go@v1.28.1
	go get -d google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.2
	go get -d github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.28.1
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.2
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

all: proto server tracing cli zen-shar

proto: .FORCE
	@echo "\033[92mBuild proto\033[0m"
	cd proto; protoc --experimental_allow_proto3_optional --go_opt=M=gitlab.com --go_out=../model shar-workflow/models.proto;
	@echo "\033[92mCopy model\033[0m"
	mv model/gitlab.com/shar-workflow/shar/model/models.pb.go model/
	@echo "\033[92mRemove proto working directories\033[0m"
	rm -rf model/gitlab.com
server: .FORCE proto
	@echo "\033[92mCopying files\033[0m"
	mkdir -p build/server
	cd server/cmd/shar; go build
	cp server/cmd/shar/shar build/server/
	cp server/cmd/shar/shar server/
	cp server/Dockerfile build/server/
tracing: .FORCE
	mkdir -p build/telemetry
	@echo "\033[92mBuilding Telemetry\033[0m"
	cd telemetry/cmd/shar-telemetry; go build
	cp telemetry/cmd/shar-telemetry/shar-telemetry build/telemetry/
	cp telemetry/cmd/shar-telemetry/shar-telemetry server/
	cp telemetry/Dockerfile build/telemetry/
cli: .FORCE proto
	mkdir -p build/cli
	@echo "\033[92mBuilding CLI\033[0m"
	cd cli/cmd/shar; go build
	cp cli/cmd/shar/shar build/cli/
zen-shar: .FORCE proto
	mkdir -p build/zen-shar
	@echo "\033[92mBuilding Zen\033[0m"
	cd zen-shar/cmd/zen-shar; go build
	cp zen-shar/cmd/zen-shar/zen-shar build/zen-shar/
docker: clean proto server tracing .FORCE
	cd build/server; docker build -t shar .
	cd build/telemetry; docker build -t shar-telemetry .
clean: .FORCE
	@echo "\033[92mCleaning build directory\033[0m"
	cd server/cmd/shar; go clean
	rm -f server/cmd/shar/shar
	cd telemetry/cmd/shar-telemetry; go clean
	rm -f telemetry/cmd/shar-telemetry/shar-telemetry
	rm -f model/*.pb.go
test: proto server tracing .FORCE
	@echo "\033[92mCleaning test cache\033[0m"
	go clean -testcache
	@echo "\033[92mRunning tests\033[0m"
	go test ./...
	golangci-lint cache clean
	@echo "\033[92mLinting\033[0m"
	golangci-lint run -v -E gosec -E revive -E ireturn --timeout 5m0s
race: proto server tracing .FORCE
	@echo "\033[92mCleaning test cache\033[0m"
	go clean -testcache
	@echo "\033[92mRunning tests\033[0m"
	go test ./... --race
.FORCE:
