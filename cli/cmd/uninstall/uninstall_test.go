// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package uninstall

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/hashicorp/consul-k8s/cli/common"
	cmnFlag "github.com/hashicorp/consul-k8s/cli/common/flag"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/hashicorp/consul-k8s/cli/helm"
	"github.com/hashicorp/go-hclog"
	"github.com/posener/complete"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/action"
	helmRelease "helm.sh/helm/v3/pkg/release"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiext "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apiextFake "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicFake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
)

var (
	serviceDefaultsGRV = schema.GroupVersionResource{
		Group:    "consul.hashicorp.com",
		Version:  "v1alpha1",
		Resource: "servicedefaults",
	}
	nonConsulGRV = schema.GroupVersionResource{
		Group:    "example.com",
		Version:  "v1",
		Resource: "examples",
	}
)

func TestDeletePVCs(t *testing.T) {
	c := getInitializedCommand(t, nil)
	c.k8sClient = fake.NewSimpleClientset()
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
	_, err := c.k8sClient.CoreV1().PersistentVolumeClaims("default").Create(context.Background(), pvc, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.k8sClient.CoreV1().PersistentVolumeClaims("default").Create(context.Background(), pvc2, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.k8sClient.CoreV1().PersistentVolumeClaims("default").Create(context.Background(), pvc3, metav1.CreateOptions{})
	require.NoError(t, err)
	err = c.deletePVCs("consul", "default")
	require.NoError(t, err)
	pvcs, err := c.k8sClient.CoreV1().PersistentVolumeClaims("default").List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, pvcs.Items, 1)
	require.Equal(t, pvcs.Items[0].Name, pvc3.Name)
}

func TestDeleteSecrets(t *testing.T) {
	c := getInitializedCommand(t, nil)
	c.k8sClient = fake.NewSimpleClientset()
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consul-test-secret1",
			Labels: map[string]string{
				"release":          "consul",
				common.CLILabelKey: common.CLILabelValue,
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
	secret3 := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "unrelated-test-secret3",
			Labels: map[string]string{
				"release": "unrelated",
			},
		},
	}
	_, err := c.k8sClient.CoreV1().Secrets("default").Create(context.Background(), secret, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.k8sClient.CoreV1().Secrets("default").Create(context.Background(), secret2, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.k8sClient.CoreV1().Secrets("default").Create(context.Background(), secret3, metav1.CreateOptions{})
	require.NoError(t, err)
	err = c.deleteSecrets("default")
	require.NoError(t, err)
	secrets, err := c.k8sClient.CoreV1().Secrets("default").List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)

	// Only secret1 should have been deleted, secret2 and secret 3 persist since it doesn't have the label.
	require.Len(t, secrets.Items, 2)
}

func TestDeleteServiceAccounts(t *testing.T) {
	c := getInitializedCommand(t, nil)
	c.k8sClient = fake.NewSimpleClientset()
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
	sa3 := &v1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consul-test-sa3",
			Labels: map[string]string{
				"release": "unrelated",
			},
		},
	}
	_, err := c.k8sClient.CoreV1().ServiceAccounts("default").Create(context.Background(), sa, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.k8sClient.CoreV1().ServiceAccounts("default").Create(context.Background(), sa2, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.k8sClient.CoreV1().ServiceAccounts("default").Create(context.Background(), sa3, metav1.CreateOptions{})
	require.NoError(t, err)
	err = c.deleteServiceAccounts("consul", "default")
	require.NoError(t, err)
	sas, err := c.k8sClient.CoreV1().ServiceAccounts("default").List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, sas.Items, 1)
	require.Equal(t, sas.Items[0].Name, sa3.Name)
}

func TestDeleteRoles(t *testing.T) {
	c := getInitializedCommand(t, nil)
	c.k8sClient = fake.NewSimpleClientset()
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
	role3 := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consul-test-role3",
			Labels: map[string]string{
				"release": "unrelated",
			},
		},
	}
	_, err := c.k8sClient.RbacV1().Roles("default").Create(context.Background(), role, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.k8sClient.RbacV1().Roles("default").Create(context.Background(), role2, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.k8sClient.RbacV1().Roles("default").Create(context.Background(), role3, metav1.CreateOptions{})
	require.NoError(t, err)
	err = c.deleteRoles("consul", "default")
	require.NoError(t, err)
	roles, err := c.k8sClient.RbacV1().Roles("default").List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, roles.Items, 1)
	require.Equal(t, roles.Items[0].Name, role3.Name)
}

