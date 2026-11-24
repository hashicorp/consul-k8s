// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package configentries

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/hashicorp/consul-server-connection-manager/discovery"

	capi "github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	consulv1alpha1 "github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/controllers/helmvalues"
	"github.com/hashicorp/consul-k8s/control-plane/helper/cert"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

func TestTerminatingGatewayController_transformSecret(t *testing.T) {
	t.Parallel()

	s := runtime.NewScheme()
	require.NoError(t, consulv1alpha1.AddToScheme(s))
	require.NoError(t, corev1.AddToScheme(s))

	gwWithSecret := &consulv1alpha1.TerminatingGateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw-trigger-secret-rotation", Namespace: "default"},
		Spec: consulv1alpha1.TerminatingGatewaySpec{
			Services: []consulv1alpha1.LinkedService{
				{Name: "api", SecretRef: &consulv1alpha1.SecretReference{Name: "tls-secret"}},
				{Name: "api-dup", SecretRef: &consulv1alpha1.SecretReference{Name: "tls-secret"}},
			},
		},
	}
	gwWithoutSecret := &consulv1alpha1.TerminatingGateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw-without-secret", Namespace: "default"},
		Spec:       consulv1alpha1.TerminatingGatewaySpec{Services: []consulv1alpha1.LinkedService{{Name: "db"}}},
	}
	gwOtherNamespace := &consulv1alpha1.TerminatingGateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw-other-ns", Namespace: "other"},
		Spec: consulv1alpha1.TerminatingGatewaySpec{
			Services: []consulv1alpha1.LinkedService{{Name: "api", SecretRef: &consulv1alpha1.SecretReference{Name: "tls-secret"}}},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(gwWithSecret, gwWithoutSecret, gwOtherNamespace).
		WithIndex(&consulv1alpha1.TerminatingGateway{}, secretOwnerKey, termGWSecretIndexer).
		Build()
	controller := &TerminatingGatewayController{Client: fakeClient}

	requests := controller.transformSecret(context.Background(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "tls-secret", Namespace: "default"}})
	require.Equal(t, []reconcile.Request{{NamespacedName: types.NamespacedName{Name: secretTriggerPrefix + "gw-trigger-secret-rotation", Namespace: "default"}}}, requests)

	requests = controller.transformSecret(context.Background(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "non-matching", Namespace: "default"}})
	require.Empty(t, requests)
}

func TestTerminatingGatewayController_conditionallyDeletePolicies_MissingPolicy(t *testing.T) {
	t.Parallel()

	const policyName = "default-db-write-policy"
	requests := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/acl/policy/name/"+policyName {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		requests <- r.URL.Path
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	config := capi.DefaultConfig()
	config.Address = server.URL
	consulClient, err := capi.NewClient(config)
	require.NoError(t, err)

	scheme := runtime.NewScheme()
	require.NoError(t, consulv1alpha1.AddToScheme(scheme))
	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithIndex(&consulv1alpha1.TerminatingGateway{}, terminatingGatewayByLinkedServiceName, termGWLinkedServiceIndexer).
		Build()
	controller := &TerminatingGatewayController{Client: kubeClient}

	require.NotPanics(t, func() {
		err = controller.conditionallyDeletePolicies(logr.Discard(), consulClient,
			[]*capi.ACLRolePolicyLink{{Name: policyName}}, "terminating-gateway-2")
	})
	require.NoError(t, err)
	require.Len(t, requests, 1)
}

func TestTerminatingGatewayController_termGWSecretIndexer(t *testing.T) {
	t.Parallel()

	indexed := termGWSecretIndexer(&consulv1alpha1.TerminatingGateway{
		Spec: consulv1alpha1.TerminatingGatewaySpec{
			Services: []consulv1alpha1.LinkedService{
				{Name: "api", SecretRef: &consulv1alpha1.SecretReference{Name: "tls-a"}},
				{Name: "db"},
				{Name: "metrics", SecretRef: &consulv1alpha1.SecretReference{Name: "tls-b"}},
				{Name: "empty", SecretRef: &consulv1alpha1.SecretReference{}},
			},
		},
	})

	require.Equal(t, []string{"tls-a", "tls-b"}, indexed)
}

func ciGateway(name, namespace string, ci *consulv1alpha1.TerminatingGatewayCredentialInjection) *consulv1alpha1.TerminatingGateway {
	return &consulv1alpha1.TerminatingGateway{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: consulv1alpha1.TerminatingGatewaySpec{
			Deployment: consulv1alpha1.TerminatingGatewayDeploymentSpec{CredentialInjection: ci},
		},
	}
}

