// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package k8s

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/k8s"
	terratestLogger "github.com/gruntwork-io/terratest/modules/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
)

const (
	kubectlTimeout = "--timeout=120s"
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
func RunKubectlAndGetOutputE(t testutil.TestingTB, options *k8s.KubectlOptions, args ...string) (string, error) {
	t.Error("RunKubectlAndGetOutputE Running kubectl ", strings.Join(args, " "))
	return RunKubectlAndGetOutputWithLoggerE(t, options, terratestLogger.New(logger.TestLogger{}), args...)
}

// RunKubectlAndGetOutputWithLoggerE is the same as RunKubectlAndGetOutputE but
// it also allows you to provide a custom logger. This is useful if the command output
// contains sensitive information, for example, when you can pass logger.Discard.
func RunKubectlAndGetOutputWithLoggerE(t testutil.TestingTB, options *k8s.KubectlOptions, logger *terratestLogger.Logger, args ...string) (string, error) {
	t.Error("RunKubectlAndGetOutputWithLoggerE Running kubectl ", strings.Join(args, " "))

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
	command := helpers.Command{
		Command: "kubectl",
		Args:    cmdArgs,
		Env:     options.Env,
		Logger:  logger,
	}

	counter := &retry.Counter{
		Count: 10,
		Wait:  1 * time.Second,
	}
	var output string
	var err error
	retry.RunWith(counter, t, func(r *retry.R) {
		t.Error("RunWith Running command: ", command.Command, strings.Join(command.Args, " "))
		output, err = helpers.RunCommand(r, options, command)
		if err != nil {
			// Want to retry on errors connecting to actual Kube API because
			// these are intermittent.
			for _, connectionErr := range kubeAPIConnectErrs {
				if strings.Contains(err.Error(), connectionErr) {
					r.Errorf("%v", err.Error())
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
	_, err := RunKubectlAndGetOutputE(t, options, "delete", kubectlTimeout, "-f", configPath)
	require.NoError(t, err)
}

// KubectlDeleteK takes a path to a kustomize directory and
// deletes it from the cluster by running 'kubectl delete -k'.
// If there's an error deleting the file, fail the test.
func KubectlDeleteK(t *testing.T, options *k8s.KubectlOptions, kustomizeDir string) {
	// Ignore not found errors because Kubernetes automatically cleans up the kube secrets that we deployed
	// referencing the ServiceAccount when it is deleted.
	_, err := RunKubectlAndGetOutputE(t, options, "delete", kubectlTimeout, "--ignore-not-found", "-k", kustomizeDir)
	require.NoError(t, err)
}

// KubectlScale takes a deployment and scales it to the provided number of replicas.
func KubectlScale(t *testing.T, options *k8s.KubectlOptions, deployment string, replicas int) {
	_, err := RunKubectlAndGetOutputE(t, options, "scale", kubectlTimeout, fmt.Sprintf("--replicas=%d", replicas), deployment)
	require.NoError(t, err)
}

// KubectlLabel takes an object and applies the given label to it.
// Example: `KubectlLabel(t, options, "node", nodeId, corev1.LabelTopologyRegion, "us-east-1")`.
func KubectlLabel(t *testing.T, options *k8s.KubectlOptions, objectType string, objectId string, key string, value string) {
	// `kubectl label` doesn't support timeouts
	_, err := RunKubectlAndGetOutputE(t, options, "label", objectType, objectId, "--overwrite", fmt.Sprintf("%s=%s", key, value))
	require.NoError(t, err)
}

// RunKubectl runs an arbitrary kubectl command provided via args and ignores the output.
// If there's an error running the command, fail the test.
func RunKubectl(t *testing.T, options *k8s.KubectlOptions, args ...string) {
	_, err := RunKubectlAndGetOutputE(t, options, args...)
	if err != nil {
		t.Errorf("Error running kubectl %v: %v", args, err)
	}
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
