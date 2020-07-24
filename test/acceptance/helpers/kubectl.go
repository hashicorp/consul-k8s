package helpers

import (
	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/gruntwork-io/terratest/modules/logger"
	"github.com/gruntwork-io/terratest/modules/shell"
	"github.com/gruntwork-io/terratest/modules/testing"
	"github.com/stretchr/testify/require"
)

// Functions included in this file already exist in terratest's k8s library, however,
// we're re-implementing them because we don't want to use their default logger
// as it logs everything regardless of verbosity level set via go test -v flags.

// RunKubectlAndGetOutputE runs an arbitrary kubectl command provided via args
// and returns its output and error.
func RunKubectlAndGetOutputE(t testing.TestingT, options *k8s.KubectlOptions, args ...string) (string, error) {
	var cmdArgs []string
	if options.ContextName != "" {
		cmdArgs = append(cmdArgs, "--context", options.ContextName)
	}
	if options.ConfigPath != "" {
		cmdArgs = append(cmdArgs, "--kubeconfig", options.ConfigPath)
	}
	if options.Namespace != "" {
		cmdArgs = append(cmdArgs, "--namespace", options.Namespace)
	}
	cmdArgs = append(cmdArgs, args...)
	command := shell.Command{
		Command: "kubectl",
		Args:    cmdArgs,
		Env:     options.Env,
		Logger: logger.TestingT,
	}
	return shell.RunCommandAndGetOutputE(t, command)
}

// KubectlApply takes a path to a Kubernetes YAML file and
// applies it to the cluster by running 'kubectl apply -f'.
// If there's an error applying the file, fail the test.
func KubectlApply(t testing.TestingT, options *k8s.KubectlOptions, configPath string) {
	_, err := RunKubectlAndGetOutputE(t, options, "apply", "-f", configPath)
	require.NoError(t, err)
}

// KubectlDelete takes a path to a Kubernetes YAML file and
// deletes it from the cluster by running 'kubectl delete -f'.
// If there's an error deleting the file, fail the test.
func KubectlDelete(t testing.TestingT, options *k8s.KubectlOptions, configPath string) {
	_, err := RunKubectlAndGetOutputE(t, options, "delete", "-f", configPath)
	require.NoError(t, err)
}

// RunKubectl runs an arbitrary kubectl command provided via args and ignores the output.
// If there's an error running the command, fail the test.
func RunKubectl(t testing.TestingT, options *k8s.KubectlOptions, args ...string) {
	_, err := RunKubectlAndGetOutputE(t, options, args...)
	require.NoError(t, err)
}