func TestDeleteRoleBindings(t *testing.T) {
	c := getInitializedCommand(t, nil)
	c.k8sClient = fake.NewSimpleClientset()
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
	rolebinding3 := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consul-test-role3",
			Labels: map[string]string{
				"release": "unrelated",
			},
		},
	}
	_, err := c.k8sClient.RbacV1().RoleBindings("default").Create(context.Background(), rolebinding, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.k8sClient.RbacV1().RoleBindings("default").Create(context.Background(), rolebinding2, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.k8sClient.RbacV1().RoleBindings("default").Create(context.Background(), rolebinding3, metav1.CreateOptions{})
	require.NoError(t, err)
	err = c.deleteRoleBindings("consul", "default")
	require.NoError(t, err)
	rolebindings, err := c.k8sClient.RbacV1().RoleBindings("default").List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, rolebindings.Items, 1)
	require.Equal(t, rolebindings.Items[0].Name, rolebinding3.Name)
}

func TestDeleteJobs(t *testing.T) {
	c := getInitializedCommand(t, nil)
	c.k8sClient = fake.NewSimpleClientset()
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
	job3 := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consul-test-job3",
			Labels: map[string]string{
				"release": "unrelated",
			},
		},
	}
	_, err := c.k8sClient.BatchV1().Jobs("default").Create(context.Background(), job, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.k8sClient.BatchV1().Jobs("default").Create(context.Background(), job2, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.k8sClient.BatchV1().Jobs("default").Create(context.Background(), job3, metav1.CreateOptions{})
	require.NoError(t, err)
	err = c.deleteJobs("consul", "default")
	require.NoError(t, err)
	jobs, err := c.k8sClient.BatchV1().Jobs("default").List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, jobs.Items, 1)
	require.Equal(t, jobs.Items[0].Name, job3.Name)
}

func TestDeleteClusterRoles(t *testing.T) {
	c := getInitializedCommand(t, nil)
	c.k8sClient = fake.NewSimpleClientset()
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
	clusterrole3 := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consul-test-clusterrole3",
			Labels: map[string]string{
				"release": "unrelated",
			},
		},
	}
	_, err := c.k8sClient.RbacV1().ClusterRoles().Create(context.Background(), clusterrole, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.k8sClient.RbacV1().ClusterRoles().Create(context.Background(), clusterrole2, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.k8sClient.RbacV1().ClusterRoles().Create(context.Background(), clusterrole3, metav1.CreateOptions{})
	require.NoError(t, err)
	err = c.deleteClusterRoles("consul")
	require.NoError(t, err)
	clusterroles, err := c.k8sClient.RbacV1().ClusterRoles().List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, clusterroles.Items, 1)
	require.Equal(t, clusterroles.Items[0].Name, clusterrole3.Name)
}

func TestDeleteClusterRoleBindings(t *testing.T) {
	c := getInitializedCommand(t, nil)
	c.k8sClient = fake.NewSimpleClientset()
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
	clusterrolebinding3 := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consul-test-clusterrolebinding3",
			Labels: map[string]string{
				"release": "unrelated",
			},
		},
	}
	_, err := c.k8sClient.RbacV1().ClusterRoleBindings().Create(context.Background(), clusterrolebinding, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.k8sClient.RbacV1().ClusterRoleBindings().Create(context.Background(), clusterrolebinding2, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.k8sClient.RbacV1().ClusterRoleBindings().Create(context.Background(), clusterrolebinding3, metav1.CreateOptions{})
	require.NoError(t, err)
	err = c.deleteClusterRoleBindings("consul")
	require.NoError(t, err)
	clusterrolebindings, err := c.k8sClient.RbacV1().ClusterRoleBindings().List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, clusterrolebindings.Items, 1)
	require.Equal(t, clusterrolebindings.Items[0].Name, clusterrolebinding3.Name)
}

// getInitializedCommand sets up a command struct for tests.
func getInitializedCommand(t *testing.T, buf io.Writer) *Command {
	t.Helper()
	log := hclog.New(&hclog.LoggerOptions{
		Name:   "cli",
		Level:  hclog.Info,
		Output: os.Stdout,
	})
	var ui terminal.UI
	if buf != nil {
		ui = terminal.NewUI(context.Background(), buf)
	} else {
		ui = terminal.NewBasicUI(context.Background())
	}
	baseCommand := &common.BaseCommand{
		Log: log,
		UI:  ui,
	}

	c := &Command{
		BaseCommand: baseCommand,
	}
	c.init()
	return c
}

