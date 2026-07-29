
VERSION?=v0.1.5
BIN_NAME=vst-metrics-hami-exporter
IMAGE?=harbor.bjxsj.k8s.yxit.cc/vesta/$(BIN_NAME):$(VERSION)

# Golang 源文件目录和编译输出目录
SRC_DIR=./cmd/main.go
BUILD_DIR=build

build:
	docker build -t $(IMAGE) .

# 生产镜像仓库
build-image:
	docker build -t harbor.prod.yxit.cc/vesta/$(BIN_NAME):$(VERSION) .

# 在 Windows 系统中交叉编译为 Linux amd64 二进制
build-windows:
	@echo "==> Building in windows for linux/amd64"
	set GOOS=linux && set GOARCH=amd64 && go build -o $(BUILD_DIR)/$(BIN_NAME) $(SRC_DIR)
    # $env:GOOS="linux"; $env:GOARCH="amd64"; go build -o vst-metrics-hami-exporter ./cmd/main.go
    # $env:GOOS="windows";


build-linux:
	@echo "==> Building in linux/amd64"
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o $(BUILD_DIR)/$(BIN_NAME) $(SRC_DIR)

push:
	docker push $(IMAGE)

deploy:
	kubectl apply -f daemonset.yaml

# 清理构建输出
clean:
	rm -rf $(BUILD_DIR)