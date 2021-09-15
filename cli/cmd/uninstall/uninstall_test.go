package uninstall

import (
	"context"
	"os"
	"testing"

	"github.com/hashicorp/consul-k8s/cli/cmd/common"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// Helper function which sets up a Command struct for you.
func getInitializedCommand(t *testing.T) *Command {
	t.Helper()
	log := hclog.New(&hclog.LoggerOptions{
		Name:   "cli",
		Level:  hclog.Info,
		Output: os.Stdout,
	})
	ctx, _ := context.WithCancel(context.Background())

	baseCommand := &common.BaseCommand{
		Ctx: ctx,
		Log: log,
	}

	c := &Command{
		BaseCommand: baseCommand,
	}
	c.init()
	c.Init()
	return c
}

func TestDeletePVCs(t *testing.T) {
	c := getInitializedCommand(t)
	c.kubernetes = fake.NewSimpleClientset()
	pvc := &v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consul-server-test1",
			Labels: map[string]string{
				"release": "consul",
			},
		},
	}
	pvc2 := &v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consul-server-test2",
			Labels: map[string]string{
				"release": "consul",
			},
		},
	}
	_, err := c.kubernetes.CoreV1().PersistentVolumeClaims("default").Create(context.TODO(), pvc, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.kubernetes.CoreV1().PersistentVolumeClaims("default").Create(context.TODO(), pvc2, metav1.CreateOptions{})
	require.NoError(t, err)
	err = c.deletePVCs("consul", "default")
	require.NoError(t, err)
	pvcs, err := c.kubernetes.CoreV1().PersistentVolumeClaims("default").List(context.TODO(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, pvcs.Items, 0)
}

func TestDeleteSecrets(t *testing.T) {
	c := getInitializedCommand(t)
	c.kubernetes = fake.NewSimpleClientset()
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consul-test-secret1",
			Labels: map[string]string{
				"release": "consul",
			},
		},
	}
	secret2 := &v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consul-test-secret2",
			Labels: map[string]string{
				"release": "consul",
			},
		},
	}
	_, err := c.kubernetes.CoreV1().Secrets("default").Create(context.TODO(), secret, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.kubernetes.CoreV1().Secrets("default").Create(context.TODO(), secret2, metav1.CreateOptions{})
	require.NoError(t, err)
	err = c.deleteSecrets("consul", "default")
	require.NoError(t, err)
	secrets, err := c.kubernetes.CoreV1().Secrets("default").List(context.TODO(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, secrets.Items, 0)
}

func TestDeleteServiceAccounts(t *testing.T) {
	c := getInitializedCommand(t)
	c.kubernetes = fake.NewSimpleClientset()
	sa := &v1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consul-test-sa1",
			Labels: map[string]string{
				"release": "consul",
			},
		},
	}
	sa2 := &v1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consul-test-sa2",
			Labels: map[string]string{
				"release": "consul",
			},
		},
	}
	_, err := c.kubernetes.CoreV1().ServiceAccounts("default").Create(context.TODO(), sa, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.kubernetes.CoreV1().ServiceAccounts("default").Create(context.TODO(), sa2, metav1.CreateOptions{})
	require.NoError(t, err)
	err = c.deleteServiceAccounts("consul", "default")
	require.NoError(t, err)
	sas, err := c.kubernetes.CoreV1().ServiceAccounts("default").List(context.TODO(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, sas.Items, 0)
}
