### 自定义RDMA Exporter指标采集器步骤
1. 连接K8S相关配置
2. 读取本地目录
3. 解析指标信息
4. 配置Service、ServiceMonitor
5. Prometheus Operator拉取RDMA Exporter指标信息
6. Prometheus 拉取Prometheus Operator中的指标信息
7. 通过Grafna进行数据展示

Prometheus Operator整体流程图
```shell
Prometheus Operator 部署在监控命名空间（如 `monitoring`）
  │
  │ 发现 ServiceMonitor 资源
  ▼
ServiceMonitor 配置指向 `monitor` 命名空间的 Service
  │
  │ 通过标签匹配 Service `vst-metrics-hami-exporter`
  ▼
Prometheus 抓取 Service 后端 Pod 的指标（`:9808/metrics`）
```
### Docker相关命令
#### 构建镜像
docker build -t hub.innerstar.com/vesta/vst-metrics-hami-exporter:v0.1.1 .

docker build -t harbor.bjxsj.k8s.yxit.cc/vst-metrics-hami-exporter:v0.2.0 .
#### 登录镜像仓库（如果是私有仓库）
docker login your-registry

#### 推送镜像
docker push hub.innerstar.com/vesta/vst-metrics-hami-exporter:v0.1.1

#### Docker执行服务
docker run -it --rm \
harbor.bjxsj.k8s.yxit.cc/vst-metrics-hami-exporter:v0.2.0 sh

### Makefile相关命令

### Prometheus 增加配置信息

#### Service配置
```shell
# service需要加入无头配置
clusterIP: None
type: ClusterIP
```
#### ServiceMonitor 配置
ServiceMonitor 是 Prometheus Operator 提供的自定义资源，用于配置 Prometheus 如何自动发现并抓取 Kubernetes 集群中特定服务的指标数据。

#### Scrape config 配置
在 Prometheus 中添加如下 scrape config，以抓取每个节点的指标：
```yaml
- job_name: prometheus
  static_configs:
    - targets:
        - localhost:9090
- job_name: fedrate-k8s-vst-metrics-hami-exporter
  metrics_path: /federate
  honor_labels: true
  static_configs:
    - targets:
        - prom-k8s.innerstar.com
      labels:
        fedrate_source: prom-k8s.innerstar.com
  params:
    match[]:
      - '{job="vst-metrics-hami-exporter"}'
  honor_timestamps: true
  scrape_interval: 15s
  scrape_timeout: 14s
```

### 执行命令相关
1. windows环境构建二进制文件命令
```shell
$env:GOOS="linux"; $env:GOARCH="amd64"; CGO_ENABLED=0 go build -o vst-metrics-hami-exporter ./cmd/main.go
# 还原环境变量
$env:GOOS="windows";
```

2. linux环境编译二进制并且构建镜像
```shell
make build-linux
make build-image
docker push harbor.prod.yxit.cc/vesta/vst-metrics-hami-exporter:$(VERSION)
```

### 版本历史
```azure
v0.1.4 解决pod内nvml-monitor启动不成功时，配额无法正确上报的问题
v0.1.5 支持单pod多gpu上报
```