func TestTaskCreateCommand_AutocompleteFlags(t *testing.T) {
	t.Parallel()
	cmd := getInitializedCommand(t, nil)

	predictor := cmd.AutocompleteFlags()

	// Test that we get the expected number of predictions
	args := complete.Args{Last: "-"}
	res := predictor.Predict(args)

	// Grab the list of flags from the Flag object
	flags := make([]string, 0)
	cmd.set.VisitSets(func(name string, set *cmnFlag.Set) {
		set.VisitAll(func(flag *flag.Flag) {
			flags = append(flags, fmt.Sprintf("-%s", flag.Name))
		})
	})

	// Verify that there is a prediction for each flag associated with the command
	assert.Equal(t, len(flags), len(res))
	assert.ElementsMatch(t, flags, res, "flags and predictions didn't match, make sure to add "+
		"new flags to the command AutoCompleteFlags function")
}

func TestTaskCreateCommand_AutocompleteArgs(t *testing.T) {
	cmd := getInitializedCommand(t, nil)
	c := cmd.AutocompleteArgs()
	assert.Equal(t, complete.PredictNothing, c)
}

func TestFetchCustomResources(t *testing.T) {
	cr := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "consul.hashicorp.com/v1alpha1",
			"kind":       "ServiceDefaults",
			"metadata": map[string]interface{}{
				"name":      "server",
				"namespace": "default",
			},
		},
	}
	nonConsulCR1 := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "example.com/v1",
			"kind":       "Example",
			"metadata": map[string]interface{}{
				"name":      "example-resource",
				"namespace": "default",
			},
		},
	}
	nonConsulCR2 := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "example.com/v1",
			"kind":       "Example",
			"metadata": map[string]interface{}{
				"name":      "example-resource",
				"namespace": "other",
			},
		},
	}

	c := getInitializedCommand(t, nil)
	c.k8sClient = fake.NewSimpleClientset(&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "other"}})
	c.apiextK8sClient, c.dynamicK8sClient = createClientsWithCrds()

	_, err := c.dynamicK8sClient.Resource(serviceDefaultsGRV).Namespace("default").Create(context.Background(), &cr, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.dynamicK8sClient.Resource(nonConsulGRV).Namespace("default").Create(context.Background(), &nonConsulCR1, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.dynamicK8sClient.Resource(nonConsulGRV).Namespace("other").Create(context.Background(), &nonConsulCR2, metav1.CreateOptions{})
	require.NoError(t, err)

	crds, err := c.fetchCustomResourceDefinitions()
	require.NoError(t, err)

	actual, err := c.fetchCustomResources(crds)
	require.NoError(t, err)
	require.Len(t, actual, 1)
	require.Contains(t, actual, cr)
	require.NotContains(t, actual, nonConsulCR1)
	require.NotContains(t, actual, nonConsulCR2)
}

func TestDeleteCustomResources(t *testing.T) {
	cr := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "consul.hashicorp.com/v1alpha1",
			"kind":       "ServiceDefaults",
			"metadata": map[string]interface{}{
				"name":      "server",
				"namespace": "default",
			},
		},
	}

	c := getInitializedCommand(t, nil)
	c.apiextK8sClient, c.dynamicK8sClient = createClientsWithCrds()

	_, err := c.dynamicK8sClient.Resource(serviceDefaultsGRV).Namespace("default").Create(context.Background(), &cr, metav1.CreateOptions{})
	require.NoError(t, err)

	crds, err := c.fetchCustomResourceDefinitions()
	require.NoError(t, err)

	actual, err := c.fetchCustomResources(crds)
	require.NoError(t, err)
	require.Len(t, actual, 1)

	err = c.deleteCustomResources([]unstructured.Unstructured{cr}, mapCRKindToResourceName(crds), fakeUILogger)
	require.NoError(t, err)

	actual, err = c.fetchCustomResources(crds)
	require.NoError(t, err)
	require.Len(t, actual, 0)
}

