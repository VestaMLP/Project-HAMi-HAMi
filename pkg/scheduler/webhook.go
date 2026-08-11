/*
Copyright 2024 The HAMi Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
)

const template = "Processing admission hook for pod %v/%v, UID: %v"

type webhook struct {
	decoder admission.Decoder
}

func NewWebHook() (*admission.Webhook, error) {
	logf.SetLogger(klog.NewKlogr())
	schema := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(schema); err != nil {
		return nil, err
	}
	decoder := admission.NewDecoder(schema)
	wh := &admission.Webhook{Handler: &webhook{decoder: decoder}}
	return wh, nil
}

func (h *webhook) Handle(_ context.Context, req admission.Request) admission.Response {
	pod := &corev1.Pod{}
	err := h.decoder.Decode(req, pod)
	if err != nil {
		klog.Errorf("Failed to decode request: %v", err)
		return admission.Errored(http.StatusBadRequest, err)
	}
	if len(pod.Spec.Containers) == 0 {
		klog.Warningf(template+" - Denying admission as pod has no containers", pod.Namespace, pod.Name, pod.UID)
		return admission.Denied("pod has no containers")
	}
	if pod.Spec.SchedulerName != "" &&
		(pod.Spec.SchedulerName != corev1.DefaultSchedulerName || !config.ForceOverwriteDefaultScheduler) &&
		(len(config.SchedulerName) == 0 || pod.Spec.SchedulerName != config.SchedulerName) {
		klog.V(3).Infof(template+" - Pod already has different scheduler assigned", req.Namespace, req.Name, req.UID)
		return admission.Allowed("pod already has different scheduler assigned")
	}
	klog.V(3).Infof(template+" webhook recv", req.Namespace, req.Name, req.UID)

	klog.V(3).Infof("[DEBUG] Request Info - Namespace: %q, Name: %q, UID: %q, Operation: %q", req.Namespace, req.Name, req.UID, req.Operation)
	klog.V(3).Infof("[DEBUG] Request Object.Raw:\n%s", string(req.Object.Raw))

	klog.V(3).Infof("[DEBUG] Before podVestaResourceAdaptor - Pod Name: %q, Namespace: %q, UID: %q", pod.Name, pod.Namespace, pod.UID)
	for ci, ctr := range pod.Spec.Containers {
		klog.V(3).Infof("[DEBUG] Container[%d] %s - Limits: %+v, Requests: %+v",
			ci, ctr.Name, ctr.Resources.Limits, ctr.Resources.Requests)
	}

	podVestaResourceAdaptor(pod)

	klog.V(4).Infof("[DEBUG] After podVestaResourceAdaptor - Pod Name: %q, Namespace: %q", pod.Name, pod.Namespace)
	for ci, ctr := range pod.Spec.Containers {
		klog.V(4).Infof("[DEBUG] Container[%d] %s - Limits: %+v, Requests: %+v",
			ci, ctr.Name, ctr.Resources.Limits, ctr.Resources.Requests)
	}

	klog.V(5).Infof(template, pod.Namespace, pod.Name, pod.UID)
	privilegedName, hasPrivileged := privilegedContainerName(pod)
	hasResource := false

	// 1. Process InitContainers
	for idx := range pod.Spec.InitContainers {
		c := &pod.Spec.InitContainers[idx]
		for _, val := range device.GetDevices() {
			found, err := val.MutateAdmission(c, pod)
			if err != nil {
				klog.Errorf("validating pod failed:%s", err.Error())
				return admission.Errored(http.StatusInternalServerError, err)
			}
			hasResource = hasResource || found
		}
	}

	for idx := range pod.Spec.Containers {
		c := &pod.Spec.Containers[idx]
		for _, val := range device.GetDevices() {
			found, err := val.MutateAdmission(c, pod)
			if err != nil {
				klog.Errorf("validating pod failed:%s", err.Error())
				return admission.Errored(http.StatusInternalServerError, err)
			}
			hasResource = hasResource || found
		}
	}
	if hasPrivileged && hasResource {
		klog.Warningf(template+" - Denying admission as container %s is privileged", pod.Namespace, pod.Name, pod.UID, privilegedName)
		return admission.Denied(fmt.Sprintf("container %s is privileged", privilegedName))
	}

	if !hasResource {
		klog.V(3).Infof(template+" - Allowing admission: no GPU resource found", req.Namespace, req.Name, req.UID)
	} else if len(config.SchedulerName) > 0 {
		pod.Spec.SchedulerName = config.SchedulerName
		if pod.Spec.NodeName != "" {
			klog.Infof(template+" - Pod already has node assigned", req.Namespace, req.Name, req.UID)
			return admission.Denied("pod has node assigned")
		}
	}
	if !fitResourceQuota(pod) {
		return admission.Denied("exceeding resource quota")
	}
	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		klog.Errorf(template+" - Failed to marshal pod, error: %v", req.Namespace, req.Name, req.UID, err)
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

func privilegedContainerName(pod *corev1.Pod) (string, bool) {
	for _, ctr := range pod.Spec.InitContainers {
		if isPrivilegedContainer(&ctr) {
			return ctr.Name, true
		}
	}
	for _, ctr := range pod.Spec.Containers {
		if isPrivilegedContainer(&ctr) {
			return ctr.Name, true
		}
	}
	return "", false
}

func isPrivilegedContainer(ctr *corev1.Container) bool {
	return ctr.SecurityContext != nil &&
		ctr.SecurityContext.Privileged != nil &&
		*ctr.SecurityContext.Privileged
}

func fitResourceQuota(pod *corev1.Pod) bool {
	for deviceName, dev := range device.GetDevices() {
		resourceNames := dev.GetResourceNames()
		if len(resourceNames.ResourceMemoryName) == 0 && len(resourceNames.ResourceCoreName) == 0 {
			// Nothing this backend exposes can carry a quota.
			continue
		}

		// Ask the backend what the pod is requesting rather than reading the
		// container spec here. It applies its own memory factor, defaults and
		// template rounding, which is what the scheduler later records as used,
		// so this keeps admission and the scheduler on the same numbers.
		var appMemoryReq, appCoresReq int64
		for i := range pod.Spec.Containers {
			req := dev.GenerateResourceRequests(&pod.Spec.Containers[i])
			if req.Nums == 0 {
				continue
			}
			appMemoryReq += int64(req.Memreq) * int64(req.Nums)
			appCoresReq += int64(req.Coresreq) * int64(req.Nums)
		}

		// Init containers run sequentially, so the pod's effective request is
		// max(sum(app containers), max(init containers)).
		var initMemoryReq, initCoresReq int64
		for i := range pod.Spec.InitContainers {
			req := dev.GenerateResourceRequests(&pod.Spec.InitContainers[i])
			if req.Nums == 0 {
				continue
			}
			initMemoryReq = max(initMemoryReq, int64(req.Memreq)*int64(req.Nums))
			initCoresReq = max(initCoresReq, int64(req.Coresreq)*int64(req.Nums))
		}

		memoryReq := max(appMemoryReq, initMemoryReq)
		coresReq := max(appCoresReq, initCoresReq)
		if memoryReq == 0 && coresReq == 0 {
			continue
		}

		klog.V(5).Infof("Checking quota for device %s: memory %d, cores %d, factor %d", deviceName, memoryReq, coresReq, resourceNames.MemoryFactor)
		if !device.GetLocalCache().FitQuota(pod.Namespace, memoryReq, resourceNames.MemoryFactor, coresReq, deviceName) {
			klog.Infof(template+" - Denying admission", pod.Namespace, pod.Name, pod.UID)
			return false
		}
	}
	return true
}
