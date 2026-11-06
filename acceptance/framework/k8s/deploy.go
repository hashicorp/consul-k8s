// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package k8s

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/util/yaml"

	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
)

func MarkAcceptanceNamespace(t *testing.T, options *k8s.KubectlOptions, cfg *config.TestConfig, namespace string) {
	t.Helper()
	MarkAcceptanceResource(t, options, cfg, "namespace", namespace)
}

func MarkAcceptanceResource(t *testing.T, options *k8s.KubectlOptions, cfg *config.TestConfig, resource, name string) {
	t.Helper()
	if cfg.EnableOpenshift || cfg.UseOpenshift {
		RunKubectl(t, options, "label", resource, name, "consul.hashicorp.com/acceptance-namespace="+options.Namespace, "--overwrite")
	}
}

func ApplyOpenShiftCNINetworkAttachment(t *testing.T, options *k8s.KubectlOptions, cfg *config.TestConfig) {
	t.Helper()
	if !(cfg.EnableOpenshift || cfg.UseOpenshift) || !cfg.EnableCNI {
		return
	}
	output, err := RunKubectlAndGetOutputE(t, options, "get", "network-attachment-definitions.k8s.cni.cncf.io", "consul-cni", "--ignore-not-found", "-o", "name")
	require.NoError(t, err)
	if strings.TrimSpace(output) != "" {
		return
	}
	KubectlApply(t, options, "../fixtures/bases/openshift/")
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		RunKubectl(t, options, "delete", "--ignore-not-found", "-f", "../fixtures/bases/openshift/")
	})
}

// GrantOpenShiftPrivilegedSCC binds the privileged SCC to service accounts in
// options.Namespace on OpenShift, like the privileged-scc-rolebinding.yaml that
// the shared static-server and static-client bases include. Fixtures whose
// workloads use their own service account need it because otherwise the pods
// only get the restricted-v2 SCC, which rejects their fixed user ID and the
// injected transparent proxy init container.
func GrantOpenShiftPrivilegedSCC(t *testing.T, options *k8s.KubectlOptions, cfg *config.TestConfig, serviceAccounts ...string) {
	t.Helper()
	if !cfg.EnableOpenshift && !cfg.UseOpenshift {
		return
	}
	require.NotEmpty(t, options.Namespace, "kubectl options must set the namespace of the service accounts")
	for _, serviceAccount := range serviceAccounts {
		name := serviceAccount + "-openshift-privileged"
		output, err := RunKubectlAndGetOutputE(t, options, "get", "rolebinding", name, "--ignore-not-found", "-o", "name")
		require.NoError(t, err, output)
		if strings.TrimSpace(output) == "" {
			RunKubectl(t, options, "create", "rolebinding", name,
				"--clusterrole=system:openshift:scc:privileged",
				"--serviceaccount="+options.Namespace+":"+serviceAccount)
		}
		helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
			RunKubectl(t, options, "delete", "rolebinding", name, "--ignore-not-found")
		})
	}
}

// DeployStaticWorkload deploys a static-server or static-client kustomize fixture
// like DeployKustomize. On OpenShift it deploys the fixture's OpenShift variant,
// which lets the restricted SCC assign the user ID, and with CNI attaches the
// pods to the consul-cni network.
func DeployStaticWorkload(t *testing.T, options *k8s.KubectlOptions, cfg *config.TestConfig, kustomizeDir string) {
	t.Helper()
	if cfg.EnableOpenshift || cfg.UseOpenshift {
		ApplyOpenShiftCNINetworkAttachment(t, options, cfg)
		kustomizeDir = openShiftStaticWorkloadFixture(kustomizeDir, cfg.EnableCNI)
	}
	DeployKustomize(t, options, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, kustomizeDir)
}

func openShiftStaticWorkloadFixture(kustomizeDir string, cni bool) string {
	switch kustomizeDir {
	case "../fixtures/cases/static-server-inject":
		kustomizeDir = "../fixtures/cases/static-server-openshift"
	case "../fixtures/cases/static-client-inject":
		kustomizeDir = "../fixtures/cases/static-client-openshift-inject"
	case "../fixtures/cases/static-client-tproxy":
		kustomizeDir = "../fixtures/cases/static-client-openshift-tproxy"
	case "../fixtures/bases/static-server-tcp":
		if cni {
			return "../fixtures/cases/static-server-tcp-openshift-cni"
		}
		return kustomizeDir
	default:
		return kustomizeDir
	}
	if cni {
		kustomizeDir += "-cni"
	}
	return kustomizeDir
}

