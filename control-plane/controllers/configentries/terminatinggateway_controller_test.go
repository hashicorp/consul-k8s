// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package configentries

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/go-logr/logr"
	"github.com/hashicorp/consul-server-connection-manager/discovery"
	capi "github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	consulv1alpha1 "github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

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
	tgwTestNamespace   = "default"
	tgwTestRoleName    = "consul-terminating-gateway-acl-role"
	tgwTestGWPolicy    = "terminating-gateway-policy"
	tgwTestSvcAPolicy  = "default-svc-a-write-policy"
	tgwTestSvcBPolicy  = "default-svc-b-write-policy"
)

type tgwACLTestEnv struct {
	controller *TerminatingGatewayController
	kubeClient client.Client
	consul     *capi.Client
}

// newTGWACLTestEnv starts a Consul test server with ACLs enabled and returns a
// TerminatingGatewayController whose server connection manager reports an ACL
// token, so that Reconcile runs the ACL logic.
func newTGWACLTestEnv(t *testing.T, objs ...client.Object) *tgwACLTestEnv {
	t.Helper()
	const bootToken = "11111111-2222-3333-4444-555555555555"
	testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
		c.ACL.Enabled = true
		c.ACL.Tokens.InitialManagement = bootToken
	})

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
		WithObjects(objs...).
		WithStatusSubresource(&consulv1alpha1.TerminatingGateway{}).
		WithIndex(&consulv1alpha1.TerminatingGateway{}, terminatingGatewayByLinkedServiceName, termGWLinkedServiceIndexer).
		Build()

	return &tgwACLTestEnv{
		controller: &TerminatingGatewayController{
			Client: kubeClient,
			Log:    logr.Discard(),
			Scheme: scheme,
			ConfigEntryController: &ConfigEntryController{
				ConsulClientConfig:  testClient.Cfg,
				ConsulServerConnMgr: connMgr,
				DatacenterName:      "dc1",
			},
		},
		kubeClient: kubeClient,
		consul:     testClient.APIClient,
	}
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

func (e *tgwACLTestEnv) reconcile(t *testing.T) error {
	t.Helper()
	_, err := e.controller.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: tgwTestNamespace, Name: tgwTestGatewayName},
	})
	return err
}

func (e *tgwACLTestEnv) conditions(t *testing.T) (aclStatus, synced *consulv1alpha1.Condition) {
	t.Helper()
	termGW := &consulv1alpha1.TerminatingGateway{}
	require.NoError(t, e.kubeClient.Get(context.Background(),
		types.NamespacedName{Namespace: tgwTestNamespace, Name: tgwTestGatewayName}, termGW))
	return termGW.Status.GetCondition(consulv1alpha1.ConsulACLStatus), termGW.Status.GetCondition(consulv1alpha1.ConditionSynced)
}

// When the gateway policy link is missing from the role, updateACls must not
// send an empty policy link to Consul, and it must return an error that names
// the missing link. It must not write the role at all: server-acl-init can link
// the gateway policy at the same time, and a write based on this read would
// remove that link again.
func TestTerminatingGatewayController_updateACls_GatewayPolicyLinkMissing(t *testing.T) {
	t.Parallel()
	env := newTGWACLTestEnv(t)
	env.createPolicies(t, tgwTestGWPolicy, tgwTestSvcAPolicy)
	before := env.createRole(t, tgwTestSvcAPolicy)

	err := env.controller.updateACls(logr.Discard(), newTestTerminatingGateway("svc-a", "svc-b"))
	require.Error(t, err)
	require.NotContains(t, err.Error(), `No such ACL policy with name ""`)
	require.Contains(t, err.Error(), tgwTestGWPolicy)
	require.Contains(t, err.Error(), tgwTestRoleName)

	after, _, err := env.consul.ACL().RoleRead(before.ID, nil)
	require.NoError(t, err)
	require.Equal(t, before.ModifyIndex, after.ModifyIndex, "the role must not be written while the gateway policy link is missing")
	require.ElementsMatch(t, []string{tgwTestSvcAPolicy}, env.rolePolicyNames(t))
}

