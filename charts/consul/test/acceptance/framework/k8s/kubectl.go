package k8s

import (
	"strings"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/k8s"
	terratestLogger "github.com/gruntwork-io/terratest/modules/logger"
	"github.com/gruntwork-io/terratest/modules/shell"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/logger"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
)

// kubeAPIConnectErrs are errors that sometimes occur when talking to the
// Kubernetes API related to connection issues.
var kubeAPIConnectErrs = []string{
	"was refused - did you specify the right host or port?",
	"Unable to connect to the server",
}

// Functions included in this file already exist in terratest's k8s library, however,
// we're re-implementing them because we don't want to use their default logger
// as it logs everything regardless of verbosity level set via go test -v flags.

// RunKubectlAndGetOutputE runs an arbitrary kubectl command provided via args
// and returns its output and error.
func RunKubectlAndGetOutputE(t *testing.T, options *k8s.KubectlOptions, args ...string) (string, error) {
	return RunKubectlAndGetOutputWithLoggerE(t, options, terratestLogger.New(logger.TestLogger{}), args...)
}

// RunKubectlAndGetOutputWithLoggerE is the same as RunKubectlAndGetOutputE but
// it also allows you to provide a custom logger. This is useful if the command output
// contains sensitive information, for example, when you can pass logger.Discard.
func RunKubectlAndGetOutputWithLoggerE(t *testing.T, options *k8s.KubectlOptions, logger *terratestLogger.Logger, args ...string) (string, error) {
	var cmdArgs []string
	if options.ContextName != "" {
		cmdArgs = append(cmdArgs, "--context", options.ContextName)
	}
	if options.ConfigPath != "" {
		cmdArgs = append(cmdArgs, "--kubeconfig", options.ConfigPath)
	}
	if options.Namespace != "" && !sliceContains(args, "-n") && !sliceContains(args, "--namespace") {
		cmdArgs = append(cmdArgs, "--namespace", options.Namespace)
	}
	cmdArgs = append(cmdArgs, args...)
	command := shell.Command{
		Command: "kubectl",
		Args:    cmdArgs,
		Env:     options.Env,
		Logger:  logger,
	}

	counter := &retry.Counter{
		Count: 3,
		Wait:  1 * time.Second,
	}
	var output string
	var err error
	retry.RunWith(counter, t, func(r *retry.R) {
		output, err = shell.RunCommandAndGetOutputE(t, command)
		if err != nil {
			// Want to retry on errors connecting to actual Kube API because
			// these are intermittent.
			for _, connectionErr := range kubeAPIConnectErrs {
				if strings.Contains(err.Error(), connectionErr) {
					r.Errorf(err.Error())
					return
				}
			}
		}
	})
	return output, err
}

// KubectlApply takes a path to a Kubernetes YAML file and
// applies it to the cluster by running 'kubectl apply -f'.
// If there's an error applying the file, fail the test.
func KubectlApply(t *testing.T, options *k8s.KubectlOptions, configPath string) {
	_, err := RunKubectlAndGetOutputE(t, options, "apply", "-f", configPath)
	require.NoError(t, err)
}

// KubectlApplyK takes a path to a kustomize directory and
// applies it to the cluster by running 'kubectl apply -k'.
// If there's an error applying the file, fail the test.
func KubectlApplyK(t *testing.T, options *k8s.KubectlOptions, kustomizeDir string) {
	_, err := RunKubectlAndGetOutputE(t, options, "apply", "-k", kustomizeDir)
	require.NoError(t, err)
}

// KubectlDelete takes a path to a Kubernetes YAML file and
// deletes it from the cluster by running 'kubectl delete -f'.
// If there's an error deleting the file, fail the test.
func KubectlDelete(t *testing.T, options *k8s.KubectlOptions, configPath string) {
	_, err := RunKubectlAndGetOutputE(t, options, "delete", "-f", configPath)
	require.NoError(t, err)
}

// KubectlDeleteK takes a path to a kustomize directory and
// deletes it from the cluster by running 'kubectl delete -k'.
// If there's an error deleting the file, fail the test.
func KubectlDeleteK(t *testing.T, options *k8s.KubectlOptions, kustomizeDir string) {
	_, err := RunKubectlAndGetOutputE(t, options, "delete", "-k", kustomizeDir)
	require.NoError(t, err)
}

// RunKubectl runs an arbitrary kubectl command provided via args and ignores the output.
// If there's an error running the command, fail the test.
func RunKubectl(t *testing.T, options *k8s.KubectlOptions, args ...string) {
	_, err := RunKubectlAndGetOutputE(t, options, args...)
	require.NoError(t, err)
}

// sliceContains returns true if s contains target.
func sliceContains(s []string, target string) bool {
	for _, elem := range s {
		if elem == target {
			return true
		}
	}
	return false
}
