// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package serveraclinit

import (
	"context"
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
)

// Test that createAuthMethodTmpl returns an error when
// it cannot find a secret of type kubernetes.io/service-account-token
// associated with the service account.
// Note we are only testing this special error case here because we cannot assert on log output
// from the command.
// Also note that the remainder of this function is tested in the command_test.go.
func TestCommand_createAuthMethodTmpl_SecretNotFound(t *testing.T) {
	k8s := fake.NewSimpleClientset()
	ctx := context.Background()

	cmd := &Command{
		flagK8sNamespace:   ns,
		flagResourcePrefix: resourcePrefix,
		clientset:          k8s,
		log:                hclog.New(nil),
		ctx:                ctx,
	}

	// create the auth method secret since it is always deployed by helm chart.
	authMethodSecretName := resourcePrefix + "-auth-method"
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:   authMethodSecretName,
			Labels: map[string]string{common.CLILabelKey: common.CLILabelValue},
		},
		Data: map[string][]byte{},
		// Make it not a service-account-token so the test can pass through to checking the other secrets.
		Type: v1.SecretTypeOpaque,
	}
	_, err := k8s.CoreV1().Secrets(ns).Create(ctx, secret, metav1.CreateOptions{})
	require.NoError(t, err)

	serviceAccountName := resourcePrefix + "-auth-method"
	secretName := resourcePrefix + "-connect-injector"

	// Create a service account referencing secretName
	sa, _ := k8s.CoreV1().ServiceAccounts(ns).Get(ctx, serviceAccountName, metav1.GetOptions{})
	if sa == nil {
		_, err := k8s.CoreV1().ServiceAccounts(ns).Create(
			ctx,
			&v1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name: serviceAccountName,
				},
				Secrets: []v1.ObjectReference{
					{
						Name: secretName,
					},
				},
			},
			metav1.CreateOptions{})
		require.NoError(t, err)
	}

	// Create a secret of non service-account-token type (we're using the opaque type).
	secret = &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:   secretName,
			Labels: map[string]string{common.CLILabelKey: common.CLILabelValue},
		},
		Data: map[string][]byte{},
		Type: v1.SecretTypeOpaque,
	}
	_, err = k8s.CoreV1().Secrets(ns).Create(ctx, secret, metav1.CreateOptions{})
	require.NoError(t, err)

	_, err = cmd.createAuthMethodTmpl("test", true)
	require.EqualError(t, err, "found no secret of type 'kubernetes.io/service-account-token' associated with the release-name-consul-auth-method service account")
}

// Test that createAuthMethodTmpl succeeds in the case where the serviceAccount exists but no secrets are automatically
// created by Kubernetes for it. This is the behaviour that is present in Kube-1.24+.
func TestCommand_createAuthMethodTmpl(t *testing.T) {
	serviceAccountName := resourcePrefix + "-auth-method"
	secretName := resourcePrefix + "-auth-method"
	k8s := fake.NewSimpleClientset()
	ctx := context.Background()

	// Create a service account that does not reference a secret.
	_, err := k8s.CoreV1().ServiceAccounts(ns).Create(ctx, &v1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: serviceAccountName}}, metav1.CreateOptions{})
	require.NoError(t, err)

	// Create a secret that references the serviceaccount.
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:   secretName,
			Labels: map[string]string{common.CLILabelKey: common.CLILabelValue},
			Annotations: map[string]string{
				v1.ServiceAccountNameKey: serviceAccountName,
			},
		},
		Data: map[string][]byte{},
		Type: v1.SecretTypeServiceAccountToken,
	}
	_, err = k8s.CoreV1().Secrets(ns).Create(ctx, secret, metav1.CreateOptions{})
	require.NoError(t, err)

	cmd := &Command{
		flagK8sNamespace:   ns,
		flagResourcePrefix: resourcePrefix,
		clientset:          k8s,
		log:                hclog.New(nil),
		ctx:                ctx,
	}

	_, err = cmd.createAuthMethodTmpl("test", true)
	require.NoError(t, err)
}