// Reconcile must report the missing gateway policy link on the CR with a clear
// reason, and it must return an error so that the controller retries.
func TestTerminatingGatewayController_Reconcile_GatewayPolicyLinkMissing(t *testing.T) {
	t.Parallel()
	env := newTGWACLTestEnv(t, newTestTerminatingGateway("svc-a"))
	env.createPolicies(t, tgwTestGWPolicy, tgwTestSvcAPolicy)
	env.createRole(t, tgwTestSvcAPolicy)

	err := env.reconcile(t)
	require.Error(t, err)

	aclStatus, synced := env.conditions(t)
	require.NotNil(t, aclStatus)
	require.Equal(t, corev1.ConditionFalse, aclStatus.Status)
	require.Equal(t, "GatewayPolicyNotLinked", aclStatus.Reason)
	require.Contains(t, aclStatus.Message, tgwTestRoleName)
	require.Contains(t, aclStatus.Message, tgwTestGWPolicy)
	require.Contains(t, aclStatus.Message, "server-acl-init")
	require.NotNil(t, synced)
	require.Equal(t, corev1.ConditionFalse, synced.Status)
	require.Equal(t, "GatewayPolicyNotLinked", synced.Reason)
}

// After an operator links the gateway policy again, the next reconcile must
// succeed without a change to the CR.
func TestTerminatingGatewayController_Reconcile_RecoversAfterGatewayPolicyRelinked(t *testing.T) {
	t.Parallel()
	env := newTGWACLTestEnv(t, newTestTerminatingGateway("svc-a"))
	env.createPolicies(t, tgwTestGWPolicy, tgwTestSvcAPolicy)
	role := env.createRole(t, tgwTestSvcAPolicy)

	require.Error(t, env.reconcile(t))

	role, _, err := env.consul.ACL().RoleRead(role.ID, nil)
	require.NoError(t, err)
	role.Policies = append(role.Policies, &capi.ACLRolePolicyLink{Name: tgwTestGWPolicy})
	_, _, err = env.consul.ACL().RoleUpdate(role, nil)
	require.NoError(t, err)

	require.NoError(t, env.reconcile(t))
	aclStatus, synced := env.conditions(t)
	require.Equal(t, corev1.ConditionTrue, aclStatus.Status)
	require.Equal(t, corev1.ConditionTrue, synced.Status)
	require.ElementsMatch(t, []string{tgwTestSvcAPolicy, tgwTestGWPolicy}, env.rolePolicyNames(t))
}

// With all prerequisites in place, Reconcile links the services, keeps the
// gateway policy link, and writes the config entry.
func TestTerminatingGatewayController_Reconcile_GatewayPolicyLinked(t *testing.T) {
	t.Parallel()
	env := newTGWACLTestEnv(t, newTestTerminatingGateway("svc-a", "svc-b"))
	env.createPolicies(t, tgwTestGWPolicy, tgwTestSvcAPolicy)
	env.createRole(t, tgwTestGWPolicy, tgwTestSvcAPolicy)

	require.NoError(t, env.reconcile(t))

	aclStatus, synced := env.conditions(t)
	require.Equal(t, corev1.ConditionTrue, aclStatus.Status)
	require.Equal(t, corev1.ConditionTrue, synced.Status)
	require.ElementsMatch(t, []string{tgwTestGWPolicy, tgwTestSvcAPolicy, tgwTestSvcBPolicy}, env.rolePolicyNames(t))

	entry, _, err := env.consul.ConfigEntries().Get(capi.TerminatingGateway, tgwTestGatewayName, nil)
	require.NoError(t, err)
	require.Len(t, entry.(*capi.TerminatingGatewayConfigEntry).Services, 2)
}

// When the gateway role is missing, the error must name the gateway and the
// component that creates the role.
func TestTerminatingGatewayController_Reconcile_RoleNotFound(t *testing.T) {
	t.Parallel()
	env := newTGWACLTestEnv(t, newTestTerminatingGateway("svc-a"))

	err := env.reconcile(t)
	require.Error(t, err)
	require.Contains(t, err.Error(), `gateway "terminating-gateway"`)
	require.Contains(t, err.Error(), "server-acl-init")

	aclStatus, _ := env.conditions(t)
	require.Equal(t, corev1.ConditionFalse, aclStatus.Status)
	require.Equal(t, consulv1alpha1.TerminatingGatewayFailedToSetACLs, aclStatus.Reason)
}

