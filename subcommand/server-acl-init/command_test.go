package serveraclinit

import (
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/consul/agent"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/tlsutil"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
	appv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

var ns = "default"
var releaseName = "release-name"
var resourcePrefix = "release-name-consul"

func TestRun_FlagValidation(t *testing.T) {
	cases := []struct {
		Flags  []string
		ExpErr string
	}{
		{
			Flags:  []string{},
			ExpErr: "-release-name or -server-label-selector must be set",
		},
		{
			Flags:  []string{"-release-name=name", "-server-label-selector=hi"},
			ExpErr: "-release-name and -server-label-selector cannot both be set",
		},
		{
			Flags:  []string{"-server-label-selector=hi"},
			ExpErr: "if -server-label-selector is set -resource-prefix must also be set",
		},
	}

	for _, c := range cases {
		t.Run(c.ExpErr, func(t *testing.T) {
			ui := cli.NewMockUi()
			cmd := Command{
				UI: ui,
			}
			responseCode := cmd.Run(c.Flags)
			require.Equal(t, 1, responseCode, ui.ErrorWriter.String())
			require.Contains(t, ui.ErrorWriter.String(), c.ExpErr)
		})
	}
}

// Test what happens if no extra flags were set (i.e. the defaults apply).
// We test with both the deprecated -release-name and the new -server-label-selector
// flags.
func TestRun_Defaults(t *testing.T) {
	t.Parallel()
	for _, flags := range [][]string{
		{"-release-name=" + releaseName},
		{
			"-server-label-selector=component=server,app=consul,release=" + releaseName,
			"-resource-prefix=" + resourcePrefix,
		},
	} {
		t.Run(flags[0], func(t *testing.T) {
			k8s, testAgent := completeSetup(t, resourcePrefix)
			defer testAgent.Shutdown()
			require := require.New(t)

			// Run the command.
			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				clientset: k8s,
			}
			args := append([]string{
				"-k8s-namespace=" + ns,
				"-expected-replicas=1",
			}, flags...)
			responseCode := cmd.Run(args)
			require.Equal(0, responseCode, ui.ErrorWriter.String())

			// Test that the bootstrap kube secret is created.
			bootToken := getBootToken(t, k8s, resourcePrefix, ns)

			// Check that it has the right policies.
			consul := testAgent.Client()
			tokenData, _, err := consul.ACL().TokenReadSelf(&api.QueryOptions{Token: bootToken})
			require.NoError(err)
			require.Equal("global-management", tokenData.Policies[0].Name)

			// Check that the agent policy was created.
			policies, _, err := consul.ACL().PolicyList(&api.QueryOptions{Token: bootToken})
			require.NoError(err)
			found := false
			for _, p := range policies {
				if p.Name == "agent-token" {
					found = true
					break
				}
			}
			require.True(found, "agent-token policy was not found")

			// We should also test that the server's token was updated, however I
			// couldn't find a way to test that with the test agent. Instead we test
			// that in another test when we're using an httptest server instead of
			// the test agent and we can assert that the /v1/agent/token/agent
			// endpoint was called.
		})
	}
}

