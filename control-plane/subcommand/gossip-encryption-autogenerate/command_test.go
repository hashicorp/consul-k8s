package gossipencryptionautogenerate

import (
	"context"
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRun_FlagFailures(t *testing.T) {
	t.Parallel()
	cases := []struct {
		flags  []string
		expErr string
	}{
		{
			flags:  []string{},
			expErr: "-namespace must be set",
		},
		{
			flags:  []string{"-namespace", "default"},
			expErr: "-secret-name must be set",
		},
		{
			flags:  []string{"-namespace", "default", "-secret-name", "my-secret", "-log-level", "oak"},
			expErr: "unknown log level",
		},
	}

	for _, c := range cases {
		t.Run(c.expErr, func(t *testing.T) {
			ui := cli.NewMockUi()
			cmd := Command{
				UI: ui,
			}
			code := cmd.Run(c.flags)
			require.Equal(t, 1, code)
			require.Contains(t, ui.ErrorWriter.String(), c.expErr)
		})
	}
}

func TestRun_SafeFailIfSecretExists(t *testing.T) {
	namespace := "default"
	secretName := "my-secret"
	secretKey := "my-secret-key"

	ui := cli.NewMockUi()
	k8s := fake.NewSimpleClientset()

	cmd := Command{UI: ui, k8sClient: k8s}

	// Create a secret
	secret := v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			secretKey: []byte(secretKey),
		},
	}
	_, err := k8s.CoreV1().Secrets(namespace).Create(context.Background(), &secret, metav1.CreateOptions{})
	require.NoError(t, err)

	// Run the command
	flags := []string{"-namespace", namespace, "-secret-name", secretName, "-secret-key", secretKey}
	code := cmd.Run(flags)

	require.Equal(t, 0, code)
	require.Contains(t, ui.OutputWriter.String(), fmt.Sprintf("A Kubernetes secret with the name `%s` already exists.", secretName))
}

func TestCreateK8sClient_FailIfNoK8s(t *testing.T) {
	cmd := Command{}

	err := cmd.createK8sClient()
	require.Error(t, err)
}

func TestK8sSecretExists_FailIfNoClient(t *testing.T) {
	cmd := Command{}

	_, err := cmd.doesK8sSecretExist()
	require.Error(t, err)
}

func TestK8sSecretExists_WithTrue(t *testing.T) {
	namespace := "default"
	secretName := "my-secret"
	secretKey := "my-secret-key"

	k8s := fake.NewSimpleClientset()

	cmd := Command{
		flagNamespace:  namespace,
		flagSecretName: secretName,
		flagSecretKey:  secretKey,
		k8sClient:      k8s,
	}

	secret := v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			secretKey: []byte(secretKey),
		},
	}
	_, err := k8s.CoreV1().Secrets(namespace).Create(context.Background(), &secret, metav1.CreateOptions{})
	require.NoError(t, err)

	actual, err := cmd.doesK8sSecretExist()
	require.NoError(t, err)
	require.True(t, actual)
}

func TestK8sSecretExists_WithFalse(t *testing.T) {
	namespace := "default"
	secretName := "my-secret-2"
	secretKey := "my-secret-key"

	k8s := fake.NewSimpleClientset()

	cmd := Command{
		flagNamespace:  namespace,
		flagSecretName: secretName,
		flagSecretKey:  secretKey,
		k8sClient:      k8s,
	}

	actual, err := cmd.doesK8sSecretExist()
	require.NoError(t, err)
	require.False(t, actual)
}

func TestCreateK8sSecret_FailIfNamespaceNameOrKeyIsEmpty(t *testing.T) {
	t.Parallel()
	cases := []struct {
		namespace, name, key string
	}{
		{"", "name", "key"},
		{"namespace", "", "key"},
		{"namespace", "name", ""},
	}

	for _, c := range cases {
		t.Run(
			fmt.Sprintf("flagSecretName: %s, flagSecretKey %s", c.name, c.key),
			func(t *testing.T) {
				cmd := Command{
					flagSecretName: c.name,
					flagSecretKey:  c.key,
				}

				_, err := cmd.createK8sSecret("")
				require.Error(t, err)
			})
	}
}

func TestWriteToK8s_FailIfNoClient(t *testing.T) {
	cmd := Command{}

	secret := v1.Secret{
		ObjectMeta: metav1.ObjectMeta{},
		Data:       map[string][]byte{},
	}
	err := cmd.writeToK8s(secret)
	require.Error(t, err)
}

func TestGenerateGossipSecret(t *testing.T) {
	actual, err := generateGossipSecret()
	require.NoError(t, err)

	actualDecoded, err := base64.StdEncoding.DecodeString(actual)
	require.NoError(t, err)

	require.Len(t, actualDecoded, 32)
}
