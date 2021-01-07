package k8s

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/helpers"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/logger"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// Deploy creates a Kubernetes deployment by applying configuration stored at filepath,
// sets up a cleanup function and waits for the deployment to become available.
func Deploy(t *testing.T, options *k8s.KubectlOptions, noCleanupOnFailure bool, debugDirectory string, filepath string) {
	t.Helper()

	KubectlApply(t, options, filepath)

	file, err := os.Open(filepath)
	require.NoError(t, err)

	deployment := v1.Deployment{}
	err = yaml.NewYAMLOrJSONDecoder(file, 1024).Decode(&deployment)
	require.NoError(t, err)

	helpers.Cleanup(t, noCleanupOnFailure, func() {
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
func DeployKustomize(t *testing.T, options *k8s.KubectlOptions, noCleanupOnFailure bool, debugDirectory string, kustomizeDir string) {
	t.Helper()

	KubectlApplyK(t, options, kustomizeDir)

	output, err := RunKubectlAndGetOutputE(t, options, "kustomize", kustomizeDir)
	require.NoError(t, err)

	deployment := v1.Deployment{}
	err = yaml.NewYAMLOrJSONDecoder(strings.NewReader(output), 1024).Decode(&deployment)
	require.NoError(t, err)

	helpers.Cleanup(t, noCleanupOnFailure, func() {
		// Note: this delete command won't wait for pods to be fully terminated.
		// This shouldn't cause any test pollution because the underlying
		// objects are deployments, and so when other tests create these
		// they should have different pod names.
		WritePodsDebugInfoIfFailed(t, options, debugDirectory, labelMapToString(deployment.GetLabels()))
		KubectlDeleteK(t, options, kustomizeDir)
	})

	RunKubectl(t, options, "wait", "--for=condition=available", "--timeout=1m", fmt.Sprintf("deploy/%s", deployment.Name))
}

// CheckStaticServerConnection execs into a pod of the deployment given by deploymentName
// and runs a curl command with the provided curlArgs.
// This function assumes that the connection is made to the static-server and expects the output
// to be "hello world" in a case of success.
// If expectSuccess is true, it will expect connection to succeed,
// otherwise it will expect failure due to intentions.
func CheckStaticServerConnection(
	t *testing.T,
	options *k8s.KubectlOptions,
	expectSuccess bool,
	deploymentName string,
	failureMessage string,
	curlArgs ...string,
) {
	t.Helper()

	CheckStaticServerConnectionMultipleFailureMessages(t, options, expectSuccess, deploymentName, []string{failureMessage}, curlArgs...)
}

// CheckStaticServerConnectionMultipleFailureMessages execs into a pod of the deployment given by deploymentName
// and runs a curl command with the provided curlArgs.
// This function assumes that the connection is made to the static-server and expects the output
// to be "hello world" in a case of success.
// If expectSuccess is true, it will expect connection to succeed,
// otherwise it will expect failure due to intentions. If multiple failureMessages are provided it will assert
// on the existence of any of them.
func CheckStaticServerConnectionMultipleFailureMessages(
	t *testing.T,
	options *k8s.KubectlOptions,
	expectSuccess bool,
	deploymentName string,
	failureMessages []string,
	curlArgs ...string,
) {
	t.Helper()

	retrier := &retry.Timer{Timeout: 80 * time.Second, Wait: 2 * time.Second}

	args := []string{"exec", "deploy/" + deploymentName, "-c", deploymentName, "--", "curl", "-vvvsSf"}
	args = append(args, curlArgs...)

	retry.RunWith(retrier, t, func(r *retry.R) {
		output, err := RunKubectlAndGetOutputE(t, options, args...)
		if expectSuccess {
			require.NoError(r, err)
			require.Contains(r, output, "hello world")
		} else {
			require.Error(r, err)
			require.Condition(r, func() bool {
				exists := false
				for _, msg := range failureMessages {
					if strings.Contains(output, msg) {
						exists = true
					}
				}
				return exists
			})
		}
	})
}

// CheckStaticServerConnectionSuccessful is just like CheckStaticServerConnection
// but it always expects a successful connection.
func CheckStaticServerConnectionSuccessful(t *testing.T, options *k8s.KubectlOptions, deploymentName string, curlArgs ...string) {
	start := time.Now()
	CheckStaticServerConnection(t, options, true, deploymentName, "", curlArgs...)
	logger.Logf(t, "Took %s to check if static server connection was successful", time.Since(start))
}

// CheckStaticServerConnectionSuccessful is just like CheckStaticServerConnection
// but it always expects a failing connection with error "Empty reply from server."
func CheckStaticServerConnectionFailing(t *testing.T, options *k8s.KubectlOptions, deploymentName string, curlArgs ...string) {
	CheckStaticServerConnection(t, options, false, deploymentName, "curl: (52) Empty reply from server", curlArgs...)
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
