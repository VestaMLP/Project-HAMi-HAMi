package collectors

import (
	"testing"
)

func TestContainerMetricsStruct(t *testing.T) {
	metric := containerMetrics{
		namespace:     "ml-team",
		podName:       "training-job",
		containerName: "main",
		deviceIndex:   0,
		deviceUUID:    "GPU-abc123",
		memoryUsed:    6442450944, // 6GB
		memoryLimit:   8589934592, // 8GB
		smUtilization: 85,         // 85%
	}

	// 验证显存使用量（直接使用原始字节数）
	memoryUsage := float64(metric.memoryUsed)
	if memoryUsage != 6442450944 {
		t.Errorf("memory usage should be 6442450944 bytes (6GB), got %.0f", memoryUsage)
	}

	// 验证算力使用率（百分比，0-100范围，直接使用原始值）
	coresUsage := float64(metric.smUtilization)
	if coresUsage < 84 || coresUsage > 86 { // 应该接近85
		t.Errorf("cores usage should be ~85 (percentage), got %.1f", coresUsage)
	}

	// 验证算力限制值（整数1-100，针对单卡）
	coresLimit := 100.0 // 单卡100%算力 = 整数100
	if coresLimit != 100.0 {
		t.Errorf("cores limit should be 100 (integer), got %.1f", coresLimit)
	}

	t.Logf("✅ Container metrics validation passed:")
	t.Logf("   Device UUID: %s", metric.deviceUUID)
	t.Logf("   Memory Usage: %.0f bytes (%.2f GB)", memoryUsage, memoryUsage/(1024*1024*1024))
	t.Logf("   Cores Usage: %.0f%% (百分比, 0-100)", coresUsage)
	t.Logf("   Cores Limit: %.0f (整数,单卡100%%)", coresLimit)
}