// server-acl-init and the controller both read, change, and write the
// terminating gateway role, and Consul has no compare-and-set for roles. If
// server-acl-init writes a role that it read before the controller linked a new
// service, the new link is lost. The controller must schedule an ACL resync so
// that the next reconcile links the service again, without a change to the CR.
func TestTerminatingGatewayController_Reconcile_ACLResyncRestoresLinkRemovedByStaleRoleWrite(t *testing.T) {
	t.Parallel()
	env := newTGWACLTestEnv(t, newTestTerminatingGateway("svc-a"))
	env.createPolicies(t, tgwTestGWPolicy)
	env.createRole(t, tgwTestGWPolicy)

	result, err := env.controller.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: tgwTestNamespace, Name: tgwTestGatewayName},
	})
	require.NoError(t, err)
	require.Equal(t, terminatingGatewayACLResyncPeriod, result.RequeueAfter)
	require.ElementsMatch(t, []string{tgwTestGWPolicy, tgwTestSvcAPolicy}, env.rolePolicyNames(t))

	// server-acl-init reads the role.
	staleRole, _, err := env.consul.ACL().RoleReadByName(tgwTestRoleName, nil)
	require.NoError(t, err)

	// The controller links a new service.
	termGW := &consulv1alpha1.TerminatingGateway{}
	require.NoError(t, env.kubeClient.Get(context.Background(),
		types.NamespacedName{Namespace: tgwTestNamespace, Name: tgwTestGatewayName}, termGW))
	termGW.Spec.Services = append(termGW.Spec.Services, consulv1alpha1.LinkedService{Name: "svc-b"})
	require.NoError(t, env.kubeClient.Update(context.Background(), termGW))
	require.NoError(t, env.reconcile(t))
	require.ElementsMatch(t, []string{tgwTestGWPolicy, tgwTestSvcAPolicy, tgwTestSvcBPolicy}, env.rolePolicyNames(t))

	// server-acl-init writes the role that it read earlier.
	_, _, err = env.consul.ACL().RoleUpdate(staleRole, nil)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{tgwTestGWPolicy, tgwTestSvcAPolicy}, env.rolePolicyNames(t),
		"precondition: the stale write removed the svc-b link")

	// The scheduled resync links svc-b again.
	result, err = env.controller.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: tgwTestNamespace, Name: tgwTestGatewayName},
	})
	require.NoError(t, err)
	require.Equal(t, terminatingGatewayACLResyncPeriod, result.RequeueAfter)
	require.ElementsMatch(t, []string{tgwTestGWPolicy, tgwTestSvcAPolicy, tgwTestSvcBPolicy}, env.rolePolicyNames(t))
}

// A periodic resync must not write the role when its links did not change.
func TestTerminatingGatewayController_updateACls_SkipsUnchangedRoleWrite(t *testing.T) {
	t.Parallel()
	env := newTGWACLTestEnv(t)
	env.createPolicies(t, tgwTestGWPolicy, tgwTestSvcAPolicy)
	before := env.createRole(t, tgwTestGWPolicy, tgwTestSvcAPolicy)

	require.NoError(t, env.controller.updateACls(logr.Discard(), newTestTerminatingGateway("svc-a")))

	after, _, err := env.consul.ACL().RoleRead(before.ID, nil)
	require.NoError(t, err)
	require.Equal(t, before.ModifyIndex, after.ModifyIndex, "an unchanged role must not be written again")
}

// When the TerminatingGateway is deleted and the gateway policy link is
// missing, updateACls must not block the deletion. It must delete the unused
// service policies and must not write the role.
func TestTerminatingGatewayController_updateACls_DeletionWithGatewayPolicyLinkMissing(t *testing.T) {
	t.Parallel()
	env := newTGWACLTestEnv(t)
	env.createPolicies(t, tgwTestGWPolicy, tgwTestSvcAPolicy)
	before := env.createRole(t, tgwTestSvcAPolicy)

	termGW := newTestTerminatingGateway("svc-a")
	now := metav1.Now()
	termGW.DeletionTimestamp = &now

	require.NoError(t, env.controller.updateACls(logr.Discard(), termGW))

	after, _, err := env.consul.ACL().RoleRead(before.ID, nil)
	require.NoError(t, err)
	require.Equal(t, before.ModifyIndex, after.ModifyIndex, "the role must not be written while the gateway policy link is missing")
	policy, _, err := env.consul.ACL().PolicyReadByName(tgwTestSvcAPolicy, nil)
	require.NoError(t, err)
	require.Nil(t, policy, "the unused service policy must be deleted")
}