func TestTerminatingGatewayController_termGWConfigMapIndexer(t *testing.T) {
	t.Parallel()

	// Enabled gateway with all three credential ConfigMaps set (plus one empty).
	indexed := termGWConfigMapIndexer(ciGateway("gw", "default", &consulv1alpha1.TerminatingGatewayCredentialInjection{
		Enabled:             true,
		ProcessorConfigMap:  "camp-proc",
		VaultAgentConfigMap: "camp-agent",
		VaultCAConfigMap:    "camp-ca",
	}))
	require.Equal(t, []string{"camp-proc", "camp-agent", "camp-ca"}, indexed)

	// Disabled credential injection contributes no index keys.
	require.Nil(t, termGWConfigMapIndexer(ciGateway("gw", "default", &consulv1alpha1.TerminatingGatewayCredentialInjection{
		Enabled:            false,
		ProcessorConfigMap: "camp-proc",
	})))

	// No credential injection block at all contributes no index keys.
	require.Nil(t, termGWConfigMapIndexer(ciGateway("gw", "default", nil)))
}

func TestTerminatingGatewayController_transformConfigMap(t *testing.T) {
	t.Parallel()

	s := runtime.NewScheme()
	require.NoError(t, consulv1alpha1.AddToScheme(s))
	require.NoError(t, corev1.AddToScheme(s))

	gwReferencing := ciGateway("gw-referencing", "default", &consulv1alpha1.TerminatingGatewayCredentialInjection{
		Enabled:             true,
		ProcessorConfigMap:  "camp-proc",
		VaultAgentConfigMap: "camp-agent",
	})
	gwOtherConfigMap := ciGateway("gw-other-cm", "default", &consulv1alpha1.TerminatingGatewayCredentialInjection{
		Enabled:            true,
		ProcessorConfigMap: "unrelated",
	})
	// Same ConfigMap name but a different namespace: must NOT be enqueued.
	gwOtherNamespace := ciGateway("gw-other-ns", "other", &consulv1alpha1.TerminatingGatewayCredentialInjection{
		Enabled:            true,
		ProcessorConfigMap: "camp-proc",
	})

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(gwReferencing, gwOtherConfigMap, gwOtherNamespace).
		WithIndex(&consulv1alpha1.TerminatingGateway{}, configMapOwnerKey, termGWConfigMapIndexer).
		Build()
	controller := &TerminatingGatewayController{Client: fakeClient}

	// A referenced ConfigMap enqueues only the same-namespace referencing gateway
	// (no secretTriggerPrefix, so a normal reconcile recomputes the checksum).
	requests := controller.transformConfigMap(context.Background(), &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "camp-proc", Namespace: "default"}})
	require.Equal(t, []reconcile.Request{{NamespacedName: types.NamespacedName{Name: "gw-referencing", Namespace: "default"}}}, requests)

	// A ConfigMap no gateway references enqueues nothing.
	requests = controller.transformConfigMap(context.Background(), &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "non-matching", Namespace: "default"}})
	require.Empty(t, requests)
}

