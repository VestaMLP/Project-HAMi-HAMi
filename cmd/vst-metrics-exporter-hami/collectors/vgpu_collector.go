package collectors

import (
	"os"
	"reflect"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	listerscorev1 "k8s.io/client-go/listers/core/v1"

	"github.com/Project-HAMi/HAMi/pkg/monitor/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"

	"vst-metrics-hami-exporter/logger"
)

// 本文件用于将HAMi vGPUmonitor的指标转换为vst格式上报
// 采集方式：与vGPUmonitor相同，直接从共享内存+NVML读取硬件级数据
// 输出格式：保持vst体系的指标名称和标签

const (
	metricsCollectInterval = 10 * time.Second // 采集间隔
)

var (
	vGpuMetricsCollectorName = "vgpu-metrics"
)

// VGpuInfo 需要上报的GPU使用数据结构（保持原有定义以兼容下游系统）
type VGpuInfo struct {
	MemoryUsage    float64 `json:"memory_usage" help:"显存使用"`      // 显存使用量（bytes）
	VGpuCoresUsage float64 `json:"vgpu_cores_usage" help:"算力使用率"` // 算力使用率，百分比，0-100
	MemoryLimit    float64 `json:"memory_limit" help:"显存限制"`      // 显存限制（bytes）
	VGpuCoresLimit float64 `json:"vgpu_cores_limit" help:"算力限制"`  // 算力限制（整数，1-100，针对单卡）
	Timestamp      int64   `json:"timestamp"`                     // 时间戳
}

func init() {
	register(vGpuMetricsCollectorName, enabled, createVCudaGpuCollector)
}

// vCudaGpuCollector vGPU指标采集器（采用vGPUmonitor相同的底层采集方式）
type vCudaGpuCollector struct {
	name            string
	nodeName        string
	descs           map[string]*prometheus.Desc
	podLister       listerscorev1.PodLister
	containerLister *nvidia.ContainerLister
	metricsCache    []containerMetrics
	cacheLock       sync.RWMutex
	lastCollectTime time.Time
}

// containerMetrics 容器级指标缓存
type containerMetrics struct {
	podUID         string // Pod UID（用于日志追踪）
	namespace      string
	podName        string
	containerName  string
	deviceIndex    int
	deviceUUID     string
	memoryUsed     uint64
	memoryLimit    uint64
	smUtilization  uint64
	vgpuCoresLimit float64 // 从Pod Annotation解析的实际算力分配值（如30表示0.3卡）
}

// hostMetrics 主机级指标缓存
type hostMetrics struct {
	deviceIndex    int
	deviceUUID     string
	deviceType     string
	memoryUsed     uint64
	gpuUtilization uint64
}

// Collect prometheus会调用此方法上报数据
func (c vCudaGpuCollector) Collect(ch chan<- prometheus.Metric) {
	logger.Info("vCudaGpuCollector Collect - using NVML + shared memory")

	c.cacheLock.RLock()
	defer c.cacheLock.RUnlock()

	if c.metricsCache == nil || len(c.metricsCache) == 0 {
		logger.Info("no cached metrics available, waiting for first collection")
		return
	}

	// 上报容器级指标
	for _, metric := range c.metricsCache {
		// 显存使用量：直接使用原始值（bytes）
		memoryUsage := float64(metric.memoryUsed)

		// 算力使用率：直接使用原始值（百分比，0-100范围）
		coresUsage := float64(metric.smUtilization)

		// 构建标签列表
		labels := []string{c.nodeName, metric.podName, metric.namespace, metric.deviceUUID}

		// 上报显存使用量（bytes）+ 打印日志
		emitMetric(ch, c.descs["MemoryUsage"], memoryUsage, labels...)
		logMetricEmission("vst_vCudaGpu_memory_usage", memoryUsage, metric.podUID, metric.deviceUUID, labels...)

		// 上报显存限制（bytes）+ 打印日志
		emitMetric(ch, c.descs["MemoryLimit"], float64(metric.memoryLimit), labels...)
		logMetricEmission("vst_vCudaGpu_memory_limit", float64(metric.memoryLimit), metric.podUID, metric.deviceUUID, labels...)

		// 上报算力使用率（百分比，0-100）+ 打印日志
		emitMetric(ch, c.descs["VGpuCoresUsage"], coresUsage, labels...)
		logMetricEmission("vst_vCudaGpu_vgpu_cores_usage", coresUsage, metric.podUID, metric.deviceUUID, labels...)

		// 算力限制：从Pod Annotation获取实际分配值（如30表示0.3卡）+ 打印日志
		emitMetric(ch, c.descs["VGpuCoresLimit"], metric.vgpuCoresLimit, labels...)
		logMetricEmission("vst_vCudaGpu_vgpu_cores_limit", metric.vgpuCoresLimit, metric.podUID, metric.deviceUUID, labels...)
	}
}

// emitMetric 发送单个指标到Prometheus通道
func emitMetric(ch chan<- prometheus.Metric, desc *prometheus.Desc, value float64, labels ...string) {
	metric, err := prometheus.NewConstMetric(desc, prometheus.GaugeValue, value, labels...)
	if err != nil {
		logger.Error("failed to emit metric: %v", err)
		return
	}
	ch <- metric
}

// logMetricEmission 打印指标上报的详细信息（用于调试）
func logMetricEmission(metricName string, value float64, podUID, deviceUUID string, labels ...string) {
	logger.Info("📊 [METRIC] name=%s value=%.4f pod-uid=%s device=%s labels=%v",
		metricName, value, podUID, deviceUUID, labels)
}