// Test the different flags that should create tokens and save them as
// Kubernetes secrets. We test using the -release-name flag vs using the
// -resource-prefix flag.
func TestRun_Tokens(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		TokenFlag          string
		ResourcePrefixFlag string
		ReleaseNameFlag    string
		TokenName          string
		SecretName         string
	}{
		"client token -release-name": {
			TokenFlag:          "-create-client-token",
			ResourcePrefixFlag: "",
			ReleaseNameFlag:    "release-name",
			TokenName:          "client",
			SecretName:         "release-name-consul-client-acl-token",
		},
		"client token -resource-prefix": {
			TokenFlag:          "-create-client-token",
			ResourcePrefixFlag: "my-prefix",
			TokenName:          "client",
			SecretName:         "my-prefix-client-acl-token",
		},
		"catalog-sync token -release-name": {
			TokenFlag:          "-create-sync-token",
			ResourcePrefixFlag: "",
			ReleaseNameFlag:    "release-name",
			TokenName:          "catalog-sync",
			SecretName:         "release-name-consul-catalog-sync-acl-token",
		},
		"catalog-sync token -resource-prefix": {
			TokenFlag:          "-create-sync-token",
			ResourcePrefixFlag: "my-prefix",
			TokenName:          "catalog-sync",
			SecretName:         "my-prefix-catalog-sync-acl-token",
		},
		"connect-inject-namespace token -release-name": {
			TokenFlag:          "-create-inject-namespace-token",
			ResourcePrefixFlag: "",
			ReleaseNameFlag:    "release-name",
			TokenName:          "connect-inject",
			SecretName:         "release-name-consul-connect-inject-acl-token",
		},
		"connect-inject-namespace token -resource-prefix": {
			TokenFlag:          "-create-inject-namespace-token",
			ResourcePrefixFlag: "my-prefix",
			TokenName:          "connect-inject",
			SecretName:         "my-prefix-connect-inject-acl-token",
		},
		"enterprise-license token -release-name": {
			TokenFlag:          "-create-enterprise-license-token",
			ResourcePrefixFlag: "",
			ReleaseNameFlag:    "release-name",
			TokenName:          "enterprise-license",
			SecretName:         "release-name-consul-enterprise-license-acl-token",
		},
		"enterprise-license token -resource-prefix": {
			TokenFlag:          "-create-enterprise-license-token",
			ResourcePrefixFlag: "my-prefix",
			TokenName:          "enterprise-license",
			SecretName:         "my-prefix-enterprise-license-acl-token",
		},
		"mesh-gateway token -release-name": {
			TokenFlag:          "-create-mesh-gateway-token",
			ResourcePrefixFlag: "",
			ReleaseNameFlag:    "release-name",
			TokenName:          "mesh-gateway",
			SecretName:         "release-name-consul-mesh-gateway-acl-token",
		},
		"mesh-gateway token -resource-prefix": {
			TokenFlag:          "-create-mesh-gateway-token",
			ResourcePrefixFlag: "my-prefix",
			ReleaseNameFlag:    "release-name",
			TokenName:          "mesh-gateway",
			SecretName:         "my-prefix-mesh-gateway-acl-token",
		},
		"acl-replication token -release-name": {
			TokenFlag:          "-create-acl-replication-token",
			ResourcePrefixFlag: "",
			ReleaseNameFlag:    "release-name",
			TokenName:          "acl-replication",
			SecretName:         "release-name-consul-acl-replication-acl-token",
		},
		"acl-replication token -resource-prefix": {
			TokenFlag:          "-create-acl-replication-token",
			ResourcePrefixFlag: "my-prefix",
			ReleaseNameFlag:    "release-name",
			TokenName:          "acl-replication",
			SecretName:         "my-prefix-acl-replication-acl-token",
		},
	}
	for testName, c := range cases {
		t.Run(testName, func(t *testing.T) {
			prefix := c.ResourcePrefixFlag
			if c.ResourcePrefixFlag == "" {
				prefix = releaseName + "-consul"
			}
			k8s, testAgent := completeSetup(t, prefix)
			defer testAgent.Shutdown()
			require := require.New(t)

			// Run the command.
			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				clientset: k8s,
			}
			cmd.init()
			cmdArgs := []string{
				"-k8s-namespace=" + ns,
				"-expected-replicas=1",
				c.TokenFlag,
			}
			if c.ResourcePrefixFlag != "" {
				// If using the -resource-prefix flag, we expect the -server-label-selector
				// flag to also be set.
				labelSelector := fmt.Sprintf("release=%s,component=server,app=consul", releaseName)
				cmdArgs = append(cmdArgs, "-resource-prefix="+c.ResourcePrefixFlag, "-server-label-selector="+labelSelector)
			} else {
				cmdArgs = append(cmdArgs, "-release-name="+c.ReleaseNameFlag)
			}
			responseCode := cmd.Run(cmdArgs)
			require.Equal(0, responseCode, ui.ErrorWriter.String())

			// Check that the client policy was created.
			bootToken := getBootToken(t, k8s, prefix, ns)
			consul := testAgent.Client()
			policies, _, err := consul.ACL().PolicyList(&api.QueryOptions{Token: bootToken})
			require.NoError(err)
			found := false
			for _, p := range policies {
				if p.Name == c.TokenName+"-token" {
					found = true
					break
				}
			}
			require.True(found, "%s-token policy was not found", c.TokenName)

			// Test that the token was created as a Kubernetes Secret.
			tokenSecret, err := k8s.CoreV1().Secrets(ns).Get(c.SecretName, metav1.GetOptions{})
			require.NoError(err)
			require.NotNil(tokenSecret)
			token, ok := tokenSecret.Data["token"]
			require.True(ok)

			// Test that the token has the expected policies in Consul.
			tokenData, _, err := consul.ACL().TokenReadSelf(&api.QueryOptions{Token: string(token)})
			require.NoError(err)
			require.Equal(c.TokenName+"-token", tokenData.Policies[0].Name)

			// Test that if the same command is run again, it doesn't error.
			t.Run(testName+"-retried", func(t *testing.T) {
				ui := cli.NewMockUi()
				cmd := Command{
					UI:        ui,
					clientset: k8s,
				}
				cmd.init()
				responseCode := cmd.Run(cmdArgs)
				require.Equal(0, responseCode, ui.ErrorWriter.String())
			})
		})
	}
}

func TestRun_AllowDNS(t *testing.T) {
	t.Parallel()
	k8s, testAgent := completeSetup(t, resourcePrefix)
	defer testAgent.Shutdown()
	require := require.New(t)

	// Run the command.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}
	cmd.init()
	cmdArgs := []string{
		"-server-label-selector=component=server,app=consul,release=" + releaseName,
		"-resource-prefix=" + resourcePrefix,
		"-k8s-namespace=" + ns,
		"-expected-replicas=1",
		"-allow-dns",
	}
	responseCode := cmd.Run(cmdArgs)
	require.Equal(0, responseCode, ui.ErrorWriter.String())

	// Check that the dns policy was created.
	bootToken := getBootToken(t, k8s, resourcePrefix, ns)
	consul := testAgent.Client()
	policies, _, err := consul.ACL().PolicyList(&api.QueryOptions{Token: bootToken})
	require.NoError(err)
	found := false
	for _, p := range policies {
		if p.Name == "dns-policy" {
			found = true
			break
		}
	}
	require.True(found, "Did not find dns-policy")

	// Check that the anonymous token has the DNS policy.
	tokenData, _, err := consul.ACL().TokenReadSelf(&api.QueryOptions{Token: "anonymous"})
	require.NoError(err)
	require.Equal("dns-policy", tokenData.Policies[0].Name)

	// Test that if the same command is re-run it doesn't error.
	t.Run("retried", func(t *testing.T) {
		ui := cli.NewMockUi()
		cmd := Command{
			UI:        ui,
			clientset: k8s,
		}
		cmd.init()
		responseCode := cmd.Run(cmdArgs)
		require.Equal(0, responseCode, ui.ErrorWriter.String())
	})
}

