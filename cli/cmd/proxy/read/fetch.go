package read

import (
	"context"

	"k8s.io/client-go/kubernetes"
)

func FetchConfig(ctx context.Context, client kubernetes.Interface, namespace, podName string) (interface{}, error) {
	return nil, nil
}
