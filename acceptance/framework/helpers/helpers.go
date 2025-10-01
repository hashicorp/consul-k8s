// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package helpers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/helm"
	"github.com/gruntwork-io/terratest/modules/k8s"
	terratestLogger "github.com/gruntwork-io/terratest/modules/logger"
	"github.com/gruntwork-io/terratest/modules/random"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// RandomName generates a random string with a 'test-' prefix.
func RandomName() string {
	return fmt.Sprintf("test-%s", strings.ToLower(random.UniqueId()))
}

// CheckForPriorInstallations checks if there is an existing Helm release
// for this Helm chart already installed. If there is, it fails the tests.
func CheckForPriorInstallations(t *testing.T, client kubernetes.Interface, options *helm.Options, chartName, labelSelector string) {
	t.Helper()

	var helmListOutput string
	// Check if there's an existing cluster and fail if there is one.
	// We may need to retry since this is the first command run once the Kube
	// cluster is created and sometimes the API server returns errors.
	retry.RunWith(&retry.Counter{Wait: 2 * time.Second, Count: 15}, t, func(r *retry.R) {
		var err error
		// NOTE: It's okay to pass in `t` to RunHelmCommandAndGetOutputE despite being in a retry
		// because we're using RunHelmCommandAndGetOutputE (not RunHelmCommandAndGetOutput) so the `t` won't
		// get used to fail the test, just for logging.
		helmListOutput, err = helm.RunHelmCommandAndGetOutputE(r, options, "list", "--output", "json")
		require.NoError(r, err)
	})

	var installedReleases []map[string]string

	err := json.Unmarshal([]byte(helmListOutput), &installedReleases)
	require.NoError(t, err, "unmarshalling %q", helmListOutput)

	for _, r := range installedReleases {
		require.NotContains(t, r["chart"], chartName, fmt.Sprintf("detected an existing installation of %s %s, release name: %s", chartName, r["chart"], r["name"]))
	}

	// Wait for all pods in the "default" namespace to exit. A previous
	// release may not be listed by Helm but its pods may still be terminating.
	retry.RunWith(&retry.Counter{Wait: 2 * time.Second, Count: 60}, t, func(r *retry.R) {
		pods, err := client.CoreV1().Pods(options.KubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: labelSelector})
		require.NoError(r, err)
		if len(pods.Items) > 0 {
			var podNames []string
			for _, p := range pods.Items {
				podNames = append(podNames, p.Name)
			}
			r.Errorf("pods from previous installation still running: %s", strings.Join(podNames, ", "))
		}
	})
}

// SetupInterruptHandler sets up a goroutine that will wait for interrupt signals
// and call cleanup function when it catches it.
func SetupInterruptHandler(cleanup func()) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		fmt.Println("\r- Ctrl+C pressed in Terminal. Cleaning up resources.")
		cleanup()
		os.Exit(1)
	}()
}

// Cleanup will both register a cleanup function with t and SetupInterruptHandler to make sure resources
// get cleaned up if an interrupt signal is caught.
func Cleanup(t testutil.TestingTB, noCleanupOnFailure bool, noCleanup bool, cleanup func()) {
	t.Helper()

	// Always clean up when an interrupt signal is caught.
	SetupInterruptHandler(cleanup)

	// If noCleanupOnFailure is set, don't clean up resources if tests fail.
	// We need to wrap the cleanup function because t that is passed in to this function
	// might not have the information on whether the test has failed yet.
	wrappedCleanupFunc := func() {
		if !((noCleanupOnFailure && t.Failed()) || noCleanup) {
			logger.Logf(t, "cleaning up resources for %s", t.Name())
			cleanup()
		} else {
			logger.Log(t, "skipping resource cleanup")
		}
	}

	t.Cleanup(wrappedCleanupFunc)
}

// VerifyFederation checks that the WAN federation between servers is successful
// by first checking members are alive from the perspective of both servers.
// If secure is true, it will also check that the ACL replication is running on the secondary server.
func VerifyFederation(t *testing.T, primaryClient, secondaryClient *api.Client, releaseName string, secure bool) {
	retrier := &retry.Timer{Timeout: 5 * time.Minute, Wait: 1 * time.Second}
	start := time.Now()

	// Check that server in dc1 is healthy from the perspective of the server in dc2, and vice versa.
	// We're calling the Consul health API, as opposed to checking serf membership status,
	// because we need to make sure that the federated servers can make API calls and forward requests
	// from one server to another. From running tests in CI for a while and using serf membership status before,
	// we've noticed that the status could be "alive" as soon as the server in the secondary cluster joins the primary
	// and then switch to "failed". This would require us to check that the status is "alive" is showing consistently for
	// some amount of time, which could be quite flakey. Calling the API in another datacenter allows us to check that
	// each server can forward calls to another, which is what we need for connect.
	retry.RunWith(retrier, t, func(r *retry.R) {
		secondaryServerHealth, _, err := primaryClient.Health().Node(fmt.Sprintf("%s-consul-server-0", releaseName), &api.QueryOptions{Datacenter: "dc2"})
		require.NoError(r, err)
		require.Equal(r, secondaryServerHealth.AggregatedStatus(), api.HealthPassing)

		primaryServerHealth, _, err := secondaryClient.Health().Node(fmt.Sprintf("%s-consul-server-0", releaseName), &api.QueryOptions{Datacenter: "dc1"})
		require.NoError(r, err)
		require.Equal(r, primaryServerHealth.AggregatedStatus(), api.HealthPassing)

		if secure {
			replicationStatus, _, err := secondaryClient.ACL().Replication(nil)
			require.NoError(r, err)
			require.True(r, replicationStatus.Enabled)
			require.True(r, replicationStatus.Running)
		}
	})

	logger.Logf(t, "Took %s to verify federation", time.Since(start))
}

