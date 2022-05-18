package validation

import (
	"context"
	"fmt"

	"github.com/hashicorp/consul-k8s/cli/common"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ListConsulSecrets attempts to find secrets with the Consul label.
func ListConsulSecrets(ctx context.Context, client kubernetes.Interface, namespace string) (*v1.SecretList, error) {
	secrets, err := client.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", common.CLILabelKey, common.CLILabelValue),
	})

	return secrets, err
}
