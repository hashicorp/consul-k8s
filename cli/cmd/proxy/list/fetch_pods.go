package list

import (
	"context"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// FetchPods attempts to fetch all pods which are running Envoy in the cluster
// based on their labels.
func FetchPods(ctx context.Context, client kubernetes.Interface, namespace string) ([]v1.Pod, error) {
	var pods []v1.Pod

	gatewaypods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "component in (ingress-gateway, mesh-gateway, terminating-gateway)",
	})
	if err != nil {
		return nil, err
	}
	pods = append(pods, gatewaypods.Items...)

	apigatewaypods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "api-gateway.consul.hashicorp.com/managed=true",
	})
	if err != nil {
		return nil, err
	}
	pods = append(pods, apigatewaypods.Items...)

	sidecarpods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "consul.hashicorp.com/connect-inject-status=injected",
	})
	if err != nil {
		return nil, err
	}
	pods = append(pods, sidecarpods.Items...)

	return pods, nil
}
