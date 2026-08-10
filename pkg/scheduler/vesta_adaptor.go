package scheduler

import (
	"regexp"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
)

const (
	vestaRscCoreName   = "yxqiche.com/vcuda-core"
	vestaRscMemoryName = "yxqiche.com/vcuda-memory"
)

func podVestaResourceAdaptor(pod *corev1.Pod) {
	klog.V(4).Infof("Adapting pod %s/%s resources", pod.Namespace, pod.Name)

	nvDevice := device.GetDevices()[nvidia.NvidiaGPUDevice]

	nvidiaDevice, ok := nvDevice.(*nvidia.NvidiaGPUDevices)
	if !ok || nvidiaDevice == nil {
		klog.Error("Failed to get nvidia gpu devices in podVestaResourceAdaptor")
		return
	}

	nvidiaGPUCountName := corev1.ResourceName(nvidiaDevice.GetConfig().ResourceCountName)
	nvidiaCoreName := corev1.ResourceName(nvidiaDevice.GetConfig().ResourceCoreName)
	nvidiaMemPercentageName := corev1.ResourceName(nvidiaDevice.GetConfig().ResourceMemoryPercentageName)

	gpuCount := countGPUFromAnnotations(pod)

	hasVestaResource := false
	for idx, ctr := range pod.Spec.Containers {
		c := &pod.Spec.Containers[idx]
		vstCore, hasVstaCore := ctr.Resources.Limits[corev1.ResourceName(vestaRscCoreName)]
		if hasVstaCore {
			if !hasVestaResource {
				hasVestaResource = true
				if pod.Spec.NodeSelector == nil {
					pod.Spec.NodeSelector = make(map[string]string)
				}
				pod.Spec.NodeSelector["vgpu-enable"] = "hami"
			}
			//delete(c.Resources.Limits, corev1.ResourceName(vestaRscCoreName))
			//delete(c.Resources.Limits, corev1.ResourceName(vestaRscMemoryName))
			//delete(c.Resources.Requests, corev1.ResourceName(vestaRscCoreName))
			//delete(c.Resources.Requests, corev1.ResourceName(vestaRscMemoryName))

			c.Resources.Limits[nvidiaCoreName] = vstCore
			c.Resources.Limits[nvidiaMemPercentageName] = vstCore
			c.Resources.Requests[nvidiaCoreName] = vstCore
			c.Resources.Requests[nvidiaMemPercentageName] = vstCore

			gpuCountQuantity := *resource.NewQuantity(int64(gpuCount), resource.BinarySI)
			c.Resources.Limits[nvidiaGPUCountName] = gpuCountQuantity
			c.Resources.Requests[nvidiaGPUCountName] = gpuCountQuantity

			klog.V(4).Infof("Adapted pod %s container %s resources: %s/%s -> %s/%s/%s with core value %v, gpu count %d", pod.Name, c.Name, vestaRscCoreName, vestaRscMemoryName, nvidiaGPUCountName, nvidiaCoreName, nvidiaMemPercentageName, vstCore, gpuCount)
		}
	}
}

func countGPUFromAnnotations(pod *corev1.Pod) int {
	defaultGPUCount := 1
	if pod.Annotations == nil {
		return defaultGPUCount
	}

	pattern := regexp.MustCompile(`^` + regexp.QuoteMeta(vestaRscCoreName) + `\d+$`)
	count := 0
	for annotationKey := range pod.Annotations {
		if pattern.MatchString(annotationKey) {
			count++
			klog.V(5).Infof("Matched GPU annotation: %s", annotationKey)
		}
	}

	if count == 0 {
		return defaultGPUCount
	}

	klog.V(4).Infof("Found %d GPU annotations in pod %s/%s matching pattern %s\\d+", count, pod.Namespace, pod.Name, vestaRscCoreName)
	return count
}
