package collectors

import (
	"flag"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"log"
)

var (
	collectorNamespace = "vst"
	enabled            = true
	disabled           = false
	collectorState     = make(map[string]*bool)                       // 记录采集器状态
	collectorFunctions = make(map[string]func() prometheus.Collector) // 存储每个采集器的构造函数
)

// 用于组合多个指标采集器
type VstCollector []prometheus.Collector

// register 用于注册一个新的采集器。
// 参数：
// - name：采集器名称（作为命令行参数使用）
// - enabled：是否默认启用
// - collector：返回 prometheus.Collector 的构造函数
//
// 此函数将注册到两个全局 map 中，并绑定命令行参数 --collector.<name>
func register(name string, enabled bool, collector func() prometheus.Collector) {
	collectorState[name] = &enabled                                                                               // 记录启用状态
	collectorFunctions[name] = collector                                                                          // 存储采集器构造函数
	flag.BoolVar(collectorState[name], "collector."+name, enabled, fmt.Sprintf("Enables the %v collector", name)) // 添加命令行参数
}

// 依次调用每个子采集器的 Collect 方法，将采集到的指标发送到 prometheus
func (s VstCollector) Collect(ch chan<- prometheus.Metric) {
	for _, collector := range s {
		collector.Collect(ch)
	}
}

// 用于返回所有采集器支持的指标描述信息
func (s VstCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, collector := range s {
		collector.Describe(ch)
	}
}

// Enabled 函数会遍历所有已注册的采集器，并根据其是否启用来生成最终生效的采集器列表。
// 启用状态由默认值或命令行参数决定。
func Enabled() VstCollector {
	collectors := make([]prometheus.Collector, 0)
	for collector, enabled := range collectorState {
		if enabled != nil && *enabled {
			log.Printf("The %v collector is enabled", collector)
			collectors = append(collectors, collectorFunctions[collector]())
		}
	}
	return collectors
}

var logFatal = func(msg string, args ...any) {
	log.Fatalf(msg, args...)
}

var logError = func(msg string, args ...any) {
	log.Printf(msg, args...)
}

var logInfo = func(msg string, args ...any) {
	log.Printf(msg, args...)
}