// Deploy creates a Kubernetes deployment by applying configuration stored at filepath,
// sets up a cleanup function and waits for the deployment to become available.
func Deploy(t *testing.T, options *k8s.KubectlOptions, noCleanupOnFailure bool, noCleanup bool, debugDirectory string, filepath string) {
	t.Helper()

	KubectlApply(t, options, filepath)

	file, err := os.Open(filepath)
	require.NoError(t, err)

	deployment := v1.Deployment{}
	err = yaml.NewYAMLOrJSONDecoder(file, 1024).Decode(&deployment)
	require.NoError(t, err)

	helpers.Cleanup(t, noCleanupOnFailure, noCleanup, func() {
		// Note: this delete command won't wait for pods to be fully terminated.
		// This shouldn't cause any test pollution because the underlying
		// objects are deployments, and so when other tests create these
		// they should have different pod names.
		WritePodsDebugInfoIfFailed(t, options, debugDirectory, labelMapToString(deployment.GetLabels()))
		KubectlDelete(t, options, filepath)
	})

	RunKubectl(t, options, "wait", "--for=condition=available", fmt.Sprintf("deploy/%s", deployment.Name))
}

// DeployKustomize creates a Kubernetes deployment by applying the kustomize directory stored at kustomizeDir,
// sets up a cleanup function and waits for the deployment to become available.
func DeployKustomize(t *testing.T, options *k8s.KubectlOptions, noCleanupOnFailure bool, noCleanup bool, debugDirectory string, kustomizeDir string) {
	t.Helper()

	KubectlApplyK(t, options, kustomizeDir)

	output, err := RunKubectlAndGetOutputE(t, options, "kustomize", kustomizeDir)
	require.NoError(t, err)

	deployment := v1.Deployment{}
	err = yaml.NewYAMLOrJSONDecoder(strings.NewReader(output), 1024).Decode(&deployment)
	require.NoError(t, err)

	helpers.Cleanup(t, noCleanupOnFailure, noCleanup, func() {
		// Note: this delete command won't wait for pods to be fully terminated.
		// This shouldn't cause any test pollution because the underlying
		// objects are deployments, and so when other tests create these
		// they should have different pod names.
		WritePodsDebugInfoIfFailed(t, options, debugDirectory, labelMapToString(deployment.GetLabels()))
		KubectlDeleteK(t, options, kustomizeDir)
	})

	// The timeout to allow for connect-init to wait for services to be registered by the endpoints controller.
	RunKubectl(t, options, "wait", "--for=condition=available", "--timeout=5m", fmt.Sprintf("deploy/%s", deployment.Name))
}

func DeployJob(t *testing.T, options *k8s.KubectlOptions, noCleanupOnFailure bool, noCleanup bool, debugDirectory, kustomizeDir string) {
	t.Helper()

	KubectlApplyK(t, options, kustomizeDir)

	output, err := RunKubectlAndGetOutputE(t, options, "kustomize", kustomizeDir)
	require.NoError(t, err)

	job := batchv1.Job{}
	err = yaml.NewYAMLOrJSONDecoder(strings.NewReader(output), 1024).Decode(&job)
	require.NoError(t, err)

	helpers.Cleanup(t, noCleanupOnFailure, noCleanup, func() {
		// Note: this delete command won't wait for pods to be fully terminated.
		// This shouldn't cause any test pollution because the underlying
		// objects are deployments, and so when other tests create these
		// they should have different pod names.
		WritePodsDebugInfoIfFailed(t, options, debugDirectory, labelMapToString(job.GetLabels()))
		KubectlDeleteK(t, options, kustomizeDir)
	})
	logger.Log(t, "job deployed")

	// Because Jobs don't have a "started" condition, we have to check the status of the Pods they create.
	RunKubectl(t, options, "wait", "--for=condition=Ready", "--timeout=5m", "pods", "--selector", fmt.Sprintf("job-name=%s", job.Name))
}

// CheckStaticServerConnection execs into a pod of sourceApp
// and runs a curl command with the provided curlArgs.
// This function assumes that the connection is made to the static-server and expects the output
// to be "hello world" by default, or expectedSuccessOutput in a case of success.
// If expectSuccess is true, it will expect connection to succeed,
// otherwise it will expect failure due to intentions.
func CheckStaticServerConnection(t *testing.T, options *k8s.KubectlOptions, sourceApp string, expectSuccess bool, failureMessages []string, expectedSuccessOutput string, curlArgs ...string) {
	t.Helper()

	CheckStaticServerConnectionMultipleFailureMessages(t, options, sourceApp, expectSuccess, failureMessages, expectedSuccessOutput, curlArgs...)
}

