package utils

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ListLocalNodePods 获取当前节点所有Pod信息 (简化版本，用于兼容)
func ListLocalNodePods(cli *kubernetes.Clientset, nameSpace, nodeName string) ([]corev1.Pod, error) {
	if cli == nil {
		return nil, fmt.Errorf("kubernetes client is nil")
	}

	pods, err := cli.CoreV1().Pods(nameSpace).List(context.TODO(), metav1.ListOptions{
		FieldSelector: "spec.nodeName=" + nodeName,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	return pods.Items, nil
}