func TestRun_ConnectInjectAuthMethod(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		AuthMethodFlag string
	}{
		"-create-inject-token flag": {
			AuthMethodFlag: "-create-inject-token",
		},
		"-create-inject-auth-method flag": {
			AuthMethodFlag: "-create-inject-auth-method",
		},
	}
	for testName, c := range cases {
		t.Run(testName, func(tt *testing.T) {

			k8s, testAgent := completeSetup(tt, resourcePrefix)
			defer testAgent.Shutdown()
			caCert, jwtToken := setUpK8sServiceAccount(tt, k8s)
			require := require.New(tt)

			// Run the command.
			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				clientset: k8s,
			}
			cmd.init()
			bindingRuleSelector := "serviceaccount.name!=default"
			cmdArgs := []string{
				"-server-label-selector=component=server,app=consul,release=" + releaseName,
				"-resource-prefix=" + resourcePrefix,
				"-k8s-namespace=" + ns,
				"-expected-replicas=1",
				"-acl-binding-rule-selector=" + bindingRuleSelector,
			}
			cmdArgs = append(cmdArgs, c.AuthMethodFlag)
			responseCode := cmd.Run(cmdArgs)
			require.Equal(0, responseCode, ui.ErrorWriter.String())

			// Check that the auth method was created.
			bootToken := getBootToken(t, k8s, resourcePrefix, ns)
			consul := testAgent.Client()
			authMethodName := resourcePrefix + "-k8s-auth-method"
			authMethod, _, err := consul.ACL().AuthMethodRead(authMethodName,
				&api.QueryOptions{Token: bootToken})
			require.NoError(err)
			require.Contains(authMethod.Config, "Host")
			require.Equal(authMethod.Config["Host"], "https://1.2.3.4:443")
			require.Contains(authMethod.Config, "CACert")
			require.Equal(authMethod.Config["CACert"], caCert)
			require.Contains(authMethod.Config, "ServiceAccountJWT")
			require.Equal(authMethod.Config["ServiceAccountJWT"], jwtToken)

			// Check that the binding rule was created.
			rules, _, err := consul.ACL().BindingRuleList(authMethodName, &api.QueryOptions{Token: bootToken})
			require.NoError(err)
			require.Len(rules, 1)
			require.Equal("service", string(rules[0].BindType))
			require.Equal("${serviceaccount.name}", rules[0].BindName)
			require.Equal(bindingRuleSelector, rules[0].Selector)

			// Test that if the same command is re-run it doesn't error.
			t.Run("retried", func(t *testing.T) {
				ui := cli.NewMockUi()
				cmd := Command{
					UI:        ui,
					clientset: k8s,
				}
				cmd.init()
				responseCode := cmd.Run(cmdArgs)
				require.Equal(0, responseCode, ui.ErrorWriter.String())
			})
		})
	}
}

// Test that ACL binding rules are updated if the rule selector changes.
func TestRun_BindingRuleUpdates(t *testing.T) {
	t.Parallel()
	k8s, agent := completeSetup(t, resourcePrefix)
	setUpK8sServiceAccount(t, k8s)
	defer agent.Shutdown()
	require := require.New(t)
	consul := agent.Client()

	ui := cli.NewMockUi()
	commonArgs := []string{
		"-server-label-selector=component=server,app=consul,release=" + releaseName,
		"-resource-prefix=" + resourcePrefix,
		"-k8s-namespace=" + ns,
		"-expected-replicas=1",
		"-create-inject-auth-method",
	}
	firstRunArgs := append(commonArgs,
		"-acl-binding-rule-selector=serviceaccount.name!=default",
	)
	// Our second run, we change the binding rule selector.
	secondRunArgs := append(commonArgs,
		"-acl-binding-rule-selector=serviceaccount.name!=changed",
	)

	// Run the command first to populate the binding rule.
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}
	responseCode := cmd.Run(firstRunArgs)
	require.Equal(0, responseCode, ui.ErrorWriter.String())

	// Validate the binding rule.
	{
		queryOpts := &api.QueryOptions{Token: getBootToken(t, k8s, resourcePrefix, ns)}
		authMethodName := releaseName + "-consul-k8s-auth-method"
		rules, _, err := consul.ACL().BindingRuleList(authMethodName, queryOpts)
		require.NoError(err)
		require.Len(rules, 1)
		actRule, _, err := consul.ACL().BindingRuleRead(rules[0].ID, queryOpts)
		require.NoError(err)
		require.NotNil(actRule)
		require.Equal("Kubernetes binding rule", actRule.Description)
		require.Equal(api.BindingRuleBindTypeService, actRule.BindType)
		require.Equal("${serviceaccount.name}", actRule.BindName)
		require.Equal("serviceaccount.name!=default", actRule.Selector)
	}

	// Re-run the command with namespace flags. The policies should be updated.
	// NOTE: We're redefining the command so that the old flag values are
	// reset.
	cmd = Command{
		UI:        ui,
		clientset: k8s,
	}
	responseCode = cmd.Run(secondRunArgs)
	require.Equal(0, responseCode, ui.ErrorWriter.String())

	// Check the binding rule is changed expected.
	{
		queryOpts := &api.QueryOptions{Token: getBootToken(t, k8s, resourcePrefix, ns)}
		authMethodName := releaseName + "-consul-k8s-auth-method"
		rules, _, err := consul.ACL().BindingRuleList(authMethodName, queryOpts)
		require.NoError(err)
		require.Len(rules, 1)
		actRule, _, err := consul.ACL().BindingRuleRead(rules[0].ID, queryOpts)
		require.NoError(err)
		require.NotNil(actRule)
		require.Equal("Kubernetes binding rule", actRule.Description)
		require.Equal(api.BindingRuleBindTypeService, actRule.BindType)
		require.Equal("${serviceaccount.name}", actRule.BindName)
		require.Equal("serviceaccount.name!=changed", actRule.Selector)
	}
}