// MergeMaps will merge the values in b with values in a and save in a.
// If there are conflicts, the values in b will overwrite the values in a.
func MergeMaps(a, b map[string]string) {
	for k, v := range b {
		a[k] = v
	}
}

type K8sOptions struct {
	Options             *k8s.KubectlOptions
	NoCleanupOnFailure  bool
	NoCleanup           bool
	KustomizeConfigPath string
}

type ConsulOptions struct {
	ConsulClient                    *api.Client
	Namespace                       string
	ExternalServiceNameRegistration string
}

func RegisterExternalServiceCRD(t *testing.T, k8sOptions K8sOptions, consulOptions ConsulOptions) {
	t.Helper()
	t.Logf("Registering external service %s", k8sOptions.KustomizeConfigPath)

	if consulOptions.Namespace != "" && consulOptions.Namespace != "default" {
		logger.Logf(t, "creating the %s namespace in Consul", consulOptions.Namespace)
		_, _, err := consulOptions.ConsulClient.Namespaces().Create(&api.Namespace{
			Name: consulOptions.Namespace,
		}, nil)
		require.NoError(t, err)
	}

	// Register the external service
	k8s.KubectlApplyFromKustomize(t, k8sOptions.Options, k8sOptions.KustomizeConfigPath)
	Cleanup(t, k8sOptions.NoCleanupOnFailure, k8sOptions.NoCleanup, func() {
		k8s.KubectlDeleteFromKustomize(t, k8sOptions.Options, k8sOptions.KustomizeConfigPath)
	})

	CheckExternalServiceConditions(t, consulOptions.ExternalServiceNameRegistration, k8sOptions.Options)
}

func CheckExternalServiceConditions(t *testing.T, registrationName string, opts *k8s.KubectlOptions) {
	t.Helper()

	ogLogger := opts.Logger
	defer func() {
		opts.Logger = ogLogger
	}()

	opts.Logger = terratestLogger.Discard
	retry.RunWith(&retry.Counter{Wait: 2 * time.Second, Count: 15}, t, func(r *retry.R) {
		var err error
		out, err := k8s.RunKubectlAndGetOutputE(r, opts, "get", "-o=json", "registrations.consul.hashicorp.com", registrationName)
		require.NoError(r, err)
		reg := v1alpha1.Registration{}
		err = json.Unmarshal([]byte(out), &reg)
		require.NoError(r, err)
		require.NotEmpty(r, reg.Status.Conditions, "conditions should not be empty, retrying")
		// ensure all statuses are true which means that the registration is successful
		require.True(r, !slices.ContainsFunc(reg.Status.Conditions, func(c v1alpha1.Condition) bool { return c.Status == corev1.ConditionFalse }), "registration failed because of %v", reg.Status.Conditions)
	})
}

type Command struct {
	Command    string            // The command to run
	Args       []string          // The args to pass to the command
	WorkingDir string            // The working directory
	Env        map[string]string // Additional environment variables to set
	Logger     *terratestLogger.Logger
}

type cmdResult struct {
	output string
	err    error
}