func TestPatchCustomResources(t *testing.T) {
	cr := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "consul.hashicorp.com/v1alpha1",
			"kind":       "ServiceDefaults",
			"metadata": map[string]interface{}{
				"name":      "server",
				"namespace": "default",
			},
		},
	}
	cr.SetFinalizers([]string{"consul.hashicorp.com"})

	c := getInitializedCommand(t, nil)
	c.apiextK8sClient, c.dynamicK8sClient = createClientsWithCrds()

	_, err := c.dynamicK8sClient.Resource(serviceDefaultsGRV).Namespace("default").Create(context.Background(), &cr, metav1.CreateOptions{})
	require.NoError(t, err)

	crds, err := c.fetchCustomResourceDefinitions()
	require.NoError(t, err)

	err = c.patchCustomResources([]unstructured.Unstructured{cr}, mapCRKindToResourceName(crds), fakeUILogger)
	require.NoError(t, err)

	actual, err := c.fetchCustomResources(crds)
	require.NoError(t, err)
	require.Len(t, actual, 1)
	require.Len(t, actual[0].GetFinalizers(), 0)
}

func TestMapKindToResource(t *testing.T) {
	crds := apiextv1.CustomResourceDefinitionList{
		Items: []apiextv1.CustomResourceDefinition{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "servicedefaults.consul.hashicorp.com",
					Labels: map[string]string{
						"app": "consul",
					},
				},
				Spec: apiextv1.CustomResourceDefinitionSpec{
					Group: "consul.hashicorp.com",
					Names: apiextv1.CustomResourceDefinitionNames{
						Plural: "servicedefaults",
						Kind:   "ServiceDefaults",
					},
					Scope: "Namespaced",
					Versions: []apiextv1.CustomResourceDefinitionVersion{
						{
							Name: "v1alpha1",
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "examples.example.com",
				},
				Spec: apiextv1.CustomResourceDefinitionSpec{
					Group: "example.com",
					Names: apiextv1.CustomResourceDefinitionNames{
						Plural: "examples",
						Kind:   "Example",
					},
					Scope: "Namespaced",
					Versions: []apiextv1.CustomResourceDefinitionVersion{
						{
							Name: "v1",
						},
					},
				},
			},
		},
	}

	expected := map[string]string{
		"ServiceDefaults": "servicedefaults",
		"Example":         "examples",
	}

	actual := mapCRKindToResourceName(&crds)
	require.Equal(t, expected, actual)
}