func TestTerminatingGatewayController_ReconcileSecretTriggerPrefixDoesNotTrimGatewayName(t *testing.T) {
	t.Parallel()

	s := runtime.NewScheme()
	require.NoError(t, consulv1alpha1.AddToScheme(s))
	require.NoError(t, corev1.AddToScheme(s))

	kubeNS := "default"
	kubeName := "gw-trigger-secret-rotation"
	secretName := "tls-secret"
	metaKey := fmt.Sprintf("consul.hashicorp.com/secret/%s/last-rotation", secretName)

	termGW := &consulv1alpha1.TerminatingGateway{
		ObjectMeta: metav1.ObjectMeta{Name: kubeName, Namespace: kubeNS},
		Spec: consulv1alpha1.TerminatingGatewaySpec{
			Services: []consulv1alpha1.LinkedService{
				{Name: "svc", SecretRef: &consulv1alpha1.SecretReference{Name: secretName}},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(termGW, terminatingGatewayHelmValuesConfigMap(kubeNS)).
		WithStatusSubresource(termGW).
		Build()

	testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
	testClient.TestServer.WaitForServiceIntentions(t)
	consulClient := testClient.APIClient

	reconciler := &TerminatingGatewayController{
		Client:           fakeClient,
		ReleaseName:      "consul",
		ReleaseNamespace: kubeNS,
		ConfigEntryController: &ConfigEntryController{
			ConsulClientConfig:  testClient.Cfg,
			ConsulServerConnMgr: testClient.Watcher,
			DatacenterName:      datacenterName,
		},
	}

	secretReq := reconcile.Request{NamespacedName: types.NamespacedName{Name: secretTriggerPrefix + kubeName, Namespace: kubeNS}}
	resp, err := reconciler.Reconcile(context.Background(), secretReq)
	require.NoError(t, err)
	require.False(t, resp.Requeue)

	entry, _, err := consulClient.ConfigEntries().Get(capi.TerminatingGateway, kubeName, nil)
	require.NoError(t, err)

	tgEntry, ok := entry.(*capi.TerminatingGatewayConfigEntry)
	require.True(t, ok)
	require.Equal(t, kubeName, tgEntry.Name)
	_, ok = tgEntry.Meta[metaKey]
	require.True(t, ok)
}

func TestTerminatingGatewayController_MutateConsulEntry(t *testing.T) {
	t.Parallel()

	controller := &TerminatingGatewayController{}
	obj := &consulv1alpha1.TerminatingGateway{
		Spec: consulv1alpha1.TerminatingGatewaySpec{
			Services: []consulv1alpha1.LinkedService{
				{Name: "svc-a", SecretRef: &consulv1alpha1.SecretReference{Name: "tls-a"}},
				{Name: "svc-b"},
				{Name: "svc-c", SecretRef: &consulv1alpha1.SecretReference{Name: "tls-b"}},
				{Name: "svc-d", SecretRef: &consulv1alpha1.SecretReference{Name: "tls-a"}},
			},
		},
	}

	entry := &capi.TerminatingGatewayConfigEntry{Meta: map[string]string{"existing": "value"}}
	err := controller.MutateConsulEntry(obj, entry, reconcile.Request{})
	require.NoError(t, err)

	require.Equal(t, "value", entry.Meta["existing"])
	for _, secret := range []string{"tls-a", "tls-b"} {
		k := fmt.Sprintf("consul.hashicorp.com/secret/%s/last-rotation", secret)
		v, ok := entry.Meta[k]
		require.True(t, ok, "expected metadata key for secret %q", secret)
		_, parseErr := time.Parse(time.RFC3339Nano, v)
		require.NoError(t, parseErr)
	}

	err = controller.MutateConsulEntry(obj, &capi.ServiceConfigEntry{}, reconcile.Request{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "expected TerminatingGatewayConfigEntry")
}

// TestTerminatingGatewayController_terminatingGatewayConsulNamespace verifies the
// resolution order for the Consul namespace a terminating gateway registers into,
// including the namespace-mirroring fallback used when Admin Partitions are enabled.
func TestTerminatingGatewayController_terminatingGatewayConsulNamespace(t *testing.T) {
	t.Parallel()

	const kubeNS = "consul"

	newTermGW := func(specNS, gatewayName string) *consulv1alpha1.TerminatingGateway {
		return &consulv1alpha1.TerminatingGateway{
			ObjectMeta: metav1.ObjectMeta{Name: "terminating-gateway", Namespace: kubeNS},
			Spec: consulv1alpha1.TerminatingGatewaySpec{
				Deployment: consulv1alpha1.TerminatingGatewayDeploymentSpec{
					ConsulNamespace: specNS,
					GatewayName:     gatewayName,
				},
			},
		}
	}

	cases := map[string]struct {
		termGW     *consulv1alpha1.TerminatingGateway
		helm       *helmvalues.HelmValues
		cec        *ConfigEntryController
		expectedNS string
	}{
		"explicit spec.deployment.consulNamespace wins": {
			termGW: newTermGW("explicit-ns", ""),
			helm:   &helmvalues.HelmValues{},
			cec: &ConfigEntryController{
				EnableConsulNamespaces: true,
				EnableNSMirroring:      true,
			},
			expectedNS: "explicit-ns",
		},
		"per-gateway helm override wins over defaults": {
			termGW: newTermGW("", ""),
			helm: &helmvalues.HelmValues{
				TerminatingGateways: helmvalues.TerminatingGatewaysConfig{
					Defaults: helmvalues.Defaults{ConsulNamespace: "defaults-ns"},
					Gateways: []helmvalues.Gateway{{Name: "terminating-gateway", ConsulNamespace: "per-gw-ns"}},
				},
			},
			cec:        &ConfigEntryController{EnableConsulNamespaces: true},
			expectedNS: "per-gw-ns",
		},
		"non-default defaults.consulNamespace is respected": {
			termGW: newTermGW("", ""),
			helm: &helmvalues.HelmValues{
				TerminatingGateways: helmvalues.TerminatingGatewaysConfig{
					Defaults: helmvalues.Defaults{ConsulNamespace: "defaults-ns"},
				},
			},
			cec: &ConfigEntryController{
				EnableConsulNamespaces: true,
				EnableNSMirroring:      true,
			},
			expectedNS: "defaults-ns",
		},
		"mirroring maps to the K8s namespace when defaults is the default sentinel": {
			termGW: newTermGW("", ""),
			helm: &helmvalues.HelmValues{
				TerminatingGateways: helmvalues.TerminatingGatewaysConfig{
					Defaults: helmvalues.Defaults{ConsulNamespace: "default"},
				},
			},
			cec: &ConfigEntryController{
				EnableConsulNamespaces: true,
				EnableNSMirroring:      true,
			},
			expectedNS: kubeNS,
		},
		"mirroring with prefix maps to prefixed K8s namespace": {
			termGW: newTermGW("", ""),
			helm:   &helmvalues.HelmValues{},
			cec: &ConfigEntryController{
				EnableConsulNamespaces: true,
				EnableNSMirroring:      true,
				NSMirroringPrefix:      "k8s-",
			},
			expectedNS: "k8s-" + kubeNS,
		},
		"default when mirroring disabled even if destination namespace is set": {
			termGW: newTermGW("", ""),
			helm:   &helmvalues.HelmValues{},
			cec: &ConfigEntryController{
				EnableConsulNamespaces:     true,
				ConsulDestinationNamespace: "dest-ns",
			},
			expectedNS: "default",
		},
		"falls back to default when namespaces disabled": {
			termGW:     newTermGW("", ""),
			helm:       &helmvalues.HelmValues{},
			cec:        &ConfigEntryController{EnableConsulNamespaces: false},
			expectedNS: "default",
		},
	}

	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := &TerminatingGatewayController{ConfigEntryController: tc.cec}
			require.Equal(t, tc.expectedNS, r.terminatingGatewayConsulNamespace(tc.termGW, tc.helm))
		})
	}
}

func TestCredentialInjectionAIGateError(t *testing.T) {
	t.Parallel()

	plain := &consulv1alpha1.TerminatingGateway{
		Spec: consulv1alpha1.TerminatingGatewaySpec{Services: []consulv1alpha1.LinkedService{{Name: "web"}}},
	}
	credentialed := &consulv1alpha1.TerminatingGateway{
		Spec: consulv1alpha1.TerminatingGatewaySpec{
			CredentialInjection: &consulv1alpha1.TerminatingGatewayCredentialRouting{},
			Services: []consulv1alpha1.LinkedService{{
				Name:       "openai",
				Credential: &consulv1alpha1.LinkedServiceCredential{Mode: consulv1alpha1.CredentialModeInject, BindingID: "openai-a"},
			}},
		},
	}
	deleting := credentialed.DeepCopy()
	now := metav1.Now()
	deleting.DeletionTimestamp = &now

	aiOff := &helmvalues.HelmValues{}
	aiOn := &helmvalues.HelmValues{AI: helmvalues.AIConfig{Enabled: true}}

	require.NoError(t, credentialInjectionAIGateError(plain, aiOff), "gateways without credential injection never depend on ai.enabled")
	require.ErrorContains(t, credentialInjectionAIGateError(credentialed, aiOff), "requires ai.enabled=true")
	require.NoError(t, credentialInjectionAIGateError(credentialed, aiOn))
	require.NoError(t, credentialInjectionAIGateError(deleting, aiOff), "deleting a gateway must never be blocked")
}

// TestTerminatingGatewayController_ReconcileRejectsCredentialInjectionWithoutAI
// verifies a credential-injection gateway is rejected with a status condition
// before anything is written to Consul when ai.enabled is false.
func TestTerminatingGatewayController_ReconcileRejectsCredentialInjectionWithoutAI(t *testing.T) {
	t.Parallel()

	s := runtime.NewScheme()
	require.NoError(t, consulv1alpha1.AddToScheme(s))
	require.NoError(t, corev1.AddToScheme(s))

	kubeNS := "default"
	termGW := &consulv1alpha1.TerminatingGateway{
		ObjectMeta: metav1.ObjectMeta{Name: "camp-egress", Namespace: kubeNS},
		Spec: consulv1alpha1.TerminatingGatewaySpec{
			CredentialInjection: &consulv1alpha1.TerminatingGatewayCredentialRouting{},
			Services: []consulv1alpha1.LinkedService{{
				Name:       "openai",
				CAFile:     "/etc/ssl/ca.pem",
				SNI:        "api.openai.com",
				Credential: &consulv1alpha1.LinkedServiceCredential{Mode: consulv1alpha1.CredentialModeInject, BindingID: "openai-a"},
			}},
		},
	}

	// The helm-values ConfigMap omits ai, so ai.enabled is false.
	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(termGW, terminatingGatewayHelmValuesConfigMap(kubeNS)).
		WithStatusSubresource(termGW).
		Build()

	// No Consul client is configured: reaching Consul would panic, proving the
	// gateway is rejected before any config entry or ACL write.
	reconciler := &TerminatingGatewayController{
		Client:                fakeClient,
		ReleaseName:           "consul",
		ReleaseNamespace:      kubeNS,
		ConfigEntryController: &ConfigEntryController{},
	}

	_, err := reconciler.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: termGW.Name, Namespace: kubeNS},
	})
	require.ErrorContains(t, err, "requires ai.enabled=true")

	updated := &consulv1alpha1.TerminatingGateway{}
	require.NoError(t, fakeClient.Get(context.Background(), types.NamespacedName{Name: termGW.Name, Namespace: kubeNS}, updated))
	status, reason, message := updated.SyncedCondition()
	require.Equal(t, corev1.ConditionFalse, status)
	require.Equal(t, "CredentialInjectionRequiresAI", reason)
	require.Contains(t, message, "consul-ai feature gate")
}

