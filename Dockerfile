FROM registry.gitlab.com/vitrifi/build-images/go:v1.18.4 as build-stage
ARG BINARY_VERSION="0.1.0"
ARG COMMIT_HASH="12345abcd"

WORKDIR /work

RUN apk add protoc
# dep caching
COPY go.mod go.sum ./
RUN go mod download -x
RUN 	go get -d google.golang.org/protobuf/cmd/protoc-gen-go@v1.28 && \
    	go get -d google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.2 && \
    	go get -d github.com/golangci/golangci-lint/cmd/golangci-lint@latest && \
    	go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.28 && \
    	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.2
# everything else
COPY . .
RUN cd proto; protoc --go_opt=M=gitlab.com --go_out=../model shar-workflow/models.proto
RUN ls model/gitlab.com/shar-workflow/shar/model && \
    mv model/gitlab.com/shar-workflow/shar/model/models.pb.go model && \
    rm -rf model/gitlab.com

RUN CGO_ENABLED=0 go build -ldflags "-X 'main.ServerVersion=$BINARY_VERSION' -X 'main.CommitHash=$COMMIT_HASH' -X 'main.BuildDate=$(date)'" -o build/server ./server/cmd/shar/main.go
RUN CGO_ENABLED=0 go build -ldflags "-X 'main.ServerVersion=$BINARY_VERSION' -X 'main.CommitHash=$COMMIT_HASH' -X 'main.BuildDate=$(date)'" -o build/telemetry ./telemetry/cmd/shar-telemetry/main.go

FROM registry.gitlab.com/vitrifi/build-images/scratch:v1.0.0 as production-stage-server
WORKDIR /app
COPY --from=build-stage /work/build/server .
ENTRYPOINT ["/app/server"]

FROM registry.gitlab.com/vitrifi/build-images/scratch:v1.0.0 as production-stage-telemetry
WORKDIR /app
COPY --from=build-stage /work/build/telemetry .
ENTRYPOINT ["/app/telemetry"]