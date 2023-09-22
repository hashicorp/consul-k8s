// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package serveraclinit

import (
	"context"
	"fmt"

	apiv1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
)

const SecretsBackendTypeKubernetes SecretsBackendType = "kubernetes"

type KubernetesSecretsBackend struct {
	ctx          context.Context
	clientset    kubernetes.Interface
	k8sNamespace string
	secretName   string
	secretKey    string
}

var _ SecretsBackend = (*KubernetesSecretsBackend)(nil)

// BootstrapToken returns the existing bootstrap token if there is one by
// reading the Kubernetes Secret. If there is no bootstrap token yet, then
// it returns an empty string (not an error).
func (b *KubernetesSecretsBackend) BootstrapToken() (string, error) {
	secret, err := b.clientset.CoreV1().Secrets(b.k8sNamespace).Get(b.ctx, b.secretName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	token, ok := secret.Data[b.secretKey]
	if !ok {
		return "", fmt.Errorf("secret %q does not have data key %q", b.secretName, b.secretKey)
	}
	return string(token), nil

}

// WriteBootstrapToken writes the given bootstrap token to the Kubernetes Secret.
func (b *KubernetesSecretsBackend) WriteBootstrapToken(bootstrapToken string) error {
	secret := &apiv1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:   b.secretName,
			Labels: map[string]string{common.CLILabelKey: common.CLILabelValue},
		},
		Data: map[string][]byte{
			b.secretKey: []byte(bootstrapToken),
		},
	}
	_, err := b.clientset.CoreV1().Secrets(b.k8sNamespace).Create(b.ctx, secret, metav1.CreateOptions{})
	return err
}

func (b *KubernetesSecretsBackend) BootstrapTokenSecretName() string {
	return b.secretName
}
