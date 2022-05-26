package list

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestFetchPods(t *testing.T) {
	cases := map[string]struct {
		namespace string
		pods      []v1.Pod
	}{
		"No pods": {
			namespace: "default",
			pods:      []v1.Pod{},
		},
		"Gateway pods": {
			namespace: "default",
			pods: []v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ingress-gateway",
						Namespace: "default",
						Labels: map[string]string{
							"component": "ingress-gateway",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "mesh-gateway",
						Namespace: "default",
						Labels: map[string]string{
							"component": "mesh-gateway",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "terminating-gateway",
						Namespace: "default",
						Labels: map[string]string{
							"component": "terminating-gateway",
						},
					},
				},
			},
		},
		"API Gateway Pods": {
			namespace: "default",
			pods: []v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "api-gateway",
						Namespace: "default",
						Labels: map[string]string{
							"api-gateway.consul.hashicorp.com/managed": "true",
						},
					},
				},
			},
		},
		"Sidecar Pods": {
			namespace: "default",
			pods: []v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "sidecar",
						Namespace: "default",
						Labels: map[string]string{
							"consul.hashicorp.com/connect-inject-status": "injected",
						},
					},
				},
			},
		},
		"All kinds of Pods": {
			namespace: "default",
			pods: []v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "sidecar",
						Namespace: "default",
						Labels: map[string]string{
							"consul.hashicorp.com/connect-inject-status": "injected",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "mesh-gateway",
						Namespace: "default",
						Labels: map[string]string{
							"component": "mesh-gateway",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "api-gateway",
						Namespace: "default",
						Labels: map[string]string{
							"api-gateway.consul.hashicorp.com/managed": "true",
						},
					},
				},
			},
		},
		"Pods in multiple namespaces": {
			namespace: "",
			pods: []v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "api-gateway",
						Namespace: "consul",
						Labels: map[string]string{
							"api-gateway.consul.hashicorp.com/managed": "true",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "sidecar",
						Namespace: "default",
						Labels: map[string]string{
							"consul.hashicorp.com/connect-inject-status": "injected",
						},
					},
				},
			},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			client := fake.NewSimpleClientset()

			// Add the pods to the client.
			for _, pod := range c.pods {
				client.CoreV1().Pods(pod.ObjectMeta.Namespace).Create(context.Background(), &pod, metav1.CreateOptions{})
			}

			pods, err := FetchPods(context.Background(), client, c.namespace)
			require.NoError(t, err)

			if len(pods) != len(c.pods) {
				t.Errorf("FetchPods(%v) returned %d pods, expected %d", c.namespace, len(pods), len(c.pods))
			}
		})
	}
}
