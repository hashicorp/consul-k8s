package uninstall

import (
	"context"
	"os"
	"testing"

	"github.com/hashicorp/consul-k8s/cli/cmd/common"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

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
	pvc3 := &v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "unrelated-pvc",
			Labels: map[string]string{
				"release": "unrelated",
			},
		},
	}
	_, err := c.kubernetes.CoreV1().PersistentVolumeClaims("default").Create(context.Background(), pvc, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.kubernetes.CoreV1().PersistentVolumeClaims("default").Create(context.Background(), pvc2, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.kubernetes.CoreV1().PersistentVolumeClaims("default").Create(context.Background(), pvc3, metav1.CreateOptions{})
	require.NoError(t, err)
	err = c.deletePVCs("consul", "default")
	require.NoError(t, err)
	pvcs, err := c.kubernetes.CoreV1().PersistentVolumeClaims("default").List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, pvcs.Items, 1)
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
	secret2 := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consul-test-secret2",
			Labels: map[string]string{
				"release": "consul",
			},
		},
	}
	_, err := c.kubernetes.CoreV1().Secrets("default").Create(context.Background(), secret, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.kubernetes.CoreV1().Secrets("default").Create(context.Background(), secret2, metav1.CreateOptions{})
	require.NoError(t, err)
	err = c.deleteSecrets("consul", "default")
	require.NoError(t, err)
	secrets, err := c.kubernetes.CoreV1().Secrets("default").List(context.Background(), metav1.ListOptions{})
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
	_, err := c.kubernetes.CoreV1().ServiceAccounts("default").Create(context.Background(), sa, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.kubernetes.CoreV1().ServiceAccounts("default").Create(context.Background(), sa2, metav1.CreateOptions{})
	require.NoError(t, err)
	err = c.deleteServiceAccounts("consul", "default")
	require.NoError(t, err)
	sas, err := c.kubernetes.CoreV1().ServiceAccounts("default").List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, sas.Items, 0)
}

func TestDeleteRoles(t *testing.T) {
	c := getInitializedCommand(t)
	c.kubernetes = fake.NewSimpleClientset()
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consul-test-role1",
			Labels: map[string]string{
				"release": "consul",
			},
		},
	}
	role2 := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consul-test-role2",
			Labels: map[string]string{
				"release": "consul",
			},
		},
	}
	_, err := c.kubernetes.RbacV1().Roles("default").Create(context.Background(), role, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.kubernetes.RbacV1().Roles("default").Create(context.Background(), role2, metav1.CreateOptions{})
	require.NoError(t, err)
	err = c.deleteRoles("consul", "default")
	require.NoError(t, err)
	roles, err := c.kubernetes.RbacV1().Roles("default").List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, roles.Items, 0)
}

func TestDeleteRoleBindings(t *testing.T) {
	c := getInitializedCommand(t)
	c.kubernetes = fake.NewSimpleClientset()
	rolebinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consul-test-role1",
			Labels: map[string]string{
				"release": "consul",
			},
		},
	}
	rolebinding2 := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consul-test-role2",
			Labels: map[string]string{
				"release": "consul",
			},
		},
	}
	_, err := c.kubernetes.RbacV1().RoleBindings("default").Create(context.Background(), rolebinding, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.kubernetes.RbacV1().RoleBindings("default").Create(context.Background(), rolebinding2, metav1.CreateOptions{})
	require.NoError(t, err)
	err = c.deleteRoleBindings("consul", "default")
	require.NoError(t, err)
	rolebindings, err := c.kubernetes.RbacV1().RoleBindings("default").List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, rolebindings.Items, 0)
}

func TestDeleteJobs(t *testing.T) {
	c := getInitializedCommand(t)
	c.kubernetes = fake.NewSimpleClientset()
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consul-test-job1",
			Labels: map[string]string{
				"release": "consul",
			},
		},
	}
	job2 := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consul-test-job2",
			Labels: map[string]string{
				"release": "consul",
			},
		},
	}
	_, err := c.kubernetes.BatchV1().Jobs("default").Create(context.Background(), job, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.kubernetes.BatchV1().Jobs("default").Create(context.Background(), job2, metav1.CreateOptions{})
	require.NoError(t, err)
	err = c.deleteJobs("consul", "default")
	require.NoError(t, err)
	jobs, err := c.kubernetes.BatchV1().Jobs("default").List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, jobs.Items, 0)
}

func TestDeleteClusterRoles(t *testing.T) {
	c := getInitializedCommand(t)
	c.kubernetes = fake.NewSimpleClientset()
	clusterrole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consul-test-clusterrole1",
			Labels: map[string]string{
				"release": "consul",
			},
		},
	}
	clusterrole2 := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consul-test-clusterrole2",
			Labels: map[string]string{
				"release": "consul",
			},
		},
	}
	_, err := c.kubernetes.RbacV1().ClusterRoles().Create(context.Background(), clusterrole, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.kubernetes.RbacV1().ClusterRoles().Create(context.Background(), clusterrole2, metav1.CreateOptions{})
	require.NoError(t, err)
	err = c.deleteClusterRoles("consul")
	require.NoError(t, err)
	clusterroles, err := c.kubernetes.RbacV1().ClusterRoles().List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, clusterroles.Items, 0)
}

func TestDeleteClusterRoleBindings(t *testing.T) {
	c := getInitializedCommand(t)
	c.kubernetes = fake.NewSimpleClientset()
	clusterrolebinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consul-test-clusterrolebinding1",
			Labels: map[string]string{
				"release": "consul",
			},
		},
	}
	clusterrolebinding2 := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consul-test-clusterrolebinding2",
			Labels: map[string]string{
				"release": "consul",
			},
		},
	}
	_, err := c.kubernetes.RbacV1().ClusterRoleBindings().Create(context.Background(), clusterrolebinding, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.kubernetes.RbacV1().ClusterRoleBindings().Create(context.Background(), clusterrolebinding2, metav1.CreateOptions{})
	require.NoError(t, err)
	err = c.deleteClusterRoleBindings("consul")
	require.NoError(t, err)
	clusterrolebindings, err := c.kubernetes.RbacV1().ClusterRoleBindings().List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, clusterrolebindings.Items, 0)
}

// getInitializedCommand sets up a command struct for tests.
func getInitializedCommand(t *testing.T) *Command {
	t.Helper()
	log := hclog.New(&hclog.LoggerOptions{
		Name:   "cli",
		Level:  hclog.Info,
		Output: os.Stdout,
	})

	baseCommand := &common.BaseCommand{
		Log: log,
	}

	c := &Command{
		BaseCommand: baseCommand,
	}
	c.init()
	return c
}
