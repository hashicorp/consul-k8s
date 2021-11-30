package serveraclinit

import (
	"context"
	"testing"

	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
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

	serviceAccountName := resourcePrefix + "-connect-injector-authmethod-svc-account"
	secretName := resourcePrefix + "-connect-injector-authmethod-svc-account"

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
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:   secretName,
			Labels: map[string]string{common.CLILabelKey: common.CLILabelValue},
		},
		Data: map[string][]byte{},
		Type: v1.SecretTypeOpaque,
	}
	_, err := k8s.CoreV1().Secrets(ns).Create(ctx, secret, metav1.CreateOptions{})
	require.NoError(t, err)

	_, err = cmd.createAuthMethodTmpl("test")
	require.EqualError(t, err, "found no secret of type 'kubernetes.io/service-account-token' associated with the release-name-consul-connect-injector-authmethod-svc-account service account")
}
