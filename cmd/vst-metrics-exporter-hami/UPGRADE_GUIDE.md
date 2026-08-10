# vst-metrics-hami-exporter-hami 升级指南 (v0.1.0 → v0.2.0)

## 📋 改造概述

本次升级将 **vst-metrics-hami-exporter-hami** 从基于 **pod exec + JSON文件** 的采集方式，升级为与 **HAMi vGPUmonitor 相同的底层采集方式（共享内存 + NVML）**。

### 核心变化

| 维度 | 旧版本 (v0.1.0) | 新版本 (v0.2.0) |
|------|----------------|----------------|
| **数据采集方式** | exec进入pod执行脚本 | 直接读取libvgpu.so共享内存 |
| **数据来源** | Pod内业务层JSON文件 | NVML硬件级数据 |
| **性能影响** | 高（每个pod都要exec） | 低（单次内存映射） |
| **依赖组件** | Docker client, K8s exec | HAMi monitor/nvidia包 |
| **部署要求** | 需挂载metrics目录 | 需挂载HOOK_PATH目录 |

---

## 🔧 技术实现细节

### 1. 数据采集机制

#### 旧方案（已移除）
```
Pod → exec执行keep_monitor_alive.sh → 写入JSON → 宿主机读取
```

#### 新方案（当前）
```
libvgpu.so → 共享内存(.cache文件) → syscall.Mmap映射 → UsageInfo接口 → 指标转换
```

