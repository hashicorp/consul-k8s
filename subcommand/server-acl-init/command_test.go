package serveraclinit

import (
	"encoding/base64"
	"fmt"
	"github.com/hashicorp/consul/agent"
	"github.com/hashicorp/consul/api"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"
)

var ns = "default"
var releaseName = "release-name"

// Set up test consul agent and kubernetes clusters with
func completeSetup(t *testing.T) (*fake.Clientset, *agent.TestAgent) {
	require := require.New(t)
	k8s := fake.NewSimpleClientset()

	a := agent.NewTestAgent(t, t.Name(), `
	primary_datacenter = "dc1"
	acl {
		enabled = true
	}`)

	consulURL, err := url.Parse("http://" + a.HTTPAddr())
	require.NoError(err)
	port, err := strconv.Atoi(consulURL.Port())
	require.NoError(err)

	// Create Consul server Pod.
	_, err = k8s.CoreV1().Pods(ns).Create(&v1.Pod{
		ObjectMeta: v12.ObjectMeta{
			Name: releaseName + "-consul-server-0",
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
							Name:          "http",
							ContainerPort: int32(port),
						},
					},
				},
			},
		},
	})
	require.NoError(err)
	return k8s, a
}

func TestRun_Defaults(t *testing.T) {
	t.Parallel()
	k8s, testAgent := completeSetup(t)
	defer testAgent.Shutdown()
	require := require.New(t)

	// Run the command.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}
	cmd.init()
	responseCode := cmd.Run([]string{
		"-release-name=" + releaseName,
		"-k8s-namespace=" + ns,
		"-expected-replicas=1",
	})
	require.Equal(0, responseCode, ui.ErrorWriter.String())

	// Test that the bootstrap kube secret is created.
	bootToken := getBootToken(t, k8s, releaseName)

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
}

// Test the different flags that should create tokens and save them as
// Kubernetes secrets.
func TestRun_Tokens(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		Flag      string
		TokenName string
	}{
		"client token": {
			"-create-client-token",
			"client",
		},
		"catalog-sync token": {
			"-create-sync-token",
			"catalog-sync",
		},
		"enterprise-license token": {
			"-create-enterprise-license-token",
			"enterprise-license",
		},
		"snapshot-agent token": {
			"-create-snapshot-agent-token",
			"client-snapshot-agent",
		},
		"mesh-gateway token": {
			"-create-mesh-gateway-token",
			"mesh-gateway",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			k8s, testAgent := completeSetup(t)
			defer testAgent.Shutdown()
			require := require.New(t)

			// Run the command.
			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				clientset: k8s,
			}
			cmd.init()
			responseCode := cmd.Run([]string{
				"-release-name=" + releaseName,
				"-k8s-namespace=" + ns,
				"-expected-replicas=1",
				c.Flag,
			})
			require.Equal(0, responseCode, ui.ErrorWriter.String())

			// Check that the client policy was created.
			bootToken := getBootToken(t, k8s, releaseName)
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

			// Test that the token was created as a Kubernetes Secret.
			require.True(found, "%s-token policy was not found", c.TokenName)
			tokenSecret, err := k8s.CoreV1().Secrets(ns).Get(fmt.Sprintf("%s-consul-%s-acl-token", releaseName, c.TokenName), metav1.GetOptions{})
			require.NoError(err)
			require.NotNil(tokenSecret)
			token, ok := tokenSecret.Data["token"]
			require.True(ok)

			// Test that the token has the expected policies in Consul.
			tokenData, _, err := consul.ACL().TokenReadSelf(&api.QueryOptions{Token: string(token)})
			require.NoError(err)
			require.Equal(c.TokenName+"-token", tokenData.Policies[0].Name)
		})
	}
}

func TestRun_AllowDNS(t *testing.T) {
	t.Parallel()
	k8s, testAgent := completeSetup(t)
	defer testAgent.Shutdown()
	require := require.New(t)

	// Run the command.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}
	cmd.init()
	responseCode := cmd.Run([]string{
		"-release-name=" + releaseName,
		"-k8s-namespace=" + ns,
		"-expected-replicas=1",
		"-allow-dns",
	})
	require.Equal(0, responseCode, ui.ErrorWriter.String())

	// Check that the dns policy was created.
	bootToken := getBootToken(t, k8s, releaseName)
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
}