// Test that if the server pods aren't available at first that bootstrap
// still succeeds.
func TestRun_DelayedServerPods(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	k8s := fake.NewSimpleClientset()

	type APICall struct {
		Method string
		Path   string
	}
	var consulAPICalls []APICall
	consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Record all the API calls made.
		consulAPICalls = append(consulAPICalls, APICall{
			Method: r.Method,
			Path:   r.URL.Path,
		})

		// Send an empty JSON response with code 200 to all calls.
		fmt.Fprintln(w, "{}")
	}))
	defer consulServer.Close()
	serverURL, err := url.Parse(consulServer.URL)
	require.NoError(err)
	port, err := strconv.Atoi(serverURL.Port())
	require.NoError(err)

	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}
	cmd.init()

	// Start the command before the Pod exist.
	// Run in a goroutine so we can create the Pods asynchronously
	done := make(chan bool)
	var responseCode int
	go func() {
		responseCode = cmd.Run([]string{
			"-server-label-selector=component=server,app=consul,release=" + releaseName,
			"-resource-prefix=" + resourcePrefix,
			"-k8s-namespace=" + ns,
			"-expected-replicas=1",
		})
		close(done)
	}()

	// Asynchronously create the server Pod after a delay.
	go func() {
		// Create the Pods after a delay between 100 and 500ms.
		// It's randomized to ensure we're not relying on specific timing.
		delay := 100 + rand.Intn(400)
		time.Sleep(time.Duration(delay) * time.Millisecond)

		pods := k8s.CoreV1().Pods(ns)
		_, err = pods.Create(&v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: resourcePrefix + "-server-0",
				Labels: map[string]string{
					"component": "server",
					"app":       "consul",
					"release":   releaseName,
				},
			},
			Status: v1.PodStatus{
				PodIP: serverURL.Hostname(),
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name: "consul",
						Ports: []v1.ContainerPort{
							{
								Name:          "http",
								ContainerPort: int32(port),
							},
						},
					},
				},
			},
		})
		require.NoError(err)
		_, err = k8s.AppsV1().StatefulSets(ns).Create(&appv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: resourcePrefix + "-server",
				Labels: map[string]string{
					"component": "server",
					"app":       "consul",
					"release":   releaseName,
				},
			},
			Status: appv1.StatefulSetStatus{
				UpdateRevision:  "current",
				CurrentRevision: "current",
			},
		})
		require.NoError(err)
	}()

	// Wait for the command to exit.
	select {
	case <-done:
		require.Equal(0, responseCode, ui.ErrorWriter.String())
	case <-time.After(2 * time.Second):
		require.FailNow("command did not exit after 2s")
	}

	// Test that the bootstrap kube secret is created.
	getBootToken(t, k8s, resourcePrefix, ns)

	// Test that the expected API calls were made.
	require.Equal([]APICall{
		{
			"PUT",
			"/v1/acl/bootstrap",
		},
		{
			"PUT",
			"/v1/acl/policy",
		},
		{
			"PUT",
			"/v1/acl/token",
		},
		{
			"PUT",
			"/v1/agent/token/agent",
		},
		{
			"PUT",
			"/v1/acl/policy",
		},
		{
			"PUT",
			"/v1/acl/token",
		},
	}, consulAPICalls)
}

// Test that if a deployment of the statefulset is in progress we wait.
func TestRun_InProgressDeployment(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	k8s := fake.NewSimpleClientset()

	type APICall struct {
		Method string
		Path   string
	}
	var consulAPICalls []APICall
	consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Record all the API calls made.
		consulAPICalls = append(consulAPICalls, APICall{
			Method: r.Method,
			Path:   r.URL.Path,
		})

		// Send an empty JSON response with code 200 to all calls.
		fmt.Fprintln(w, "{}")
	}))
	defer consulServer.Close()
	serverURL, err := url.Parse(consulServer.URL)
	require.NoError(err)
	port, err := strconv.Atoi(serverURL.Port())
	require.NoError(err)

	// The pods and statefulset are created but as an in-progress deployment
	pods := k8s.CoreV1().Pods(ns)
	_, err = pods.Create(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourcePrefix + "-server-0",
			Labels: map[string]string{
				"component": "server",
				"app":       "consul",
				"release":   releaseName,
			},
		},
		Status: v1.PodStatus{
			PodIP: serverURL.Hostname(),
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: "consul",
					Ports: []v1.ContainerPort{
						{
							Name:          "http",
							ContainerPort: int32(port),
						},
					},
				},
			},
		},
	})
	require.NoError(err)
	_, err = k8s.AppsV1().StatefulSets(ns).Create(&appv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourcePrefix + "-server",
			Labels: map[string]string{
				"component": "server",
				"app":       "consul",
				"release":   releaseName,
			},
		},
		Status: appv1.StatefulSetStatus{
			UpdateRevision:  "updated",
			CurrentRevision: "current",
		},
	})
	require.NoError(err)

	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}
	cmd.init()

	// Start the command before the Pod exist.
	// Run in a goroutine so we can create the Pods asynchronously
	done := make(chan bool)
	var responseCode int
	go func() {
		responseCode = cmd.Run([]string{
			"-server-label-selector=component=server,app=consul,release=" + releaseName,
			"-resource-prefix=" + resourcePrefix,
			"-k8s-namespace=" + ns,
			"-expected-replicas=1",
		})
		close(done)
	}()

	// Asynchronously update the deployment status after a delay.
	go func() {
		// Update after a delay between 100 and 500ms.
		// It's randomized to ensure we're not relying on specific timing.
		delay := 100 + rand.Intn(400)
		time.Sleep(time.Duration(delay) * time.Millisecond)
		_, err = k8s.AppsV1().StatefulSets(ns).Update(&appv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: resourcePrefix + "-server",
				Labels: map[string]string{
					"component": "server",
					"app":       "consul",
					"release":   releaseName,
				},
			},
			Status: appv1.StatefulSetStatus{
				UpdateRevision:  "updated",
				CurrentRevision: "updated",
			},
		})
		require.NoError(err)
	}()

	// Wait for the command to exit.
	select {
	case <-done:
		require.Equal(0, responseCode, ui.ErrorWriter.String())
	case <-time.After(2 * time.Second):
		require.FailNow("command did not exit after 2s")
	}

	// Test that the bootstrap kube secret is created.
	getBootToken(t, k8s, resourcePrefix, ns)

	// Test that the expected API calls were made.
	require.Equal([]APICall{
		{
			"PUT",
			"/v1/acl/bootstrap",
		},
		{
			"PUT",
			"/v1/acl/policy",
		},
		{
			"PUT",
			"/v1/acl/token",
		},
		{
			"PUT",
			"/v1/agent/token/agent",
		},
		{
			"PUT",
			"/v1/acl/policy",
		},
		{
			"PUT",
			"/v1/acl/token",
		},
	}, consulAPICalls)
}

