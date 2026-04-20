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
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

var CommonConsulCRDs = []string{
	"controlplanerequestlimits.consul.hashicorp.com",
	"customgatewayclasses.consul.hashicorp.com",
	"customgatewaypolicies.consul.hashicorp.com",
	"exportedservices.consul.hashicorp.com",
	"gatewayclassconfigs.consul.hashicorp.com",
	"gatewaypolicies.consul.hashicorp.com",
	"gateways.consul.hashicorp.com",
	"grpcroutes.consul.hashicorp.com",
	"httproutes.consul.hashicorp.com",
	"ingressgateways.consul.hashicorp.com",
	"jwtproviders.consul.hashicorp.com",
	"meshes.consul.hashicorp.com",
	"meshservices.consul.hashicorp.com",
	"peeringacceptors.consul.hashicorp.com",
	"peeringdialers.consul.hashicorp.com",
	"proxydefaults.consul.hashicorp.com",
	"referencegrants.consul.hashicorp.com",
	"registrations.consul.hashicorp.com",
	"routeauthfilters.consul.hashicorp.com",
	"routeretryfilters.consul.hashicorp.com",
	"routetimeoutfilters.consul.hashicorp.com",
	"samenessgroups.consul.hashicorp.com",
	"servicedefaults.consul.hashicorp.com",
	"serviceintentions.consul.hashicorp.com",
	"serviceresolvers.consul.hashicorp.com",
	"servicerouters.consul.hashicorp.com",
	"servicesplitters.consul.hashicorp.com",
	"tcproutes.consul.hashicorp.com",
	"terminatinggateways.consul.hashicorp.com",
	"tlsroutes.consul.hashicorp.com",
	"trafficpermissions.auth.consul.hashicorp.com",
	"udproutes.consul.hashicorp.com",
}

var CommonGatewayAPICRDs = []string{
	"gatewayclasses.gateway.networking.k8s.io",
	"gateways.gateway.networking.k8s.io",
	"httproutes.gateway.networking.k8s.io",
	"referencegrants.gateway.networking.k8s.io",
	"tcproutes.gateway.networking.k8s.io",
}