func TestUninstall(t *testing.T) {
	cases := map[string]struct {
		input                                   []string
		messages                                []string
		helmActionsRunner                       *helm.MockActionRunner
		preProcessingFunc                       func()
		expectedReturnCode                      int
		expectCheckedForConsulInstallations     bool
		expectCheckedForConsulDemoInstallations bool
		expectConsulUninstalled                 bool
		expectConsulDemoUninstalled             bool
	}{
		"uninstall when consul installation exists returns success": {
			input: []string{},
			messages: []string{
				"\n==> Checking if Consul demo application can be uninstalled\n    No existing Consul demo application installation found.\n",
				"\n==> Checking if Consul can be uninstalled\n ✓ Existing Consul installation found.\n",
				"\n==> Consul Uninstall Summary\n    Name: consul\n    Namespace: consul\n --> Deleting custom resources managed by Consul\n --> Starting delete for \"server\" ServiceDefaults\n ✓ Successfully uninstalled Consul Helm release.\n ✓ Skipping deleting PVCs, secrets, and service accounts.\n",
			},
			helmActionsRunner: &helm.MockActionRunner{
				CheckForInstallationsFunc: func(options *helm.CheckForInstallationsOptions) (bool, string, string, error) {
					if options.ReleaseName == "consul" {
						return true, "consul", "consul", nil
					} else {
						return false, "", "", nil
					}
				},
			},
			expectedReturnCode:                      0,
			expectCheckedForConsulInstallations:     true,
			expectCheckedForConsulDemoInstallations: true,
			expectConsulUninstalled:                 true,
			expectConsulDemoUninstalled:             false,
		},
		"uninstall when consul installation does not exist returns error": {
			input: []string{},
			messages: []string{
				"\n==> Checking if Consul demo application can be uninstalled\n    No existing Consul demo application installation found.\n ! could not find Consul installation in cluster\n",
			},
			helmActionsRunner: &helm.MockActionRunner{
				CheckForInstallationsFunc: func(options *helm.CheckForInstallationsOptions) (bool, string, string, error) {
					if options.ReleaseName == "consul" {
						return false, "", "", nil
					} else {
						return false, "", "", nil
					}
				},
			},
			expectedReturnCode:                      1,
			expectCheckedForConsulInstallations:     true,
			expectCheckedForConsulDemoInstallations: true,
			expectConsulUninstalled:                 false,
			expectConsulDemoUninstalled:             false,
		},
		"uninstall with -wipe-data flag processes other resource and returns success": {
			input: []string{
				"-wipe-data",
			},
			messages: []string{
				"\n==> Checking if Consul demo application can be uninstalled\n    No existing Consul demo application installation found.\n",
				"\n==> Checking if Consul can be uninstalled\n ✓ Existing Consul installation found.\n",
				"\n==> Consul Uninstall Summary\n    Name: consul\n    Namespace: consul\n --> Deleting custom resources managed by Consul\n --> Starting delete for \"server\" ServiceDefaults\n ✓ Successfully uninstalled Consul Helm release.\n",
				"\n==> Other Consul Resources\n    Deleting data for installation: \n    Name: consul\n    Namespace consul\n ✓ No PVCs found.\n ✓ No Consul secrets found.\n ✓ No Consul service accounts found.\n ✓ No Consul roles found.\n ✓ No Consul rolebindings found.\n ✓ No Consul jobs found.\n ✓ No Consul cluster roles found.\n ✓ No Consul cluster role bindings found.\n",
			},
			helmActionsRunner: &helm.MockActionRunner{
				CheckForInstallationsFunc: func(options *helm.CheckForInstallationsOptions) (bool, string, string, error) {
					if options.ReleaseName == "consul" {
						return true, "consul", "consul", nil
					} else {
						return false, "", "", nil
					}
				},
			},
			expectedReturnCode:                      0,
			expectCheckedForConsulInstallations:     true,
			expectCheckedForConsulDemoInstallations: true,
			expectConsulUninstalled:                 true,
			expectConsulDemoUninstalled:             false,
		},
		"uninstall when both consul and consul demo installations exist returns success": {
			input: []string{},
			messages: []string{
				"\n==> Checking if Consul demo application can be uninstalled\n ✓ Existing Consul demo application installation found.\n",
				"\n==> Consul Demo Application Uninstall Summary\n    Name: consul-demo\n    Namespace: consul-demo\n ✓ Successfully uninstalled Consul demo application Helm release.\n",
				"\n==> Checking if Consul can be uninstalled\n ✓ Existing Consul installation found.\n",
				"\n==> Consul Uninstall Summary\n    Name: consul\n    Namespace: consul\n --> Deleting custom resources managed by Consul\n --> Starting delete for \"server\" ServiceDefaults\n ✓ Successfully uninstalled Consul Helm release.\n ✓ Skipping deleting PVCs, secrets, and service accounts.\n",
			},
			helmActionsRunner: &helm.MockActionRunner{
				CheckForInstallationsFunc: func(options *helm.CheckForInstallationsOptions) (bool, string, string, error) {
					if options.ReleaseName == "consul" {
						return true, "consul", "consul", nil
					} else {
						return true, "consul-demo", "consul-demo", nil
					}
				},
			},
			expectedReturnCode:                      0,
			expectCheckedForConsulInstallations:     true,
			expectCheckedForConsulDemoInstallations: true,
			expectConsulUninstalled:                 true,
			expectConsulDemoUninstalled:             true,
		},
		"uninstall when consul uninstall errors returns error": {
			input: []string{},
			messages: []string{
				"\n==> Checking if Consul demo application can be uninstalled\n    No existing Consul demo application installation found.\n",
				"\n==> Checking if Consul can be uninstalled\n ✓ Existing Consul installation found.\n",
				"\n==> Consul Uninstall Summary\n    Name: consul\n    Namespace: consul\n --> Deleting custom resources managed by Consul\n --> Starting delete for \"server\" ServiceDefaults\n ! Helm returned an error.\n",
			},
			helmActionsRunner: &helm.MockActionRunner{
				CheckForInstallationsFunc: func(options *helm.CheckForInstallationsOptions) (bool, string, string, error) {
					if options.ReleaseName == "consul" {
						return true, "consul", "consul", nil
					} else {
						return false, "", "", nil
					}
				},
				UninstallFunc: func(uninstall *action.Uninstall, name string) (*helmRelease.UninstallReleaseResponse, error) {
					return nil, errors.New("Helm returned an error.")
				},
			},
			expectedReturnCode:                      1,
			expectCheckedForConsulInstallations:     true,
			expectCheckedForConsulDemoInstallations: true,
			expectConsulUninstalled:                 false,
			expectConsulDemoUninstalled:             false,
		},
		"uninstall when consul demo is installed consul demo uninstall errors returns error": {
			input: []string{},
			messages: []string{
				"\n==> Checking if Consul demo application can be uninstalled\n ✓ Existing Consul demo application installation found.\n",
				"\n==> Consul Demo Application Uninstall Summary\n    Name: consul-demo\n    Namespace: consul-demo\n ! Helm returned an error.\n",
			},
			helmActionsRunner: &helm.MockActionRunner{
				CheckForInstallationsFunc: func(options *helm.CheckForInstallationsOptions) (bool, string, string, error) {
					if options.ReleaseName == "consul" {
						return true, "consul", "consul", nil
					} else {
						return true, "consul-demo", "consul-demo", nil
					}
				},
				UninstallFunc: func(uninstall *action.Uninstall, name string) (*helmRelease.UninstallReleaseResponse, error) {
					if name == "consul" {
						return &helmRelease.UninstallReleaseResponse{}, nil
					} else {
						return nil, errors.New("Helm returned an error.")
					}
				},
			},
			expectedReturnCode:                      1,
			expectCheckedForConsulInstallations:     true,
			expectCheckedForConsulDemoInstallations: true,
			expectConsulUninstalled:                 false,
			expectConsulDemoUninstalled:             false,
		},
	}

	cr := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "consul.hashicorp.com/v1alpha1",
			"kind":       "ServiceDefaults",
			"metadata": map[string]interface{}{
				"name":      "server",
				"namespace": "default",
			},
		},
	}
	cr.SetFinalizers([]string{"consul.hashicorp.com"})

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			buf := new(bytes.Buffer)
			c := getInitializedCommand(t, buf)

			c.k8sClient = fake.NewSimpleClientset()

			c.apiextK8sClient, c.dynamicK8sClient = createClientsWithCrds()
			_, err := c.dynamicK8sClient.Resource(serviceDefaultsGRV).Namespace("default").Create(context.Background(), &cr, metav1.CreateOptions{})
			require.NoError(t, err)

			mock := tc.helmActionsRunner
			c.helmActionsRunner = mock

			if tc.preProcessingFunc != nil {
				tc.preProcessingFunc()
			}
			input := append([]string{
				"--auto-approve",
			}, tc.input...)
			returnCode := c.Run(input)
			output := buf.String()
			require.Equal(t, tc.expectedReturnCode, returnCode, output)

			require.Equal(t, tc.expectCheckedForConsulInstallations, mock.CheckedForConsulInstallations)
			require.Equal(t, tc.expectCheckedForConsulDemoInstallations, mock.CheckedForConsulDemoInstallations)
			require.Equal(t, tc.expectConsulUninstalled, mock.ConsulUninstalled)
			require.Equal(t, tc.expectConsulDemoUninstalled, mock.ConsulDemoUninstalled)
			for _, msg := range tc.messages {
				require.Contains(t, output, msg)
			}

			if tc.expectConsulUninstalled {
				crds, err := c.fetchCustomResourceDefinitions()
				require.NoError(t, err)
				crs, err := c.fetchCustomResources(crds)
				require.NoError(t, err)
				require.Len(t, crs, 0)
			}
		})
	}
}