关键代码位置：
- **ContainerLister初始化**: [vgpu_collector.go:261](collectors/vgpu_collector.go#L261)
- **共享内存读取**: `pkg/monitor/nvidia/cudevshr.go` (HAMi包)
- **指标提取**: [vgpu_collector.go:143-223](collectors/vgpu_collector.go#L143-L223)

### 2. 指标映射关系

| HAMi原始指标 (从共享内存) | vst输出指标 | 转换逻辑 |
|--------------------------|------------|---------|
| **`DeviceMemoryTotal(i)`** | **`vst_vCudaGpu_memory_usage`** | **直接使用原始值 (bytes)** |
| `DeviceMemoryLimit(i)` | `vst_vCudaGpu_memory_limit` | 原值(bytes) |
| **`DeviceSmUtil(i)`** | **`vst_vCudaGpu_vgpu_cores_usage`** | **直接使用原始值 (百分比, 0-100)** |
| **固定值100** | **`vst_vCudaGpu_vgpu_cores_limit`** | **整数, 单卡100%算力** |

### 3. 数据结构

```go
// 共享内存中的UsageInfo接口 (HAMi定义)
type UsageInfo interface {
    DeviceNum() int
    DeviceMemoryTotal(idx int) uint64      // 显存使用量
    DeviceMemoryLimit(idx int) uint64      // 显存限制
    DeviceSmUtil(idx int) uint64           // SM利用率(0-100)
    DeviceUUID(idx int) string             // 设备UUID
    // ... 其他方法
}

// 输出的容器级指标缓存
type containerMetrics struct {
    namespace     string
    podName       string
    deviceUUID    string
    memoryUsed    uint64
    memoryLimit   uint64
    smUtilization uint64
}
```

---

## 🚀 部署指南

### 前置条件

1. **HAMi设备插件已部署**：确保节点上运行着HAMi的device-plugin
2. **libvgpu.so已安装**：确保 `/usr/local/vgpu/containers` 目录存在
3. **K8s集群访问权限**：需要能读取pod列表的RBAC权限

### 环境变量说明

| 变量名 | 必需 | 说明 | 示例值 |
|--------|------|------|--------|
| `MY_NODE_NAME` | ✅ | 当前节点名称 | 通过fieldRef自动注入 |
| `NODE_NAME` | ✅ | HAMi所需节点名（兼容） | 同上 |
| `HOOK_PATH` | ✅ | libvgpu共享内存路径 | `/usr/local/vgpu/containers` |
| `KUBECONFIG` | ✅ | K8s配置文件路径 | `/etc/kubernetes/kubeconfig` |

### 部署步骤

#### 1. 创建kubeconfig ConfigMap

```bash
# 从集群获取serviceaccount token
kubectl -n monitor create sa vst-metrics-hami-exporter

# 创建ClusterRoleBinding（如果还没有）
kubectl create clusterrolebinding vst-metrics-hami-exporter-binding \
  --clusterrole=view \
  --serviceaccount=monitor:vst-metrics-hami-exporter

# 创建ConfigMap（包含kubeconfig）
kubectl -n monitor create configmap vst-metrics-hami-exporter-kubeconfig \
  --from-literal=kubeconfig=$(cat <<EOF
apiVersion: v1
kind: Config
clusters:
- cluster:
    certificate-authority-data: $(kubectl config view --raw -o json | jq -r '.clusters[0].cluster."certificate-authority-data"')
    server: https://your-k8s-api-server:6443
  name: k8s
contexts:
- context:
    cluster: k8s
    user: vst-metrics-hami-exporter
  name: default
current-context: default
users:
- name: vst-metrics-hami-exporter
  user:
    token: <service-account-token>
EOF
)
```

#### 2. 应用DaemonSet

```bash
kubectl apply -f deployment/daemonset.yaml
```

#### 3. 验证部署

```bash
# 检查Pod状态
kubectl -n monitor get pods -l app=vst-metrics-hami-exporter -o wide

# 查看日志
kubectl -n monitor logs -f <pod-name> | head -50

# 测试指标端点
kubectl -n monitor port-forward svc/vst-metrics-hami-exporter 9808:9808
curl http://localhost:9808/metrics | grep "vst_vCudaGpu"
```

---

## 📊 监控指标示例

成功部署后，Prometheus将抓取到以下格式指标（与v0.1.0完全一致）：

```
# 显存使用量 (bytes)
vst_vCudaGpu_memory_usage{node="worker-01",pod="training-job",namespace="ml-team",gpu_device="GPU-abc123"} 6442450944

# 显存限制 (bytes)
vst_vCudaGpu_memory_limit{node="worker-01",pod="training-job",namespace="ml-team",gpu_device="GPU-abc123"} 8589934592

# 算力使用率 (百分比，0-100范围)
vst_vCudaGpu_vgpu_cores_usage{node="worker-01",pod="training-job",namespace="ml-team",gpu_device="GPU-abc123"} 85

# 算力限制 (整数，1-100，针对单卡)
vst_vCudaGpu_vgpu_cores_limit{node="worker-01",pod="training-job",namespace="ml-team",gpu_device="GPU-abc123"} 100
```

### 📌 指标说明

| 指标名称 | 类型 | 范围 | 说明 |
|---------|------|------|------|
| **`vst_vCudaGpu_memory_usage`** | Gauge | **bytes** | **显存实际使用量（字节数）** |
| `vst_vCudaGpu_memory_limit` | Gauge | bytes | 显存分配上限（bytes） |
| **`vst_vCudaGpu_vgpu_cores_usage`** | Gauge | **0-100 (百分比)** | **GPU算力实际使用率（原始值）** |
| **`vst_vCudaGpu_vgpu_cores_limit`** | Gauge | **1-100 (整数)** | **单卡算力分配上限，固定值100** |

---

## ⚠️ 注意事项

### 兼容性

- **HAMi版本**: 需要 HAMi ≥ v2.7.0 (支持共享内存机制)
- **GPU驱动**: NVIDIA驱动 ≥ 525.x (支持NVML完整功能)
- **K8s版本**: 支持 Kubernetes 1.24+

### 已知限制

1. **首次启动延迟**: 启动后需要等待10秒才能获取首批指标
2. **MIG设备**: 当前版本暂不单独处理MIG设备的细分指标（后续可扩展）
3. **多容器场景**: 同一pod内多个容器的指标会分别上报

### 故障排查

| 问题现象 | 可能原因 | 解决方案 |
|---------|---------|---------|
| 日志显示 "HOOK_PATH not set" | 环境变量未正确传递 | 检查DaemonSet的env配置 |
| 日志显示 "no cached metrics" | 共享内存目录为空或无权限 | 确认libvgpu.so正常运行且路径正确 |
| 指标值为0 | 容器未实际使用GPU | 正常现象，等待工作负载运行 |
| K8s API调用失败 | kubeconfig配置错误 | 验证ConfigMap和RBAC权限 |

---

## 🔍 与vGPUmonitor对比

### 优势

✅ **非侵入式**: 无需exec进入pod，不影响业务容器
✅ **低延迟**: 内存直接映射，无需进程间通信
✅ **高精度**: 获取NVML硬件级真实数据
✅ **轻量化**: 移除了Docker client等重依赖

### 差异点

| 特性 | vGPUmonitor | vst-metrics-hami-exporter-hami |
|------|-------------|--------------------------|
| 指标名称前缀 | `hami_*` | `vst_*` |
| 主机级指标 | ✅ 包含 | ❌ 仅容器级 |
| Legacy模式 | ✅ 支持 | ❌ 不支持 |
| Feedback机制 | ✅ 有调度反馈 | ❌ 纯监控无反馈 |
| MIG信息 | ✅ 详细 | ⚠️ 基础支持 |

---

## 🔄 回滚方案

如遇问题可快速回滚到旧版本：

```bash
# 使用旧的镜像和配置
kubectl -n monitor set image daemonset/vst-metrics-hami-exporter \
  vst-metrics-hami-exporter=hub.innerstar.com/vesta.nvidia/vst-metrics-hami-exporter:v0.1.0

# 或应用旧的yaml文件
kubectl apply -f deployment/daemonset-v1.yaml
```

---

## 📝 更新日志

### v0.2.0 (2026-07-28)

**Breaking Changes**
- ❌ 移除Docker client依赖
- ❌ 移除pod exec采集逻辑
- ❌ 移除JSON文件读取逻辑
- ❌ 移除ants线程池依赖

**New Features**
- ✅ 采用HAMi ContainerLister从共享内存采集
- ✅ 支持NVML硬件级精准数据
- ✅ 保持vst指标体系不变
- ✅ 降低资源占用(CPU/Memory)

**Dependencies**
- 新增: `github.com/Project-HAMi/HAMi` (本地replace)
- 新增: `github.com/NVIDIA/go-nvml/pkg/nvml`
- 移除: `github.com/docker/docker`
- 移除: `github.com/dustin/go-humanize`
- 移除: `github.com/panjf2000/ants/v2`
- 移除: `github.com/spf13/viper`

---

## 🤝 贡献指南

如需扩展功能，建议：

1. **添加主机级指标**: 在 `collectMetrics()` 中增加NVML主机查询
2. **支持MIG设备**: 参考 `metrics.go` 中的MIG处理逻辑
3. **自定义标签**: 修改 `containerMetrics` 结构体和 `emitMetric()` 调用
4. **调整采集频率**: 修改 `metricsCollectInterval` 常量

---

**维护团队**: GPU虚拟化平台组
**最后更新**: 2026-07-28
**适用环境**: 生产环境 (Production Ready)