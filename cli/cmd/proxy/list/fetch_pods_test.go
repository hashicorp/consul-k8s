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
			pods:      []v1.Pod{},
		},
		"API Gateway Pods": {
			namespace: "default",
			pods:      []v1.Pod{},
		},
		"Sidecar Pods": {
			namespace: "default",
			pods:      []v1.Pod{},
		},
		"All kinds of Pods": {
			namespace: "default",
			pods:      []v1.Pod{},
		},
		"Pods in multiple namespaces": {
			namespace: "",
			pods:      []v1.Pod{},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			client := fake.NewSimpleClientset()

			// Add the pods to the client.
			for _, pod := range c.pods {
				client.CoreV1().Pods(c.namespace).Create(context.Background(), &pod, metav1.CreateOptions{})
			}

			pods, err := FetchPods(context.Background(), client, c.namespace)
			require.NoError(t, err)

			if len(pods) != len(c.pods) {
				t.Errorf("FetchPods(%v) returned %d pods, expected %d", c.namespace, len(pods), len(c.pods))
			}
		})
	}
}