func createClientsWithCrds() (apiext.Interface, dynamic.Interface) {
	grvToListKind := map[schema.GroupVersionResource]string{
		serviceDefaultsGRV: "ServiceDefaultsList",
		nonConsulGRV:       "ExamplesList",
	}
	crds := apiextv1.CustomResourceDefinitionList{
		Items: []apiextv1.CustomResourceDefinition{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "servicedefaults.consul.hashicorp.com",
					Labels: map[string]string{
						"app": "consul",
					},
				},
				Spec: apiextv1.CustomResourceDefinitionSpec{
					Group: "consul.hashicorp.com",
					Names: apiextv1.CustomResourceDefinitionNames{
						Plural: "servicedefaults",
						Kind:   "ServiceDefaults",
					},
					Scope: "Namespaced",
					Versions: []apiextv1.CustomResourceDefinitionVersion{
						{
							Name: "v1alpha1",
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "examples.example.com",
				},
				Spec: apiextv1.CustomResourceDefinitionSpec{
					Group: "example.com",
					Names: apiextv1.CustomResourceDefinitionNames{
						Plural: "examples",
						Kind:   "Example",
					},
					Scope: "Namespaced",
					Versions: []apiextv1.CustomResourceDefinitionVersion{
						{
							Name: "v1",
						},
					},
				},
			},
		},
	}
	return apiextFake.NewSimpleClientset(&crds), dynamicFake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), grvToListKind)
}

func fakeUILogger(s string, i ...interface{}) {}