// Test that if there's no leader, we retry until one is elected.
func TestRun_NoLeader(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	k8s := fake.NewSimpleClientset()

	type APICall struct {
		Method string
		Path   string
	}
	var consulAPICalls []APICall

	// Start the Consul server.
	numACLBootCalls := 0
	consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Record all the API calls made.
		consulAPICalls = append(consulAPICalls, APICall{
			Method: r.Method,
			Path:   r.URL.Path,
		})

		switch r.URL.Path {
		case "/v1/acl/bootstrap":
			// On the first two calls, return the error that results from no leader
			// being elected.
			if numACLBootCalls < 2 {
				w.WriteHeader(500)
				fmt.Fprintln(w, "The ACL system is currently in legacy mode.")
			} else {
				fmt.Fprintln(w, "{}")
			}
			numACLBootCalls++
		default:
			fmt.Fprintln(w, "{}")
		}
	}))
	defer consulServer.Close()

	// Create the Server Pods.
	serverURL, err := url.Parse(consulServer.URL)
	require.NoError(err)
	port, err := strconv.Atoi(serverURL.Port())
	require.NoError(err)
	pods := k8s.CoreV1().Pods(ns)
	_, err = pods.Create(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourcePrefix + "-server-0",
			Labels: map[string]string{
				"component": "server",
				"app":       "consul",
				"release":   releaseName,
			},
		},
		Status: v1.PodStatus{
			PodIP: serverURL.Hostname(),
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: "consul",
					Ports: []v1.ContainerPort{
						{
							Name:          "http",
							ContainerPort: int32(port),
						},
					},
				},
			},
		},
	})
	require.NoError(err)
	// Create Consul server Statefulset.
	_, err = k8s.AppsV1().StatefulSets(ns).Create(&appv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourcePrefix + "-server",
			Labels: map[string]string{
				"component": "server",
				"app":       "consul",
				"release":   releaseName,
			},
		},
		Status: appv1.StatefulSetStatus{
			UpdateRevision:  "current",
			CurrentRevision: "current",
		},
	})

	// Run the command.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}
	cmd.init()

	done := make(chan bool)
	var responseCode int
	go func() {
		responseCode = cmd.Run([]string{
			"-server-label-selector=component=server,app=consul,release=" + releaseName,
			"-resource-prefix=" + resourcePrefix,
			"-k8s-namespace=" + ns,
			"-expected-replicas=1",
		})
		close(done)
	}()

	select {
	case <-done:
		require.Equal(0, responseCode, ui.ErrorWriter.String())
	case <-time.After(5 * time.Second):
		require.FailNow("command did not complete within 5s")
	}

	// Test that the bootstrap kube secret is created.
	getBootToken(t, k8s, resourcePrefix, ns)

	// Test that the expected API calls were made.
	require.Equal([]APICall{
		// Bootstrap will have been called 3 times.
		{
			"PUT",
			"/v1/acl/bootstrap",
		},
		{
			"PUT",
			"/v1/acl/bootstrap",
		},
		{
			"PUT",
			"/v1/acl/bootstrap",
		},
		{
			"PUT",
			"/v1/acl/policy",
		},
		{
			"PUT",
			"/v1/acl/token",
		},
		{
			"PUT",
			"/v1/agent/token/agent",
		},
		{
			"PUT",
			"/v1/acl/policy",
		},
		{
			"PUT",
			"/v1/acl/token",
		},
	}, consulAPICalls)
}

// Test that if creating client tokens fails at first, we retry.
func TestRun_ClientTokensRetry(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	k8s := fake.NewSimpleClientset()

	type APICall struct {
		Method string
		Path   string
	}
	var consulAPICalls []APICall

	// Start the Consul server.
	numPolicyCalls := 0
	consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Record all the API calls made.
		consulAPICalls = append(consulAPICalls, APICall{
			Method: r.Method,
			Path:   r.URL.Path,
		})

		switch r.URL.Path {
		// The second call to create a policy will fail. This is the client
		// token call.
		case "/v1/acl/policy":
			if numPolicyCalls == 1 {
				w.WriteHeader(500)
				fmt.Fprintln(w, "The ACL system is currently in legacy mode.")
			} else {
				fmt.Fprintln(w, "{}")
			}
			numPolicyCalls++
		default:
			fmt.Fprintln(w, "{}")
		}
	}))
	defer consulServer.Close()

	// Create the Server Pods.
	serverURL, err := url.Parse(consulServer.URL)
	require.NoError(err)
	port, err := strconv.Atoi(serverURL.Port())
	require.NoError(err)
	pods := k8s.CoreV1().Pods(ns)
	_, err = pods.Create(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourcePrefix + "-server-0",
			Labels: map[string]string{
				"component": "server",
				"app":       "consul",
				"release":   releaseName,
			},
		},
		Status: v1.PodStatus{
			PodIP: serverURL.Hostname(),
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: "consul",
					Ports: []v1.ContainerPort{
						{
							Name:          "http",
							ContainerPort: int32(port),
						},
					},
				},
			},
		},
	})
	require.NoError(err)
	// Create the server statefulset.
	_, err = k8s.AppsV1().StatefulSets(ns).Create(&appv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourcePrefix + "-server",
			Labels: map[string]string{
				"component": "server",
				"app":       "consul",
				"release":   releaseName,
			},
		},
		Status: appv1.StatefulSetStatus{
			UpdateRevision:  "current",
			CurrentRevision: "current",
		},
	})
	require.NoError(err)

	// Run the command.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}
	cmd.init()
	responseCode := cmd.Run([]string{
		"-server-label-selector=component=server,app=consul,release=" + releaseName,
		"-resource-prefix=" + resourcePrefix,
		"-k8s-namespace=" + ns,
		"-expected-replicas=1",
	})
	require.Equal(0, responseCode, ui.ErrorWriter.String())

	// Test that the expected API calls were made.
	require.Equal([]APICall{
		{
			"PUT",
			"/v1/acl/bootstrap",
		},
		{
			"PUT",
			"/v1/acl/policy",
		},
		{
			"PUT",
			"/v1/acl/token",
		},
		{
			"PUT",
			"/v1/agent/token/agent",
		},
		// This call should happen twice since the first will fail.
		{
			"PUT",
			"/v1/acl/policy",
		},
		{
			"PUT",
			"/v1/acl/policy",
		},
		{
			"PUT",
			"/v1/acl/token",
		},
	}, consulAPICalls)
}

