package utils

import "testing"

func TestGetContainerInfoFromDocker(t *testing.T) {
	gpuDevice, err := GetContainerGpuDeviceFromDocker("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Log(gpuDevice)
}