func RunCommand(t testutil.TestingTB, options *k8s.KubectlOptions, command Command) (string, error) {
	t.Log("RunCommand 1")
	t.Helper()
	t.Log("RunCommand 2")

	resultCh := make(chan *cmdResult, 1)
	go func() {
		o, err := exec.Command("kubectl", "get", "ns").CombinedOutput()
		t.Logf("Current namespaces in the cluster: with error: %s \noutput:\n %s", err, string(o))
		o, err = exec.Command("kubectl", "get", "pods", "-n", "consul").CombinedOutput()
		t.Logf("Current pods in consul the cluster: with error: %s \noutput:\n %s", err, string(o))
		o, err = exec.Command("kubectl", "get", "pods", "-A").CombinedOutput()
		t.Logf("Current pods in the cluster: with error: %s \noutput:\n %s", err, string(o))
		output, err := exec.Command(command.Command, command.Args...).CombinedOutput()
		t.Log(
			"Executing command: ",
			command.Command,
			strings.Join(command.Args, " "),
			"with error:",
			err,
			" and output:",
			string(output),
		)
		resultCh <- &cmdResult{output: string(output), err: err}
	}()

	// might not be needed
	for _, arg := range command.Args {
		if strings.Contains(arg, "delete") {
			go func() {
				GetCRDRemoveFinalizers(t, options)
			}()
		}
	}

	select {
	case res := <-resultCh:
		if res.err != nil {
			logger.Logf(t, "Output: %v.", res.output)
		}
		o, err := exec.Command("kubectl", "get", "pods", "-A").CombinedOutput()
		t.Logf("Current pods in the cluster: with error: %s \noutput:\n %s", err, string(o))
		return res.output, res.err
		// Sometimes this func runs for too long handle timeout if needed.
	case <-time.After(320 * time.Second):
		GetCRDRemoveFinalizers(t, options)
		logger.Logf(t, "RunCommand timed out")
		return "", nil
	}
}

// getCRDRemoveFinalizers gets CRDs with finalizers and removes them.
func GetCRDRemoveFinalizers(t testutil.TestingTB, options *k8s.KubectlOptions) {
	t.Helper()
	crdNames, err := getCRDsWithFinalizers(options)
	if err != nil {
		logger.Logf(t, "Unable to get CRDs with finalizers, %v.", err)
	}

	if len(crdNames) > 0 {
		removeFinalizers(t, options, crdNames)
	}
}

// CRD struct to parse CRD JSON output.
type CRD struct {
	Items []struct {
		Metadata struct {
			Name       string   `json:"name"`
			Finalizers []string `json:"finalizers"`
		} `json:"metadata"`
	} `json:"items"`
}

func getCRDsWithFinalizers(options *k8s.KubectlOptions) ([]string, error) {
	cmdArgs := createCmdArgs(options)
	args := []string{"get", "crd", "-o=json"}

	cmdArgs = append(cmdArgs, args...)
	command := Command{
		Command: "kubectl",
		Args:    cmdArgs,
		Env:     options.Env,
	}

	output, err := exec.Command(command.Command, command.Args...).CombinedOutput()

	var crds CRD
	if err := json.Unmarshal(output, &crds); err != nil {
		return nil, fmt.Errorf("error parsing JSON: %v", err)
	}

	var crdNames []string
	for _, item := range crds.Items {
		if len(item.Metadata.Finalizers) > 0 {
			crdNames = append(crdNames, item.Metadata.Name)
		}
	}

	return crdNames, err
}

// removeFinalizers removes finalizers from CRDs.
func removeFinalizers(t testutil.TestingTB, options *k8s.KubectlOptions, crdNames []string) {
	cmdArgs := createCmdArgs(options)
	for _, crd := range crdNames {
		args := []string{"patch", "crd", crd, "--type=json", "-p=[{\"op\": \"remove\", \"path\": \"/metadata/finalizers\"}]"}

		cmdArgs = append(cmdArgs, args...)
		command := Command{
			Command: "kubectl",
			Args:    cmdArgs,
			Env:     options.Env,
		}

		_, err := exec.Command(command.Command, command.Args...).CombinedOutput()
		if err != nil {
			logger.Logf(t, "Unable to remove finalizers, proceeding anyway: %v.", err)
		}
		fmt.Printf("Finalizers removed from CRD %s\n", crd)
	}
}

func createCmdArgs(options *k8s.KubectlOptions) []string {
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
	return cmdArgs
}

const DEFAULT_PAUSE_PORT = "38501"

// WaitForInput starts a http server on a random port (which is output in the logs) and waits until you
// issue a request to that endpoint to continue the tests. This is useful for debugging tests that require
// inspecting the current state of a running cluster and you don't need to use long sleeps.
func WaitForInput(t *testing.T) {
	t.Helper()

	listenerPort := os.Getenv("CONSUL_K8S_TEST_PAUSE_PORT")

	if listenerPort == "" {
		listenerPort = DEFAULT_PAUSE_PORT
	}

	mux := http.NewServeMux()
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%s", listenerPort),
		Handler: mux,
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)

		_, err := w.Write([]byte("input received\n"))
		if err != nil {
			t.Logf("error writing body: %v", err)
			err = nil
		}

		err = r.Body.Close()
		if err != nil {
			t.Logf("error closing request body: %v", err)
			err = nil
		}

		t.Log("input received, continuing test")
		go func() {
			err = srv.Shutdown(context.Background())
			if err != nil {
				t.Logf("error closing listener: %v", err)
			}
		}()
	})

	t.Logf("Waiting for input on http://localhost:%s", listenerPort)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		t.Fatal(err)
	}
}