// Test if there is an old bootstrap Secret we assume the servers were
// bootstrapped already and continue on to the next step.
func TestRun_AlreadyBootstrapped(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	k8s := fake.NewSimpleClientset()

	type APICall struct {
		Method string
		Path   string
	}
	var consulAPICalls []APICall

	// Start the Consul server.
	consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Record all the API calls made.
		consulAPICalls = append(consulAPICalls, APICall{
			Method: r.Method,
			Path:   r.URL.Path,
		})

		switch r.URL.Path {
		default:
			fmt.Fprintln(w, "{}")
		}
	}))
	defer consulServer.Close()

	// Create the Server Pods.
	serverURL, err := url.Parse(consulServer.URL)
	require.NoError(err)
	port, err := strconv.Atoi(serverURL.Port())
	require.NoError(err)
	pods := k8s.CoreV1().Pods(ns)
	_, err = pods.Create(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourcePrefix + "-server-0",
			Labels: map[string]string{
				"component": "server",
				"app":       "consul",
				"release":   releaseName,
			},
		},
		Status: v1.PodStatus{
			PodIP: serverURL.Hostname(),
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: "consul",
					Ports: []v1.ContainerPort{
						{
							Name:          "http",
							ContainerPort: int32(port),
						},
					},
				},
			},
		},
	})
	require.NoError(err)
	// Create the server statefulset.
	_, err = k8s.AppsV1().StatefulSets(ns).Create(&appv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourcePrefix + "-server",
			Labels: map[string]string{
				"component": "server",
				"app":       "consul",
				"release":   releaseName,
			},
		},
		Status: appv1.StatefulSetStatus{
			UpdateRevision:  "current",
			CurrentRevision: "current",
		},
	})
	require.NoError(err)

	// Create the bootstrap secret.
	_, err = k8s.CoreV1().Secrets(ns).Create(&v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourcePrefix + "-bootstrap-acl-token",
		},
		Data: map[string][]byte{
			"token": []byte("old-token"),
		},
	})
	require.NoError(err)

	// Run the command.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}
	cmd.init()
	responseCode := cmd.Run([]string{
		"-server-label-selector=component=server,app=consul,release=" + releaseName,
		"-resource-prefix=" + resourcePrefix,
		"-k8s-namespace=" + ns,
		"-expected-replicas=1",
	})
	require.Equal(0, responseCode, ui.ErrorWriter.String())

	// Test that the Secret is the same.
	secret, err := k8s.CoreV1().Secrets(ns).Get(resourcePrefix+"-bootstrap-acl-token", metav1.GetOptions{})
	require.NoError(err)
	require.Contains(secret.Data, "token")
	require.Equal("old-token", string(secret.Data["token"]))

	// Test that the expected API calls were made.
	require.Equal([]APICall{
		// We only expect the calls for creating client tokens
		// and updating the server policy.
		{
			"PUT",
			"/v1/acl/policy",
		},
		{
			"PUT",
			"/v1/acl/policy",
		},
		{
			"PUT",
			"/v1/acl/token",
		},
	}, consulAPICalls)
}

// Test that we exit after timeout.
func TestRun_Timeout(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	k8s := fake.NewSimpleClientset()
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}
	cmd.init()
	responseCode := cmd.Run([]string{
		"-server-label-selector=component=server,app=consul,release=" + releaseName,
		"-resource-prefix=" + resourcePrefix,
		"-k8s-namespace=" + ns,
		"-expected-replicas=1",
		"-timeout=500ms",
	})
	require.Equal(1, responseCode, ui.ErrorWriter.String())
}

// Test that the bootstrapping process can make calls to Consul API over HTTPS
// when the consul agent is configured with HTTPS only (HTTP disabled).
func TestRun_HTTPS(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	k8s := fake.NewSimpleClientset()

	caFile, certFile, keyFile, cleanup := generateServerCerts(t)
	defer cleanup()

	agentConfig := fmt.Sprintf(`
		primary_datacenter = "dc1"
		acl {
			enabled = true
		}
		ca_file = "%s"
		cert_file = "%s"
		key_file = "%s"`, caFile, certFile, keyFile)

	a := &agent.TestAgent{
		Name:   t.Name(),
		HCL:    agentConfig,
		UseTLS: true, // this also disables HTTP port
	}

	a.Start()
	defer a.Shutdown()

	createTestK8SResources(t, k8s, a.HTTPAddr(), resourcePrefix, "https", ns)

	// Run the command.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}
	cmd.init()
	responseCode := cmd.Run([]string{
		"-server-label-selector=component=server,app=consul,release=" + releaseName,
		"-resource-prefix=" + resourcePrefix,
		"-k8s-namespace=" + ns,
		"-use-https",
		"-consul-tls-server-name", "server.dc1.consul",
		"-consul-ca-cert", caFile,
		"-expected-replicas=1",
	})
	require.Equal(0, responseCode, ui.ErrorWriter.String())

	// Test that the bootstrap token is created to make sure the bootstrapping succeeded.
	// The presence of the bootstrap token tells us that the API calls to Consul have been successful.
	tokenSecret, err := k8s.CoreV1().Secrets(ns).Get(resourcePrefix+"-bootstrap-acl-token", metav1.GetOptions{})
	require.NoError(err)
	require.NotNil(tokenSecret)
	_, ok := tokenSecret.Data["token"]
	require.True(ok)
}

// Set up test consul agent and kubernetes cluster.
func completeSetup(t *testing.T, prefix string) (*fake.Clientset, *agent.TestAgent) {
	k8s := fake.NewSimpleClientset()

	a := agent.NewTestAgent(t, t.Name(), `
	primary_datacenter = "dc1"
	acl {
		enabled = true
	}`)

	createTestK8SResources(t, k8s, a.HTTPAddr(), prefix, "http", ns)

	return k8s, a
}