func TestRun_ConnectInjectToken(t *testing.T) {
	t.Parallel()
	k8s, testAgent := completeSetup(t)
	defer testAgent.Shutdown()
	require := require.New(t)

	// Create Kubernetes Service.
	_, err := k8s.CoreV1().Services(ns).Create(&v1.Service{
		Spec: v1.ServiceSpec{
			ClusterIP: "1.2.3.4",
		},
		ObjectMeta: v12.ObjectMeta{
			Name: "kubernetes",
		},
	})
	require.NoError(err)

	// Create ServiceAccount for the injector that the helm chart creates.
	_, err = k8s.CoreV1().ServiceAccounts(ns).Create(&v1.ServiceAccount{
		ObjectMeta: v12.ObjectMeta{
			Name: releaseName + "-consul-connect-injector-authmethod-svc-account",
		},
		Secrets: []v1.ObjectReference{
			{
				Name: releaseName + "-consul-connect-injector-authmethod-svc-accohndbv",
			},
		},
	})
	require.NoError(err)

	// Create the ServiceAccount Secret.
	caCertBytes, err := base64.StdEncoding.DecodeString(serviceAccountCACert)
	require.NoError(err)
	tokenBytes, err := base64.StdEncoding.DecodeString(serviceAccountToken)
	require.NoError(err)
	_, err = k8s.CoreV1().Secrets(ns).Create(&v1.Secret{
		ObjectMeta: v12.ObjectMeta{
			Name: releaseName + "-consul-connect-injector-authmethod-svc-accohndbv",
		},
		Data: map[string][]byte{
			"ca.crt": caCertBytes,
			"token":  tokenBytes,
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
	bindingRuleSelector := "serviceaccount.name!=default"
	responseCode := cmd.Run([]string{
		"-release-name=" + releaseName,
		"-k8s-namespace=" + ns,
		"-expected-replicas=1",
		"-create-inject-token",
		"-acl-binding-rule-selector=" + bindingRuleSelector,
	})
	require.Equal(0, responseCode, ui.ErrorWriter.String())

	// Check that the auth method was created.
	bootToken := getBootToken(t, k8s, releaseName)
	consul := testAgent.Client()
	authMethodName := releaseName + "-consul-k8s-auth-method"
	authMethod, _, err := consul.ACL().AuthMethodRead(authMethodName,
		&api.QueryOptions{Token: bootToken})
	require.NoError(err)
	require.Contains(authMethod.Config, "Host")
	require.Equal(authMethod.Config["Host"], "https://1.2.3.4:443")
	require.Contains(authMethod.Config, "CACert")
	require.Equal(authMethod.Config["CACert"], string(caCertBytes))
	require.Contains(authMethod.Config, "ServiceAccountJWT")
	require.Equal(authMethod.Config["ServiceAccountJWT"], string(tokenBytes))

	// Check that the binding rule was created.
	rules, _, err := consul.ACL().BindingRuleList(authMethodName, &api.QueryOptions{Token: bootToken})
	require.NoError(err)
	require.Len(rules, 1)
	require.Equal("service", string(rules[0].BindType))
	require.Equal("${serviceaccount.name}", rules[0].BindName)
	require.Equal(bindingRuleSelector, rules[0].Selector)
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
			"-release-name=" + releaseName,
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
			ObjectMeta: v12.ObjectMeta{
				Name: releaseName + "-consul-server-0",
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
	}()

	// Wait for the command to exit.
	select {
	case <-done:
		require.Equal(0, responseCode, ui.ErrorWriter.String())
	case <-time.After(2 * time.Second):
		require.FailNow("command did not exit after 2s")
	}

	// Test that the bootstrap kube secret is created.
	getBootToken(t, k8s, releaseName)

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
		ObjectMeta: v12.ObjectMeta{
			Name: releaseName + "-consul-server-0",
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
			"-release-name=" + releaseName,
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
	getBootToken(t, k8s, releaseName)

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

// Test that if already bootstrapped, we continue on to next steps.
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
		// If ACLs are already bootstrapped then the bootstrap endpoint returns this error.
		case "/v1/acl/bootstrap":
			w.WriteHeader(403)
			fmt.Fprintln(w, "Permission denied: rpc error making call: ACL bootstrap no longer allowed (reset index: 14)")
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
		ObjectMeta: v12.ObjectMeta{
			Name: releaseName + "-consul-server-0",
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

	// Create the bootstrap secret since this should have already been created.
	_, err = k8s.CoreV1().Secrets(ns).Create(&v1.Secret{
		ObjectMeta: v12.ObjectMeta{
			Name: releaseName + "-consul-bootstrap-acl-token",
		},
		Data: map[string][]byte{
			"token": []byte("bootstrap-token"),
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
		"-release-name=" + releaseName,
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
		ObjectMeta: v12.ObjectMeta{
			Name: releaseName + "-consul-server-0",
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

	// Run the command.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}
	cmd.init()
	responseCode := cmd.Run([]string{
		"-release-name=" + releaseName,
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

// Test if there is an old bootstrap Secret we update it.
func TestRun_BootstrapTokenExists(t *testing.T) {
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
		ObjectMeta: v12.ObjectMeta{
			Name: releaseName + "-consul-server-0",
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

	// Create the old bootstrap secret.
	_, err = k8s.CoreV1().Secrets(ns).Create(&v1.Secret{
		ObjectMeta: v12.ObjectMeta{
			Name: releaseName + "-consul-bootstrap-acl-token",
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
		"-release-name=" + releaseName,
		"-k8s-namespace=" + ns,
		"-expected-replicas=1",
	})
	require.Equal(0, responseCode, ui.ErrorWriter.String())

	// Test that the Secret was updated.
	secret, err := k8s.CoreV1().Secrets(ns).Get(releaseName+"-consul-bootstrap-acl-token", metav1.GetOptions{})
	require.NoError(err)
	require.Contains(secret.Data, "token")
	require.NotEqual("old-token", string(secret.Data["token"]))

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

// getBootToken gets the bootstrap token from the Kubernetes secret. It will
// cause a test failure if the Secret doesn't exist or is malformed.
func getBootToken(t *testing.T, k8s *fake.Clientset, releaseName string) string {
	bootstrapSecret, err := k8s.CoreV1().Secrets(ns).Get(fmt.Sprintf("%s-consul-bootstrap-acl-token", releaseName), metav1.GetOptions{})
	require.NoError(t, err)
	require.NotNil(t, bootstrapSecret)
	bootToken, ok := bootstrapSecret.Data["token"]
	require.True(t, ok)
	return string(bootToken)
}

var serviceAccountCACert = "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSURDekNDQWZPZ0F3SUJBZ0lRS3pzN05qbDlIczZYYzhFWG91MjVoekFOQmdrcWhraUc5dzBCQVFzRkFEQXYKTVMwd0t3WURWUVFERXlRMU9XVTJaR00wTVMweU1EaG1MVFF3T1RVdFlUSTRPUzB4Wm1NM01EQmhZekZqWXpndwpIaGNOTVRrd05qQTNNVEF4TnpNeFdoY05NalF3TmpBMU1URXhOek14V2pBdk1TMHdLd1lEVlFRREV5UTFPV1UyClpHTTBNUzB5TURobUxUUXdPVFV0WVRJNE9TMHhabU0zTURCaFl6RmpZemd3Z2dFaU1BMEdDU3FHU0liM0RRRUIKQVFVQUE0SUJEd0F3Z2dFS0FvSUJBUURaakh6d3FvZnpUcEdwYzBNZElDUzdldXZmdWpVS0UzUEMvYXBmREFnQgo0anpFRktBNzgvOStLVUd3L2MvMFNIZVNRaE4rYThnd2xIUm5BejFOSmNmT0lYeTRkd2VVdU9rQWlGeEg4cGh0CkVDd2tlTk83ejhEb1Y4Y2VtaW5DUkhHamFSbW9NeHBaN2cycFpBSk5aZVB4aTN5MWFOa0ZBWGU5Z1NVU2RqUloKUlhZa2E3d2gyQU85azJkbEdGQVlCK3Qzdld3SjZ0d2pHMFR0S1FyaFlNOU9kMS9vTjBFMDFMekJjWnV4a04xawo4Z2ZJSHk3Yk9GQ0JNMldURURXLzBhQXZjQVByTzhETHFESis2TWpjM3I3K3psemw4YVFzcGIwUzA4cFZ6a2k1CkR6Ly84M2t5dTBwaEp1aWo1ZUI4OFY3VWZQWHhYRi9FdFY2ZnZyTDdNTjRmQWdNQkFBR2pJekFoTUE0R0ExVWQKRHdFQi93UUVBd0lDQkRBUEJnTlZIUk1CQWY4RUJUQURBUUgvTUEwR0NTcUdTSWIzRFFFQkN3VUFBNElCQVFCdgpRc2FHNnFsY2FSa3RKMHpHaHh4SjUyTm5SVjJHY0lZUGVOM1p2MlZYZTNNTDNWZDZHMzJQVjdsSU9oangzS21BCi91TWg2TmhxQnpzZWtrVHowUHVDM3dKeU0yT0dvblZRaXNGbHF4OXNGUTNmVTJtSUdYQ2Ezd0M4ZS9xUDhCSFMKdzcvVmVBN2x6bWozVFFSRS9XMFUwWkdlb0F4bjliNkp0VDBpTXVjWXZQMGhYS1RQQldsbnpJaWphbVU1MHIyWQo3aWEwNjVVZzJ4VU41RkxYL3Z4T0EzeTRyanBraldvVlFjdTFwOFRaclZvTTNkc0dGV3AxMGZETVJpQUhUdk9ICloyM2pHdWs2cm45RFVIQzJ4UGozd0NUbWQ4U0dFSm9WMzFub0pWNWRWZVE5MHd1c1h6M3ZURzdmaWNLbnZIRlMKeHRyNVBTd0gxRHVzWWZWYUdIMk8KLS0tLS1FTkQgQ0VSVElGSUNBVEUtLS0tLQo="
var serviceAccountToken = "ZXlKaGJHY2lPaUpTVXpJMU5pSXNJbXRwWkNJNklpSjkuZXlKcGMzTWlPaUpyZFdKbGNtNWxkR1Z6TDNObGNuWnBZMlZoWTJOdmRXNTBJaXdpYTNWaVpYSnVaWFJsY3k1cGJ5OXpaWEoyYVdObFlXTmpiM1Z1ZEM5dVlXMWxjM0JoWTJVaU9pSmtaV1poZFd4MElpd2lhM1ZpWlhKdVpYUmxjeTVwYnk5elpYSjJhV05sWVdOamIzVnVkQzl6WldOeVpYUXVibUZ0WlNJNkltdG9ZV3RwTFdGeVlXTm9ibWxrTFdOdmJuTjFiQzFqYjI1dVpXTjBMV2x1YW1WamRHOXlMV0YxZEdodFpYUm9iMlF0YzNaakxXRmpZMjlvYm1SaWRpSXNJbXQxWW1WeWJtVjBaWE11YVc4dmMyVnlkbWxqWldGalkyOTFiblF2YzJWeWRtbGpaUzFoWTJOdmRXNTBMbTVoYldVaU9pSnJhR0ZyYVMxaGNtRmphRzVwWkMxamIyNXpkV3d0WTI5dWJtVmpkQzFwYm1wbFkzUnZjaTFoZFhSb2JXVjBhRzlrTFhOMll5MWhZMk52ZFc1MElpd2lhM1ZpWlhKdVpYUmxjeTVwYnk5elpYSjJhV05sWVdOamIzVnVkQzl6WlhKMmFXTmxMV0ZqWTI5MWJuUXVkV2xrSWpvaU4yVTVOV1V4TWprdFpUUTNNeTB4TVdVNUxUaG1ZV0V0TkRJd01UQmhPREF3TVRJeUlpd2ljM1ZpSWpvaWMzbHpkR1Z0T25ObGNuWnBZMlZoWTJOdmRXNTBPbVJsWm1GMWJIUTZhMmhoYTJrdFlYSmhZMmh1YVdRdFkyOXVjM1ZzTFdOdmJtNWxZM1F0YVc1cVpXTjBiM0l0WVhWMGFHMWxkR2h2WkMxemRtTXRZV05qYjNWdWRDSjkuWWk2M01NdHpoNU1CV0tLZDNhN2R6Q0pqVElURTE1aWtGeV9UbnBka19Bd2R3QTlKNEFNU0dFZUhONXZXdEN1dUZqb19sTUpxQkJQSGtLMkFxYm5vRlVqOW01Q29wV3lxSUNKUWx2RU9QNGZVUS1SYzBXMVBfSmpVMXJaRVJIRzM5YjVUTUxnS1BRZ3V5aGFpWkVKNkNqVnRtOXdVVGFncmdpdXFZVjJpVXFMdUY2U1lObTZTckt0a1BTLWxxSU8tdTdDMDZ3Vms1bTV1cXdJVlFOcFpTSUNfNUxzNWFMbXlaVTNuSHZILVY3RTNIbUJoVnlaQUI3NmpnS0IwVHlWWDFJT3NrdDlQREZhck50VTNzdVp5Q2p2cUMtVUpBNnNZZXlTZTRkQk5Lc0tsU1o2WXV4VVVtbjFSZ3YzMllNZEltbnNXZzhraGYtekp2cWdXazdCNUVB"
