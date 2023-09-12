
INSTALL_DIR=/data/share/tools
DIR=$(shell pwd)

all:
	go mod tidy
	go build -ldflags '-extldflags "-fno-PIC -static"' -o utping
	GOOS=linux GOARCH=arm64 go build -ldflags '-extldflags "-fno-PIC -static"' -o utpingarm

# aarch64 compile
arm:
	GOOS=linux GOARCH=arm64 go build -ldflags '-extldflags "-fno-PIC -static"' -o utpingarm

install: all
	tar -zcvf utping.tar.gz utping utpingarm
	mkdir -p ${INSTALL_DIR}
	cp -f ${DIR}/utping ${INSTALL_DIR}
	cp -f ${DIR}/utping.tar.gz ${INSTALL_DIR}

clean:
	rm -f ${DIR}/utping
	rm -f ${DIR}/utpingarm
	rm -f ${DIR}/utping.tar.gz