// Create test k8s resources (server pods and server stateful set)
func createTestK8SResources(t *testing.T, k8s *fake.Clientset, consulHTTPAddr, prefix, scheme, k8sNamespace string) {
	require := require.New(t)
	consulURL, err := url.Parse("http://" + consulHTTPAddr)
	require.NoError(err)
	port, err := strconv.Atoi(consulURL.Port())
	require.NoError(err)

	// Create Consul server Pod.
	_, err = k8s.CoreV1().Pods(k8sNamespace).Create(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: prefix + "-server-0",
			Labels: map[string]string{
				"component": "server",
				"app":       "consul",
				"release":   releaseName,
			},
		},
		Status: v1.PodStatus{
			PodIP: consulURL.Hostname(),
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: "consul",
					Ports: []v1.ContainerPort{
						{
							Name:          scheme,
							ContainerPort: int32(port),
						},
					},
				},
			},
		},
	})
	require.NoError(err)

	// Create Consul server Statefulset.
	_, err = k8s.AppsV1().StatefulSets(k8sNamespace).Create(&appv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: prefix + "-server",
			Labels: map[string]string{
				"component": "server",
				"app":       "consul",
				"release":   releaseName,
			},
		},
		Status: appv1.StatefulSetStatus{
			UpdateRevision:  "current",
			CurrentRevision: "current",
		},
	})
	require.NoError(err)
}

// getBootToken gets the bootstrap token from the Kubernetes secret. It will
// cause a test failure if the Secret doesn't exist or is malformed.
func getBootToken(t *testing.T, k8s *fake.Clientset, prefix string, k8sNamespace string) string {
	bootstrapSecret, err := k8s.CoreV1().Secrets(k8sNamespace).Get(fmt.Sprintf("%s-bootstrap-acl-token", prefix), metav1.GetOptions{})
	require.NoError(t, err)
	require.NotNil(t, bootstrapSecret)
	bootToken, ok := bootstrapSecret.Data["token"]
	require.True(t, ok)
	return string(bootToken)
}

// generateServerCerts generates Consul CA
// and a server certificate and saves them to temp files.
// It returns file names in this order:
// CA certificate, server certificate, and server key.
// Note that it's the responsibility of the caller to
// remove the temporary files created by this function.
func generateServerCerts(t *testing.T) (string, string, string, func()) {
	require := require.New(t)

	caFile, err := ioutil.TempFile("", "ca")
	require.NoError(err)

	certFile, err := ioutil.TempFile("", "cert")
	require.NoError(err)

	certKeyFile, err := ioutil.TempFile("", "key")
	require.NoError(err)

	// Generate CA
	sn, err := tlsutil.GenerateSerialNumber()
	require.NoError(err)

	s, _, err := tlsutil.GeneratePrivateKey()
	require.NoError(err)

	constraints := []string{"consul", "localhost"}
	ca, err := tlsutil.GenerateCA(s, sn, 1, constraints)
	require.NoError(err)

	// Generate Server Cert
	name := fmt.Sprintf("server.%s.%s", "dc1", "consul")
	DNSNames := []string{name, "localhost"}
	IPAddresses := []net.IP{net.ParseIP("127.0.0.1")}
	extKeyUsage := []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}

	sn, err = tlsutil.GenerateSerialNumber()
	require.NoError(err)

	pub, priv, err := tlsutil.GenerateCert(s, ca, sn, name, 1, DNSNames, IPAddresses, extKeyUsage)
	require.NoError(err)

	// Write certs and key to files
	_, err = caFile.WriteString(ca)
	require.NoError(err)
	_, err = certFile.WriteString(pub)
	require.NoError(err)
	_, err = certKeyFile.WriteString(priv)
	require.NoError(err)

	cleanupFunc := func() {
		os.Remove(caFile.Name())
		os.Remove(certFile.Name())
		os.Remove(certKeyFile.Name())
	}
	return caFile.Name(), certFile.Name(), certKeyFile.Name(), cleanupFunc
}

// setUpK8sServiceAccount creates a Service Account for the connect injector.
// This Service Account would normally automatically be created by Kubernetes
// when the injector deployment is created. It returns the Service Account
// CA Cert and JWT token.
func setUpK8sServiceAccount(t *testing.T, k8s *fake.Clientset) (string, string) {
	// Create Kubernetes Service.
	_, err := k8s.CoreV1().Services(ns).Create(&v1.Service{
		Spec: v1.ServiceSpec{
			ClusterIP: "1.2.3.4",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "kubernetes",
		},
	})
	require.NoError(t, err)

	// Create ServiceAccount for the injector that the helm chart creates.
	_, err = k8s.CoreV1().ServiceAccounts(ns).Create(&v1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourcePrefix + "-connect-injector-authmethod-svc-account",
		},
		Secrets: []v1.ObjectReference{
			{
				Name: resourcePrefix + "-connect-injector-authmethod-svc-account",
			},
		},
	})
	require.NoError(t, err)

	// Create the ServiceAccount Secret.
	caCertBytes, err := base64.StdEncoding.DecodeString(serviceAccountCACert)
	require.NoError(t, err)
	tokenBytes, err := base64.StdEncoding.DecodeString(serviceAccountToken)
	require.NoError(t, err)
	_, err = k8s.CoreV1().Secrets(ns).Create(&v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourcePrefix + "-connect-injector-authmethod-svc-account",
		},
		Data: map[string][]byte{
			"ca.crt": caCertBytes,
			"token":  tokenBytes,
		},
	})
	require.NoError(t, err)
	return string(caCertBytes), string(tokenBytes)
}