// Test that the controller scopes the terminating gateway binding rule to the Helm release
// namespace, and does not overwrite a namespaced rule written by server-acl-init with a
// name-only selector.
func TestTerminatingGatewayController_ensureACLBootstrapScopesBindingRuleToReleaseNamespace(t *testing.T) {
	t.Parallel()

	const (
		releaseNS = "consul"
		gwName    = "terminating-gateway"
		saName    = "consul-terminating-gateway"
		roleName  = "consul-terminating-gateway-acl-role"
		authName  = "consul-k8s-component-auth-method"
		ruleID    = "existing-rule-id"
	)

	var written *capi.ACLBindingRule
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && (strings.HasPrefix(r.URL.Path, "/v1/acl/policy/name/") ||
			strings.HasPrefix(r.URL.Path, "/v1/acl/role/name/")):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPut && (r.URL.Path == "/v1/acl/policy" || r.URL.Path == "/v1/acl/role"):
			fmt.Fprint(w, `{"ID":"id"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/acl/binding-rules":
			// The rule as written by server-acl-init.
			require.NoError(t, json.NewEncoder(w).Encode([]capi.ACLBindingRule{{
				ID:          ruleID,
				Description: "Binding Rule for " + saName,
				AuthMethod:  authName,
				Selector:    fmt.Sprintf("serviceaccount.name==%q and serviceaccount.namespace==%q", saName, releaseNS),
				BindType:    capi.BindingRuleBindTypeRole,
				BindName:    roleName,
			}}))
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/v1/acl/binding-rule"):
			written = &capi.ACLBindingRule{}
			require.NoError(t, json.NewDecoder(r.Body).Decode(written))
			fmt.Fprint(w, `{"ID":"`+ruleID+`"}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer svr.Close()

	addr := svr.Listener.Addr().(*net.TCPAddr)
	connMgr := &consul.MockServerConnectionManager{}
	connMgr.On("State").Return(discovery.State{Address: discovery.Addr{TCPAddr: *addr}}, nil)

	controller := &TerminatingGatewayController{
		ReleaseNamespace: releaseNS,
		ConfigEntryController: &ConfigEntryController{
			ConsulClientConfig: &consul.Config{
				APIClientConfig: &capi.Config{},
				HTTPPort:        addr.Port,
			},
			ConsulServerConnMgr: connMgr,
		},
	}

	helmValues := &helmvalues.HelmValues{
		Release: helmvalues.ReleaseConfig{Name: "consul", Namespace: releaseNS},
		Global: helmvalues.GlobalConfig{
			Name: "consul",
			ACLs: helmvalues.ACLsConfig{ManageSystemACLs: true},
		},
	}
	termGW := &consulv1alpha1.TerminatingGateway{
		ObjectMeta: metav1.ObjectMeta{Name: gwName, Namespace: releaseNS},
	}

	require.NoError(t, controller.ensureTerminatingGatewayACLBootstrap(logr.Discard(), termGW, helmValues))
	require.NotNil(t, written)
	require.Equal(t, fmt.Sprintf("serviceaccount.name==%q and serviceaccount.namespace==%q", saName, releaseNS), written.Selector)
}

func TestTerminatingGatewayController_conditionallyDeletePolicies_PolicyReadError(t *testing.T) {
	t.Parallel()

	const (
		failingPolicy = "default-db-write-policy"
		missingPolicy = "default-cache-write-policy"
	)
	var (
		mu       sync.Mutex
		requests []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.Path)
		mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/acl/policy/name/"+failingPolicy:
			w.WriteHeader(http.StatusInternalServerError)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/acl/policy/name/"+missingPolicy:
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	config := capi.DefaultConfig()
	config.Address = server.URL
	consulClient, err := capi.NewClient(config)
	require.NoError(t, err)

	scheme := runtime.NewScheme()
	require.NoError(t, consulv1alpha1.AddToScheme(scheme))
	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithIndex(&consulv1alpha1.TerminatingGateway{}, terminatingGatewayByLinkedServiceName, termGWLinkedServiceIndexer).
		Build()
	controller := &TerminatingGatewayController{Client: kubeClient}

	require.NotPanics(t, func() {
		err = controller.conditionallyDeletePolicies(logr.Discard(), consulClient,
			[]*capi.ACLRolePolicyLink{{Name: failingPolicy}, {Name: missingPolicy}}, "terminating-gateway")
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), failingPolicy)
	require.ElementsMatch(t, []string{
		"GET /v1/acl/policy/name/" + failingPolicy,
		"GET /v1/acl/policy/name/" + missingPolicy,
	}, requests, "no policy is deleted, and the next policy is still processed")
}

const (
	tgwTestGatewayName = "terminating-gateway"
	tgwTestNamespace   = "consul"
	tgwTestRoleName    = "consul-terminating-gateway-acl-role"
	tgwTestGWPolicy    = "terminating-gateway-policy"
	tgwTestSvcAPolicy  = "default-svc-a-write-policy"
	tgwTestSvcBPolicy  = "default-svc-b-write-policy"
	tgwTestAuthMethod  = "consul-k8s-component-auth-method"
)

type tgwACLTestEnv struct {
	controller *TerminatingGatewayController
	consul     *capi.Client
	helmValues *helmvalues.HelmValues
}

// newTGWACLTestEnv starts a Consul test server with ACLs enabled and the
// component auth method, and returns a TerminatingGatewayController that uses
// it with the bootstrap token.
func newTGWACLTestEnv(t *testing.T) *tgwACLTestEnv {
	t.Helper()
	const bootToken = "11111111-2222-3333-4444-555555555555"
	testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
		c.ACL.Enabled = true
		c.ACL.Tokens.InitialManagement = bootToken
	})

	_, _, caPEM, _, err := cert.GenerateCA("kubernetes")
	require.NoError(t, err)
	_, _, err = testClient.APIClient.ACL().AuthMethodCreate(&capi.ACLAuthMethod{
		Name: tgwTestAuthMethod,
		Type: "kubernetes",
		Config: map[string]interface{}{
			"Host":              "https://kubernetes.example.com",
			"CACert":            caPEM,
			"ServiceAccountJWT": testUnsignedJWT(),
		},
	}, nil)
	require.NoError(t, err)

	connMgr := &consul.MockServerConnectionManager{}
	connMgr.On("State").Return(discovery.State{
		Address: discovery.Addr{TCPAddr: net.TCPAddr{
			IP:   net.ParseIP("127.0.0.1"),
			Port: testClient.TestServer.Config.Ports.GRPC,
		}},
		Token: bootToken,
	}, nil)

	scheme := runtime.NewScheme()
	require.NoError(t, consulv1alpha1.AddToScheme(scheme))
	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithIndex(&consulv1alpha1.TerminatingGateway{}, terminatingGatewayByLinkedServiceName, termGWLinkedServiceIndexer).
		Build()

	return &tgwACLTestEnv{
		controller: &TerminatingGatewayController{
			Client:           kubeClient,
			Log:              logr.Discard(),
			Scheme:           scheme,
			ReleaseNamespace: tgwTestNamespace,
			ConfigEntryController: &ConfigEntryController{
				ConsulClientConfig:  testClient.Cfg,
				ConsulServerConnMgr: connMgr,
			},
		},
		consul: testClient.APIClient,
		helmValues: &helmvalues.HelmValues{
			Release: helmvalues.ReleaseConfig{Name: "consul", Namespace: tgwTestNamespace},
			Global: helmvalues.GlobalConfig{
				Name: "consul",
				ACLs: helmvalues.ACLsConfig{ManageSystemACLs: true},
			},
		},
	}
}

