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

// TestDebugger is used to play with install.go for ad-hoc testing.
//func TestDebugger(t *testing.T) {
//	c := getInitializedCommand(t)
//	c.Run([]string{"-auto-approve", "-f=../../config.yaml"})
//}

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
	c.kubernetes.CoreV1().PersistentVolumeClaims("default").Create(context.TODO(), pvc, metav1.CreateOptions{})
	c.kubernetes.CoreV1().PersistentVolumeClaims("default").Create(context.TODO(), pvc2, metav1.CreateOptions{})
	err := c.deletePVCs("consul", "default")
	require.NoError(t, err)
	pvcs, err := c.kubernetes.CoreV1().PersistentVolumeClaims("default").List(context.TODO(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, pvcs.Items, 0)

	// Clear out the client and make sure the check now passes.
	//c.kubernetes = fake.NewSimpleClientset()
	//err = c.checkForPreviousPVCs()
	//require.NoError(t, err)

	// Add a new irrelevant PVC and make sure the check continues to pass.
	//pvc = &v1.PersistentVolumeClaim{
	//	ObjectMeta: metav1.ObjectMeta{
	//		Name: "irrelevant-pvc",
	//	},
	//}
	//c.kubernetes.CoreV1().PersistentVolumeClaims("default").Create(context.TODO(), pvc, metav1.CreateOptions{})
	//err = c.checkForPreviousPVCs()
	//require.NoError(t, err)
}

//func TestCheckForPreviousSecrets(t *testing.T) {
//	c := getInitializedCommand(t)
//	c.kubernetes = fake.NewSimpleClientset()
//	secret := &v1.Secret{
//		ObjectMeta: metav1.ObjectMeta{
//			Name: "test-consul-bootstrap-acl-token",
//		},
//	}
//	c.kubernetes.CoreV1().Secrets("default").Create(context.TODO(), secret, metav1.CreateOptions{})
//	err := c.checkForPreviousSecrets()
//	require.Error(t, err)
//	require.Contains(t, err.Error(), "found consul-acl-bootstrap-token secret from previous installations: \"test-consul-bootstrap-acl-token\" in namespace \"default\". To delete, run kubectl delete secret test-consul-bootstrap-acl-token --namespace default")
//
//	// Clear out the client and make sure the check now passes.
//	c.kubernetes = fake.NewSimpleClientset()
//	err = c.checkForPreviousSecrets()
//	require.NoError(t, err)
//
//	// Add a new irrelevant secret and make sure the check continues to pass.
//	secret = &v1.Secret{
//		ObjectMeta: metav1.ObjectMeta{
//			Name: "irrelevant-secret",
//		},
//	}
//	c.kubernetes.CoreV1().Secrets("default").Create(context.TODO(), secret, metav1.CreateOptions{})
//	err = c.checkForPreviousSecrets()
//	require.NoError(t, err)
//}