var serviceAccountCACert = "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSURDekNDQWZPZ0F3SUJBZ0lRS3pzN05qbDlIczZYYzhFWG91MjVoekFOQmdrcWhraUc5dzBCQVFzRkFEQXYKTVMwd0t3WURWUVFERXlRMU9XVTJaR00wTVMweU1EaG1MVFF3T1RVdFlUSTRPUzB4Wm1NM01EQmhZekZqWXpndwpIaGNOTVRrd05qQTNNVEF4TnpNeFdoY05NalF3TmpBMU1URXhOek14V2pBdk1TMHdLd1lEVlFRREV5UTFPV1UyClpHTTBNUzB5TURobUxUUXdPVFV0WVRJNE9TMHhabU0zTURCaFl6RmpZemd3Z2dFaU1BMEdDU3FHU0liM0RRRUIKQVFVQUE0SUJEd0F3Z2dFS0FvSUJBUURaakh6d3FvZnpUcEdwYzBNZElDUzdldXZmdWpVS0UzUEMvYXBmREFnQgo0anpFRktBNzgvOStLVUd3L2MvMFNIZVNRaE4rYThnd2xIUm5BejFOSmNmT0lYeTRkd2VVdU9rQWlGeEg4cGh0CkVDd2tlTk83ejhEb1Y4Y2VtaW5DUkhHamFSbW9NeHBaN2cycFpBSk5aZVB4aTN5MWFOa0ZBWGU5Z1NVU2RqUloKUlhZa2E3d2gyQU85azJkbEdGQVlCK3Qzdld3SjZ0d2pHMFR0S1FyaFlNOU9kMS9vTjBFMDFMekJjWnV4a04xawo4Z2ZJSHk3Yk9GQ0JNMldURURXLzBhQXZjQVByTzhETHFESis2TWpjM3I3K3psemw4YVFzcGIwUzA4cFZ6a2k1CkR6Ly84M2t5dTBwaEp1aWo1ZUI4OFY3VWZQWHhYRi9FdFY2ZnZyTDdNTjRmQWdNQkFBR2pJekFoTUE0R0ExVWQKRHdFQi93UUVBd0lDQkRBUEJnTlZIUk1CQWY4RUJUQURBUUgvTUEwR0NTcUdTSWIzRFFFQkN3VUFBNElCQVFCdgpRc2FHNnFsY2FSa3RKMHpHaHh4SjUyTm5SVjJHY0lZUGVOM1p2MlZYZTNNTDNWZDZHMzJQVjdsSU9oangzS21BCi91TWg2TmhxQnpzZWtrVHowUHVDM3dKeU0yT0dvblZRaXNGbHF4OXNGUTNmVTJtSUdYQ2Ezd0M4ZS9xUDhCSFMKdzcvVmVBN2x6bWozVFFSRS9XMFUwWkdlb0F4bjliNkp0VDBpTXVjWXZQMGhYS1RQQldsbnpJaWphbVU1MHIyWQo3aWEwNjVVZzJ4VU41RkxYL3Z4T0EzeTRyanBraldvVlFjdTFwOFRaclZvTTNkc0dGV3AxMGZETVJpQUhUdk9ICloyM2pHdWs2cm45RFVIQzJ4UGozd0NUbWQ4U0dFSm9WMzFub0pWNWRWZVE5MHd1c1h6M3ZURzdmaWNLbnZIRlMKeHRyNVBTd0gxRHVzWWZWYUdIMk8KLS0tLS1FTkQgQ0VSVElGSUNBVEUtLS0tLQo="
var serviceAccountToken = "ZXlKaGJHY2lPaUpTVXpJMU5pSXNJbXRwWkNJNklpSjkuZXlKcGMzTWlPaUpyZFdKbGNtNWxkR1Z6TDNObGNuWnBZMlZoWTJOdmRXNTBJaXdpYTNWaVpYSnVaWFJsY3k1cGJ5OXpaWEoyYVdObFlXTmpiM1Z1ZEM5dVlXMWxjM0JoWTJVaU9pSmtaV1poZFd4MElpd2lhM1ZpWlhKdVpYUmxjeTVwYnk5elpYSjJhV05sWVdOamIzVnVkQzl6WldOeVpYUXVibUZ0WlNJNkltdG9ZV3RwTFdGeVlXTm9ibWxrTFdOdmJuTjFiQzFqYjI1dVpXTjBMV2x1YW1WamRHOXlMV0YxZEdodFpYUm9iMlF0YzNaakxXRmpZMjlvYm1SaWRpSXNJbXQxWW1WeWJtVjBaWE11YVc4dmMyVnlkbWxqWldGalkyOTFiblF2YzJWeWRtbGpaUzFoWTJOdmRXNTBMbTVoYldVaU9pSnJhR0ZyYVMxaGNtRmphRzVwWkMxamIyNXpkV3d0WTI5dWJtVmpkQzFwYm1wbFkzUnZjaTFoZFhSb2JXVjBhRzlrTFhOMll5MWhZMk52ZFc1MElpd2lhM1ZpWlhKdVpYUmxjeTVwYnk5elpYSjJhV05sWVdOamIzVnVkQzl6WlhKMmFXTmxMV0ZqWTI5MWJuUXVkV2xrSWpvaU4yVTVOV1V4TWprdFpUUTNNeTB4TVdVNUxUaG1ZV0V0TkRJd01UQmhPREF3TVRJeUlpd2ljM1ZpSWpvaWMzbHpkR1Z0T25ObGNuWnBZMlZoWTJOdmRXNTBPbVJsWm1GMWJIUTZhMmhoYTJrdFlYSmhZMmh1YVdRdFkyOXVjM1ZzTFdOdmJtNWxZM1F0YVc1cVpXTjBiM0l0WVhWMGFHMWxkR2h2WkMxemRtTXRZV05qYjNWdWRDSjkuWWk2M01NdHpoNU1CV0tLZDNhN2R6Q0pqVElURTE1aWtGeV9UbnBka19Bd2R3QTlKNEFNU0dFZUhONXZXdEN1dUZqb19sTUpxQkJQSGtLMkFxYm5vRlVqOW01Q29wV3lxSUNKUWx2RU9QNGZVUS1SYzBXMVBfSmpVMXJaRVJIRzM5YjVUTUxnS1BRZ3V5aGFpWkVKNkNqVnRtOXdVVGFncmdpdXFZVjJpVXFMdUY2U1lObTZTckt0a1BTLWxxSU8tdTdDMDZ3Vms1bTV1cXdJVlFOcFpTSUNfNUxzNWFMbXlaVTNuSHZILVY3RTNIbUJoVnlaQUI3NmpnS0IwVHlWWDFJT3NrdDlQREZhck50VTNzdVp5Q2p2cUMtVUpBNnNZZXlTZTRkQk5Lc0tsU1o2WXV4VVVtbjFSZ3YzMllNZEltbnNXZzhraGYtekp2cWdXazdCNUVB"