// Describe 指标描述信息
func (c vCudaGpuCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range c.descs {
		ch <- desc
	}
}

// collectMetrics 采集指标数据（核心逻辑，与vGPUmonitor相同）
func (c *vCudaGpuCollector) collectMetrics() {
	logger.Info("starting metrics collection from shared memory + NVML")

	var metrics []containerMetrics

	// 1. 从共享内存获取容器使用数据（与vGPUmonitor相同的方式）
	containers := c.containerLister.ListContainers()
	containerMap := make(map[string][]*nvidia.ContainerUsage)
	for _, ctr := range containers {
		if ctr.Info != nil && ctr.PodUID != "" {
			containerMap[ctr.PodUID] = append(containerMap[ctr.PodUID], ctr)
		}
	}

	// 2. 获取当前节点的Pod列表
	nodeName := os.Getenv(util.NodeNameEnvName)
	if nodeName == "" {
		logger.Error("NODE_NAME environment variable not set")
		return
	}

	pods, err := c.podLister.List(labels.SelectorFromSet(labels.Set{util.AssignedNodeAnnotations: nodeName}))
	if err != nil {
		logger.Error("Failed to list pods for node %s: %v", nodeName, err)
		return
	}

	// 3. 遍历每个Pod和容器，提取指标
	for _, pod := range pods {
		podContainers, found := containerMap[string(pod.UID)]
		if !found {
			continue
		}

		for _, ctr := range pod.Spec.Containers {
			for _, cusage := range podContainers {
				if cusage.ContainerName != ctr.Name {
					continue
				}

				if cusage.Info == nil {
					continue
				}

				// 提取每个设备的指标
				for i := 0; i < cusage.Info.DeviceNum(); i++ {
					uuid := cusage.Info.DeviceUUID(i)
					if len(uuid) < 40 {
						continue
					}
					uuid = uuid[0:40]

					// 收集各项指标
					memoryTotal := cusage.Info.DeviceMemoryTotal(i)
					memoryLimit := cusage.Info.DeviceMemoryLimit(i)
					smUtil := cusage.Info.DeviceSmUtil(i)

					// 从共享内存读取算力限制（如30表示0.3卡）
					coresLimit := float64(cusage.Info.DeviceSmLimit(i))

					metrics = append(metrics, containerMetrics{
						podUID:         string(pod.UID), // 记录Pod UID用于日志追踪
						namespace:      pod.Namespace,
						podName:        pod.Name,
						containerName:  ctr.Name,
						deviceIndex:    i,
						deviceUUID:     uuid,
						memoryUsed:     memoryTotal,
						memoryLimit:    memoryLimit,
						smUtilization:  smUtil,
						vgpuCoresLimit: coresLimit, // 使用从共享内存读取的真实值
					})
				}
			}
		}
	}

	// 4. 更新缓存
	c.cacheLock.Lock()
	c.metricsCache = metrics
	c.lastCollectTime = time.Now()
	c.cacheLock.Unlock()

	logger.Info("metrics collection completed, collected %d container device metrics", len(metrics))
}

// createVCudaGpuCollector 创建vCUDA GPU采集器实例
func createVCudaGpuCollector() prometheus.Collector {
	nodeName := os.Getenv("MY_NODE_NAME")
	if nodeName == "" {
		var err error
		nodeName, err = os.Hostname()
		if err != nil {
			logger.Fatal("failed to get hostname: %v", err)
		}
	}

	// 创建指标描述符（保持原有定义不变）
	tmp := VGpuInfo{}
	targetType := reflect.TypeOf(tmp)
	metricHelp := make(map[string]string, targetType.NumField())

	for i := 0; i < targetType.NumField(); i++ {
		field := targetType.Field(i)
		tagValue := field.Tag.Get("help")
		if tagValue != "" {
			metricHelp[field.Name] = tagValue
		}
	}

	descs := make(map[string]*prometheus.Desc)
	for metric, help := range metricHelp {
		metricName := prometheus.BuildFQName(collectorNamespace, "vCudaGpu", metric)
		descs[metric] = prometheus.NewDesc(
			metricName,
			help,
			[]string{"node", "pod", "namespace", "gpu_device"},
			nil,
		)
	}

	// 初始化ContainerLister（与vGPUmonitor相同）
	containerLister, err := nvidia.NewContainerLister()
	if err != nil {
		logger.Fatal("failed to create container lister: %v", err)
	}

	// 初始化K8s Informer用于获取Pod列表
	informerFactory := informers.NewSharedInformerFactoryWithOptions(
		containerLister.Clientset(),
		time.Hour*1,
	)
	podLister := informerFactory.Core().V1().Pods().Lister()
	stopCh := make(chan struct{})
	informerFactory.Start(stopCh)

	collector := &vCudaGpuCollector{
		name:            vGpuMetricsCollectorName,
		nodeName:        nodeName,
		descs:           descs,
		podLister:       podLister,
		containerLister: containerLister,
		metricsCache:    nil,
	}

	// 启动后台goroutine定时采集数据（与vGPUmonitor相同的更新机制）
	go func() {
		ticker := time.NewTicker(metricsCollectInterval)
		defer ticker.Stop()

		for {
			// 更新容器列表（与vGPUmonitor的watchAndFeedback相同）
			if err := containerLister.Update(); err != nil {
				logger.Error("Failed to update container list: %v", err)
			}

			// 采集指标
			collector.collectMetrics()

			select {
			case <-ticker.C:
				continue
			case <-stopCh:
				return
			}
		}
	}()

	return collector
}