// CheckStaticServerConnectionMultipleFailureMessages execs into a pod of sourceApp
// and runs a curl command with the provided curlArgs.
// This function assumes that the connection is made to the static-server and expects the output
// to be "hello world" by default, or expectedSuccessOutput in a case of success.
// If expectSuccess is true, it will expect connection to succeed,
// otherwise it will expect failure due to intentions. If multiple failureMessages are provided it will assert
// on the existence of any of them.
func CheckStaticServerConnectionMultipleFailureMessages(t *testing.T, options *k8s.KubectlOptions, sourceApp string, expectSuccess bool, failureMessages []string, expectedSuccessOutput string, curlArgs ...string) {
	t.Helper()
	resourceType := "deploy/"
	if sourceApp == "job-client" {
		resourceType = "jobs/"
	}
	expectedOutput := "hello world"
	if expectedSuccessOutput != "" {
		expectedOutput = expectedSuccessOutput
	}

	retrier := &retry.Timer{Timeout: 3 * time.Minute, Wait: 2 * time.Second}

	args := []string{"exec", resourceType + sourceApp, "-c", sourceApp, "--", "curl", "--connect-timeout", "5", "-vvvsSf"}
	args = append(args, curlArgs...)

	retry.RunWith(retrier, t, func(r *retry.R) {
		output, err := RunKubectlAndGetOutputE(r, options, args...)
		if expectSuccess {
			require.NoError(r, err)
			require.Contains(r, output, expectedOutput)
		} else {
			require.Error(r, err)
			require.Conditionf(r, func() bool {
				exists := false
				for _, msg := range failureMessages {
					if strings.Contains(output, msg) {
						exists = true
					}
				}
				return exists
			}, "expected failure messages %q but got %q", failureMessages, output)
		}
	})
}

// CheckStaticServerConnectionSuccessfulWithMessage is just like CheckStaticServerConnectionSuccessful
// but it asserts on a non-default expected message.
func CheckStaticServerConnectionSuccessfulWithMessage(t *testing.T, options *k8s.KubectlOptions, sourceApp string, message string, curlArgs ...string) {
	t.Helper()
	start := time.Now()
	CheckStaticServerConnectionMultipleFailureMessages(t, options, sourceApp, true, nil, message, curlArgs...)
	logger.Logf(t, "Took %s to check if static server connection was successful", time.Since(start))
}

// CheckStaticServerConnectionSuccessful is just like CheckStaticServerConnection
// but it always expects a successful connection.
func CheckStaticServerConnectionSuccessful(t *testing.T, sourceAppOpts *k8s.KubectlOptions, sourceApp string, curlArgs ...string) {
	t.Helper()
	start := time.Now()
	CheckStaticServerConnection(t, sourceAppOpts, sourceApp, true, nil, "", curlArgs...)
	logger.Logf(t, "Took %s to check if static server connection was successful", time.Since(start))
}

// CheckStaticServerConnectionFailing is just like CheckStaticServerConnection
// but it always expects a failing connection with various errors.
func CheckStaticServerConnectionFailing(t *testing.T, options *k8s.KubectlOptions, sourceApp string, curlArgs ...string) {
	t.Helper()
	CheckStaticServerConnection(t, options, sourceApp, false, []string{
		"curl: (52) Empty reply from server",
		"curl: (7) Failed to connect",
		"curl: (56) Recv failure: Connection reset by peer",
	}, "", curlArgs...)
}

// CheckStaticServerHTTPConnectionFailing is just like CheckStaticServerConnectionFailing
// except with HTTP-based intentions.
func CheckStaticServerHTTPConnectionFailing(t *testing.T, options *k8s.KubectlOptions, sourceApp string, curlArgs ...string) {
	t.Helper()
	CheckStaticServerConnection(t, options, sourceApp, false, []string{
		"curl: (22) The requested URL returned error: 403",
	}, "", curlArgs...)
}

// labelMapToString takes a label map[string]string
// and returns the string-ified version of, e.g app=foo,env=dev.
func labelMapToString(labelMap map[string]string) string {
	var labels []string
	for k, v := range labelMap {
		labels = append(labels, fmt.Sprintf("%s=%s", k, v))
	}

	return strings.Join(labels, ",")
}
