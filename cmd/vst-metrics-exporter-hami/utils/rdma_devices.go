package utils

import (
	"encoding/json"
	"strings"
)

type RDMADeviceInfo struct {
	RDMADevice string
	PCIAddress string
}

// 解析pod状态,获取RDMADevices信息
func ExtractRDMADevices(annotation string) []RDMADeviceInfo {
	var result []RDMADeviceInfo
	var entries []map[string]interface{}
	if err := json.Unmarshal([]byte(annotation), &entries); err != nil {
		return result
	}
	for _, entry := range entries {
		if entry["name"] == nil || !strings.Contains(entry["name"].(string), "ib-device") {
			continue
		}
		deviceInfo := entry["device-info"].(map[string]interface{})
		pci := deviceInfo["pci"].(map[string]interface{})
		result = append(result, RDMADeviceInfo{
			RDMADevice: pci["rdma-device"].(string),
			PCIAddress: pci["pci-address"].(string),
		})
	}
	return result
}