func OpenShiftCleanupCRDs(includeGatewayAPICRDs bool) []string {
	crds := append([]string{}, CommonConsulCRDs...)
	if includeGatewayAPICRDs {
		crds = append(crds, CommonGatewayAPICRDs...)
	}
	return crds
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
	t.Helper()

	resultCh := make(chan *cmdResult, 1)

	go func() {
		output, err := exec.Command(command.Command, command.Args...).CombinedOutput()
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

func GetCRDRemoveFinalizersForCRDNames(t testutil.TestingTB, options *k8s.KubectlOptions, crdCandidates []string) {
	t.Helper()
	crdNames, err := getCRDsWithFinalizersForCRDNames(options, crdCandidates)
	if err != nil {
		logger.Logf(t, "Unable to get CRDs with finalizers from the targeted list, %v.", err)
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
	if err != nil {
		return nil, fmt.Errorf("error running kubectl command: %v, output: %s", err, string(output))
	}

	var crds CRD
	if err := json.Unmarshal(output, &crds); err != nil {
		return nil, fmt.Errorf("error parsing JSON: %v, output: %s", err, string(output))
	}

	var crdNames []string
	for _, item := range crds.Items {
		if len(item.Metadata.Finalizers) > 0 {
			crdNames = append(crdNames, item.Metadata.Name)
		}
	}

	return crdNames, err
}

func getCRDsWithFinalizersForCRDNames(options *k8s.KubectlOptions, crdCandidates []string) ([]string, error) {
	if len(crdCandidates) == 0 {
		return nil, nil
	}

	cmdArgs := createCmdArgs(options)
	args := append([]string{"get", "crd"}, crdCandidates...)
	args = append(args, "-o=json", "--ignore-not-found=true")

	cmdArgs = append(cmdArgs, args...)
	command := Command{
		Command: "kubectl",
		Args:    cmdArgs,
		Env:     options.Env,
	}

	output, err := exec.Command(command.Command, command.Args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("error running kubectl command: %v, output: %s", err, string(output))
	}

	var crds CRD
	if err := json.Unmarshal(output, &crds); err != nil {
		return nil, fmt.Errorf("error parsing JSON: %v, output: %s", err, string(output))
	}

	var crdNames []string
	for _, item := range crds.Items {
		if len(item.Metadata.Finalizers) > 0 {
			crdNames = append(crdNames, item.Metadata.Name)
		}
	}

	return crdNames, nil
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
			continue
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

func WaitForGatewayClassConfigWithRetry(t *testing.T, kubectlOptions *k8s.KubectlOptions, configName, kustomizeDir string) {
	t.Helper()

	logger.Log(t, "waiting for gatewayclassconfig to be created")
	found := false
	maxAttempts := 3
	checksPerAttempt := 5

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		logger.Logf(t, "gatewayclassconfig existence check attempt %d/%d", attempt, maxAttempts)

		for i := range checksPerAttempt {
			_, err := k8s.RunKubectlAndGetOutputE(t, kubectlOptions, "get", "gatewayclassconfig", configName)
			if err == nil {
				found = true
				logger.Logf(t, "gatewayclassconfig %s found successfully", configName)
				break
			}
			logger.Logf(t, "gatewayclassconfig check %d/%d: %v", i+1, checksPerAttempt, err)
			time.Sleep(2 * time.Second)
		}

		if found {
			break
		}

		if attempt < maxAttempts {
			logger.Logf(t, "gatewayclassconfig not found after %d seconds, attempting delete/recreate (attempt %d/%d)", checksPerAttempt*2, attempt, maxAttempts)
			_, err := k8s.RunKubectlAndGetOutputE(t, kubectlOptions, "delete", "gatewayclassconfig", configName, "--ignore-not-found=true")
			if err != nil {
				logger.Logf(t, "warning: failed to delete gatewayclassconfig %s: %v", configName, err)
			}
			out, err := k8s.RunKubectlAndGetOutputE(t, kubectlOptions, "apply", "-k", kustomizeDir)
			require.NoError(t, err, out)
			time.Sleep(2 * time.Second)
		}
	}

	if !found {
		require.Failf(t, "gatewayclassconfig %s was not found after %d attempts with delete/recreate", configName, maxAttempts)
	}
}

func WaitForGatewayClassConfigWithClientRetry(t *testing.T, k8sClient client.Client, configName string) {
	t.Helper()

	WaitForResourceWithClientRetry(t, k8sClient, client.ObjectKey{Name: configName}, &v1alpha1.GatewayClassConfig{}, "gatewayclassconfig")
}

func WaitForResourceWithClientRetry(t *testing.T, k8sClient client.Client, objectKey client.ObjectKey, object client.Object, resourceKind string) {
	t.Helper()

	logger.Logf(t, "waiting for %s %s to be readable via controller-runtime client", resourceKind, objectKey.Name)
	retry.RunWith(&retry.Counter{Wait: 2 * time.Second, Count: 10}, t, func(r *retry.R) {
		err := k8sClient.Get(context.Background(), objectKey, object)
		if k8serrors.IsNotFound(err) {
			r.Errorf("%s %s not found yet", resourceKind, objectKey.Name)
			return
		}
		require.NoError(r, err)
	})
}

// WaitForHTTPRouteWithRetry waits for an HTTPRoute to exist with retry logic
// and delete/recreate fallback to make the tests more robust against intermittent issues.
// It checks for the HTTPRoute's existence multiple times per attempt, and if
// not found, attempts to delete and recreate the resource by reapplying the kustomize manifest.
func WaitForHTTPRouteWithRetry(t *testing.T, kubectlOptions *k8s.KubectlOptions, routeName, kustomizeDir, fqdn string) {
	t.Helper()

	logger.Logf(t, "waiting for %s to be created", fqdn)
	found := false
	maxAttempts := 3
	checksPerAttempt := 5

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		logger.Logf(t, "%s existence check attempt %d/%d", fqdn, attempt, maxAttempts)

		// Check for httproute existence using simple loop
		for i := range checksPerAttempt {
			_, err := k8s.RunKubectlAndGetOutputE(t, kubectlOptions, "get", fqdn, routeName)
			if err == nil {
				found = true
				logger.Logf(t, "%s %s found successfully", fqdn, routeName)
				break
			}
			logger.Logf(t, "%s check %d/%d: %v", fqdn, i+1, checksPerAttempt, err)
			time.Sleep(2 * time.Second)
		}

		if found {
			break
		}

		if attempt < maxAttempts {
			logger.Logf(t, "%s not found after %d seconds, attempting delete/recreate (attempt %d/%d)", fqdn, checksPerAttempt*2, attempt, maxAttempts)
			// Delete the httproute if it exists in a bad state
			_, err := k8s.RunKubectlAndGetOutputE(t, kubectlOptions, "delete", fqdn, routeName, "--ignore-not-found=true")
			if err != nil {
				logger.Logf(t, "warning: failed to delete %s %s: %v", fqdn, routeName, err)
			}
			// Recreate by reapplying the base resources
			out, err := k8s.RunKubectlAndGetOutputE(t, kubectlOptions, "apply", "-k", kustomizeDir)
			require.NoError(t, err, out)
			// Brief pause to let the recreation start
			time.Sleep(2 * time.Second)
		}
	}

	if !found {
		require.Failf(t, "%s %s was not found after %d attempts with delete/recreate", fqdn, routeName, maxAttempts)
	}
}

// EnsurePeeringAcceptorSecret ensures that a peering acceptor secret is created,
// retrying by deleting and recreating the peering acceptor if the secret name is empty.
// This is a helper function to handle flakiness in peering acceptor secret creation.
func EnsurePeeringAcceptorSecret(t *testing.T, r *retry.R, kubectlOptions *k8s.KubectlOptions, peeringAcceptorPath string) string {
	t.Helper()

	acceptorSecretName, err := k8s.RunKubectlAndGetOutputE(r, kubectlOptions, "get", "peeringacceptor", "server", "-o", "jsonpath={.status.secret.name}")
	require.NoError(r, err)

	// If the secret name is empty, retry recreating the peering acceptor up to 5 times
	if acceptorSecretName == "" {
		const maxRetries = 5
		for attempt := 1; attempt <= maxRetries; attempt++ {
			logger.Logf(t, "peering acceptor secret name is empty, recreating peering acceptor (attempt %d/%d)", attempt, maxRetries)
			k8s.KubectlDelete(t, kubectlOptions, peeringAcceptorPath)

			time.Sleep(5 * time.Second)

			k8s.KubectlApply(t, kubectlOptions, peeringAcceptorPath)

			time.Sleep(10 * time.Second)

			acceptorSecretName, err = k8s.RunKubectlAndGetOutputE(r, kubectlOptions, "get", "peeringacceptor", "server", "-o", "jsonpath={.status.secret.name}")
			require.NoError(r, err)

			if acceptorSecretName != "" {
				logger.Logf(t, "peering acceptor secret name successfully created after %d attempts", attempt)
				break
			}

			if attempt == maxRetries {
				logger.Logf(t, "peering acceptor secret name still empty after %d attempts", maxRetries)
			}
		}
	}

	require.NotEmpty(r, acceptorSecretName)
	return acceptorSecretName
}

// HasStatusCondition checks if a condition exists with the expected status and reason.
func HasStatusCondition(conditions []metav1.Condition, toCheck metav1.Condition) bool {
	for _, c := range conditions {
		if c.Type == toCheck.Type {
			return c.Reason == toCheck.Reason && c.Status == toCheck.Status
		}
	}
	return false
}
