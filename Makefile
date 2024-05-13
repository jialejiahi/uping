
INSTALL_DIR=/data/share/tools
DIR=$(shell pwd)

current_time := $(shell date +%Y-%m-%d_%H:%M:%S)

all:
	go mod tidy
	CGO_ENABLED=0 go build -ldflags '-X main.buildtime=${current_time}' -o uping
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags '-X main.buildtime=${current_time}' -o upingarm

# aarch64 compile
arm:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags '-X main.buildtime=${current_time}' -o upingarm

install: all
	tar -zcvf uping.tar.gz uping upingarm
	mkdir -p ${INSTALL_DIR}
	cp -f ${DIR}/uping ${INSTALL_DIR}
	cp -f ${DIR}/uping.tar.gz ${INSTALL_DIR}

clean:
	rm -f ${DIR}/uping
	rm -f ${DIR}/upingarm
	rm -f ${DIR}/uping.tar.gz
