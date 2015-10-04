SRC=github.com/pires/metricas/...
PROTO_PKG=github.com/pires/metricas/api
PROTO_PATH=src/${PROTO_PKG}

GOPATH=$(shell pwd):$(shell pwd)/vendor

build:
	GOARCH=amd64 gb build all

all:	clean proto format build

clean:
	rm -rf ./{bin,pkg}
	find ${PROTO_PATH} -name *.pb.go -print0 | xargs -0 rm -f

format:
	gofmt -s -w src

proto:
	protoc --proto_path=${PROTO_PATH} --go_out=plugins=grpc:${PROTO_PATH} ${PROTO_PATH}/metricas.proto

release: clean proto format
	GOOS=linux GOARCH=amd64 gb build -ldflags '-w -extldflags=-static'

test:
	go test -v ${SRC}
