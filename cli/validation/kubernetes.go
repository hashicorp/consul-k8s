package validation

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/consul-k8s/cli/common"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ListConsulSecrets attempts to find secrets with the Consul label.
func ListConsulSecrets(ctx context.Context, client kubernetes.Interface) (*v1.SecretList, error) {
	secrets, err := client.CoreV1().Secrets(v1.NamespaceDefault).List(ctx, metav1.ListOptions{
		LabelSelector: common.CLILabelKey + "=" + common.CLILabelValue,
	})

	return secrets, err
}

// ListConsulPVCs attempts to find PVCs which have "consul-server" in the name.
func ListConsulPVCs(ctx context.Context, client kubernetes.Interface) ([]v1.PersistentVolumeClaim, error) {
	pvcs, err := client.CoreV1().PersistentVolumeClaims(v1.NamespaceDefault).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("error listing persistent volume claims: %s", err)
	}

	var consulPvcs []v1.PersistentVolumeClaim
	for _, pvc := range pvcs.Items {
		if strings.Contains(pvc.Name, "consul-server") {
			consulPvcs = append(consulPvcs, pvc)
		}
	}

	return consulPvcs, nil
}

// IsValidEnterprise checks if the Consul Enterprise secret is set.
func IsValidEnterprise(ctx context.Context, client kubernetes.Interface, namespace, secretName string) (bool, error) {
	_, err := client.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	return true, nil
}