// testUnsignedJWT returns a syntactically valid, unsigned JWT for auth method
// configuration in tests.
func testUnsignedJWT() string {
	enc := base64.RawURLEncoding
	return enc.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`)) + "." +
		enc.EncodeToString([]byte(`{"sub":"test"}`)) + "."
}

func newTestTerminatingGateway(services ...string) *consulv1alpha1.TerminatingGateway {
	termGW := &consulv1alpha1.TerminatingGateway{
		ObjectMeta: metav1.ObjectMeta{Name: tgwTestGatewayName, Namespace: tgwTestNamespace},
	}
	for _, svc := range services {
		termGW.Spec.Services = append(termGW.Spec.Services, consulv1alpha1.LinkedService{Name: svc})
	}
	return termGW
}

func (e *tgwACLTestEnv) createPolicies(t *testing.T, names ...string) {
	t.Helper()
	for _, name := range names {
		_, _, err := e.consul.ACL().PolicyCreate(&capi.ACLPolicy{
			Name:  name,
			Rules: `node_prefix "" { policy = "read" }`,
		}, nil)
		require.NoError(t, err)
	}
}

func (e *tgwACLTestEnv) createRole(t *testing.T, policies ...string) *capi.ACLRole {
	t.Helper()
	role := &capi.ACLRole{Name: tgwTestRoleName}
	for _, p := range policies {
		role.Policies = append(role.Policies, &capi.ACLRolePolicyLink{Name: p})
	}
	created, _, err := e.consul.ACL().RoleCreate(role, nil)
	require.NoError(t, err)
	return created
}

func (e *tgwACLTestEnv) rolePolicyNames(t *testing.T) []string {
	t.Helper()
	role, _, err := e.consul.ACL().RoleReadByName(tgwTestRoleName, nil)
	require.NoError(t, err)
	require.NotNil(t, role)
	var names []string
	for _, p := range role.Policies {
		require.NotNil(t, p)
		require.NotEmpty(t, p.Name)
		names = append(names, p.Name)
	}
	return names
}

func (e *tgwACLTestEnv) bindingRulesForRole(t *testing.T) []*capi.ACLBindingRule {
	t.Helper()
	rules, _, err := e.consul.ACL().BindingRuleList(tgwTestAuthMethod, nil)
	require.NoError(t, err)
	var matching []*capi.ACLBindingRule
	for _, rule := range rules {
		if rule.BindName == tgwTestRoleName {
			matching = append(matching, rule)
		}
	}
	return matching
}

// Regression test: when the gateway policy link is missing from the role,
// updateACls must not send an empty policy link to Consul, and it must still
// update the linked-service policy links.
func TestTerminatingGatewayController_updateACls_GatewayPolicyLinkMissing(t *testing.T) {
	t.Parallel()
	env := newTGWACLTestEnv(t)
	env.createPolicies(t, tgwTestGWPolicy, tgwTestSvcAPolicy)
	env.createRole(t, tgwTestSvcAPolicy)

	err := env.controller.updateACls(logr.Discard(), newTestTerminatingGateway("svc-a", "svc-b"))
	if err != nil {
		require.NotContains(t, err.Error(), `No such ACL policy with name ""`)
	}
	require.ElementsMatch(t, []string{tgwTestSvcAPolicy, tgwTestSvcBPolicy}, env.rolePolicyNames(t))
}

// The bootstrap creates a deleted gateway policy again and links it to the
// existing role. It keeps the linked-service policy links.
func TestTerminatingGatewayController_ensureACLBootstrap_RelinksDeletedGatewayPolicy(t *testing.T) {
	t.Parallel()
	env := newTGWACLTestEnv(t)
	env.createPolicies(t, tgwTestSvcAPolicy)
	existing := env.createRole(t, tgwTestSvcAPolicy)

	require.NoError(t, env.controller.ensureTerminatingGatewayACLBootstrap(
		logr.Discard(), newTestTerminatingGateway("svc-a"), env.helmValues))

	policy, _, err := env.consul.ACL().PolicyReadByName(tgwTestGWPolicy, nil)
	require.NoError(t, err)
	require.NotNil(t, policy)
	role, _, err := env.consul.ACL().RoleReadByName(tgwTestRoleName, nil)
	require.NoError(t, err)
	require.Equal(t, existing.ID, role.ID)
	require.ElementsMatch(t, []string{tgwTestSvcAPolicy, tgwTestGWPolicy}, env.rolePolicyNames(t))
}

// The bootstrap creates a deleted role and binding rule again.
func TestTerminatingGatewayController_ensureACLBootstrap_RecreatesRoleAndBindingRule(t *testing.T) {
	t.Parallel()
	env := newTGWACLTestEnv(t)

	require.NoError(t, env.controller.ensureTerminatingGatewayACLBootstrap(
		logr.Discard(), newTestTerminatingGateway("svc-a"), env.helmValues))

	require.ElementsMatch(t, []string{tgwTestGWPolicy}, env.rolePolicyNames(t))
	rules := env.bindingRulesForRole(t)
	require.Len(t, rules, 1)
	require.Equal(t, capi.BindingRuleBindTypeRole, rules[0].BindType)
}

// The bootstrap followed by updateACls, run twice, gives exactly one gateway
// policy link and one binding rule, and keeps the linked-service links.
func TestTerminatingGatewayController_ensureACLBootstrapThenUpdateACls_NoDuplicates(t *testing.T) {
	t.Parallel()
	env := newTGWACLTestEnv(t)
	termGW := newTestTerminatingGateway("svc-a")

	for i := 0; i < 2; i++ {
		require.NoError(t, env.controller.ensureTerminatingGatewayACLBootstrap(logr.Discard(), termGW, env.helmValues))
		require.NoError(t, env.controller.updateACls(logr.Discard(), termGW))
	}

	require.ElementsMatch(t, []string{tgwTestGWPolicy, tgwTestSvcAPolicy}, env.rolePolicyNames(t))
	require.Len(t, env.bindingRulesForRole(t), 1)
}

// With manageSystemACLs=false, the bootstrap does not create ACL objects.
func TestTerminatingGatewayController_ensureACLBootstrap_UnmanagedIsNoop(t *testing.T) {
	t.Parallel()
	env := newTGWACLTestEnv(t)
	env.helmValues.Global.ACLs.ManageSystemACLs = false

	require.NoError(t, env.controller.ensureTerminatingGatewayACLBootstrap(
		logr.Discard(), newTestTerminatingGateway("svc-a"), env.helmValues))

	policy, _, err := env.consul.ACL().PolicyReadByName(tgwTestGWPolicy, nil)
	require.NoError(t, err)
	require.Nil(t, policy)
	role, _, err := env.consul.ACL().RoleReadByName(tgwTestRoleName, nil)
	require.NoError(t, err)
	require.Nil(t, role)
}

// When the TerminatingGateway list fails, conditionallyDeletePolicies must
// return an error that names the service, and must not delete the policy.
func TestTerminatingGatewayController_conditionallyDeletePolicies_ListError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	config := capi.DefaultConfig()
	config.Address = server.URL
	consulClient, err := capi.NewClient(config)
	require.NoError(t, err)

	scheme := runtime.NewScheme()
	require.NoError(t, consulv1alpha1.AddToScheme(scheme))
	// No linked-service index, so the indexed list fails.
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	controller := &TerminatingGatewayController{Client: kubeClient}

	err = controller.conditionallyDeletePolicies(logr.Discard(), consulClient,
		[]*capi.ACLRolePolicyLink{{Name: "default-db-write-policy"}}, "terminating-gateway")
	require.Error(t, err)
	require.Contains(t, err.Error(), `failed to lookup terminating gateway list for service "db"`)
}

// When a policy delete fails, conditionallyDeletePolicies must return an error
// that names the policy.
func TestTerminatingGatewayController_conditionallyDeletePolicies_DeleteError(t *testing.T) {
	t.Parallel()

	const policyName = "default-db-write-policy"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/acl/policy/name/"+policyName:
			fmt.Fprintf(w, `{"ID":"policy-id","Name":%q}`, policyName)
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/acl/policy/policy-id":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	config := capi.DefaultConfig()
	config.Address = server.URL
	consulClient, err := capi.NewClient(config)
	require.NoError(t, err)

	scheme := runtime.NewScheme()
	require.NoError(t, consulv1alpha1.AddToScheme(scheme))
	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithIndex(&consulv1alpha1.TerminatingGateway{}, terminatingGatewayByLinkedServiceName, termGWLinkedServiceIndexer).
		Build()
	controller := &TerminatingGatewayController{Client: kubeClient}

	err = controller.conditionallyDeletePolicies(logr.Discard(), consulClient,
		[]*capi.ACLRolePolicyLink{{Name: policyName}}, "terminating-gateway")
	require.Error(t, err)
	require.Contains(t, err.Error(), `error delete policy "`+policyName+`"`)
}
