// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package configentries

import (
	"context"
	"encoding/base64"
	"net"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/hashicorp/consul-server-connection-manager/discovery"
	capi "github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	consulv1alpha1 "github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/helper/cert"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

const (
	aclResyncNamespace = "default"
	aclResyncGateway   = "terminating-gateway"
	aclResyncRole      = "consul-terminating-gateway-acl-role"
	aclResyncGWPolicy  = "terminating-gateway-policy"
	aclResyncSvcA      = "default-svc-a-write-policy"
	aclResyncSvcB      = "default-svc-b-write-policy"
)

type aclResyncTestEnv struct {
	controller *TerminatingGatewayController
	kubeClient client.Client
	consul     *capi.Client
}

// newACLResyncTestEnv starts a Consul test server with ACLs and a Kubernetes
// auth method, and returns a TerminatingGatewayController for a CR that links
// the given services.
func newACLResyncTestEnv(t *testing.T, manageSystemACLs bool, services ...string) *aclResyncTestEnv {
	t.Helper()
	const bootToken = "11111111-2222-3333-4444-555555555555"
	testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
		c.ACL.Enabled = true
		c.ACL.Tokens.InitialManagement = bootToken
	})

	_, _, caPEM, _, err := cert.GenerateCA("kubernetes")
	require.NoError(t, err)
	enc := base64.RawURLEncoding
	unsignedJWT := enc.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`)) + "." + enc.EncodeToString([]byte(`{"sub":"test"}`)) + "."
	_, _, err = testClient.APIClient.ACL().AuthMethodCreate(&capi.ACLAuthMethod{
		Name: "consul-k8s-component-auth-method",
		Type: "kubernetes",
		Config: map[string]interface{}{
			"Host":              "https://kubernetes.example.com",
			"CACert":            caPEM,
			"ServiceAccountJWT": unsignedJWT,
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

	helmValues := terminatingGatewayHelmValuesConfigMap(aclResyncNamespace)
	if manageSystemACLs {
		helmValues.Data["values.json"] = strings.Replace(helmValues.Data["values.json"],
			`"manageSystemACLs": false`, `"manageSystemACLs": true`, 1)
	}

	termGW := &consulv1alpha1.TerminatingGateway{
		ObjectMeta: metav1.ObjectMeta{Name: aclResyncGateway, Namespace: aclResyncNamespace},
	}
	for _, svc := range services {
		termGW.Spec.Services = append(termGW.Spec.Services, consulv1alpha1.LinkedService{Name: svc})
	}

	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, consulv1alpha1.AddToScheme(scheme))
	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(termGW, helmValues).
		WithStatusSubresource(termGW).
		WithIndex(&consulv1alpha1.TerminatingGateway{}, terminatingGatewayByLinkedServiceName, termGWLinkedServiceIndexer).
		Build()

	return &aclResyncTestEnv{
		controller: &TerminatingGatewayController{
			Client:           kubeClient,
			Log:              logr.Discard(),
			Scheme:           scheme,
			ReleaseName:      "consul",
			ReleaseNamespace: aclResyncNamespace,
			ConfigEntryController: &ConfigEntryController{
				ConsulClientConfig:  testClient.Cfg,
				ConsulServerConnMgr: connMgr,
				DatacenterName:      datacenterName,
			},
		},
		kubeClient: kubeClient,
		consul:     testClient.APIClient,
	}
}

func (e *aclResyncTestEnv) reconcile(t *testing.T, name string) ctrl.Result {
	t.Helper()
	result, err := e.controller.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: aclResyncNamespace, Name: name},
	})
	require.NoError(t, err)
	return result
}

func (e *aclResyncTestEnv) role(t *testing.T) *capi.ACLRole {
	t.Helper()
	role, _, err := e.consul.ACL().RoleReadByName(aclResyncRole, nil)
	require.NoError(t, err)
	require.NotNil(t, role)
	return role
}

func aclResyncPolicyNames(role *capi.ACLRole) []string {
	names := make([]string, 0, len(role.Policies))
	for _, p := range role.Policies {
		names = append(names, p.Name)
	}
	return names
}

// server-acl-init and the controller both read, change, and write the
// terminating gateway role, and Consul has no compare-and-set for roles. If
// server-acl-init writes a role that it read before the controller linked a new
// service, the new link is lost. The controller must schedule an ACL resync so
// that the next reconcile links the service again, without a change to the CR.
func TestTerminatingGatewayController_Reconcile_ACLResyncRestoresLinkRemovedByStaleRoleWrite(t *testing.T) {
	t.Parallel()
	env := newACLResyncTestEnv(t, true, "svc-a")

	result := env.reconcile(t, aclResyncGateway)
	require.Equal(t, terminatingGatewayACLResyncPeriod, result.RequeueAfter)
	require.ElementsMatch(t, []string{aclResyncGWPolicy, aclResyncSvcA}, aclResyncPolicyNames(env.role(t)))

	// server-acl-init reads the role.
	staleRole := env.role(t)

	// The controller links a new service.
	termGW := &consulv1alpha1.TerminatingGateway{}
	require.NoError(t, env.kubeClient.Get(context.Background(),
		types.NamespacedName{Namespace: aclResyncNamespace, Name: aclResyncGateway}, termGW))
	termGW.Spec.Services = append(termGW.Spec.Services, consulv1alpha1.LinkedService{Name: "svc-b"})
	require.NoError(t, env.kubeClient.Update(context.Background(), termGW))
	env.reconcile(t, aclResyncGateway)
	require.ElementsMatch(t, []string{aclResyncGWPolicy, aclResyncSvcA, aclResyncSvcB}, aclResyncPolicyNames(env.role(t)))

	// server-acl-init writes the role that it read earlier.
	_, _, err := env.consul.ACL().RoleUpdate(staleRole, nil)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{aclResyncGWPolicy, aclResyncSvcA}, aclResyncPolicyNames(env.role(t)),
		"precondition: the stale write removed the svc-b link")

	// The scheduled resync links svc-b again.
	result = env.reconcile(t, aclResyncGateway)
	require.Equal(t, terminatingGatewayACLResyncPeriod, result.RequeueAfter)
	require.ElementsMatch(t, []string{aclResyncGWPolicy, aclResyncSvcA, aclResyncSvcB}, aclResyncPolicyNames(env.role(t)))
}

// A Secret-triggered request must not schedule a resync. Otherwise the
// secret-trigger request would run again periodically and update the
// last-rotation metadata of the config entry each time.
func TestTerminatingGatewayController_Reconcile_SecretTriggerDoesNotScheduleACLResync(t *testing.T) {
	t.Parallel()
	env := newACLResyncTestEnv(t, true, "svc-a")

	result := env.reconcile(t, secretTriggerPrefix+aclResyncGateway)
	require.Zero(t, result.RequeueAfter)
	require.False(t, result.Requeue)
}

// When consul-k8s does not manage the ACLs, the controller does not update ACLs,
// so it must not schedule a resync.
func TestTerminatingGatewayController_Reconcile_UnmanagedACLsDoNotScheduleACLResync(t *testing.T) {
	t.Parallel()
	env := newACLResyncTestEnv(t, false, "svc-a")

	result := env.reconcile(t, aclResyncGateway)
	require.Zero(t, result.RequeueAfter)
	require.False(t, result.Requeue)
}

// A periodic resync must not write the role when its links did not change.
func TestTerminatingGatewayController_updateACls_SkipsUnchangedRoleWrite(t *testing.T) {
	t.Parallel()
	env := newACLResyncTestEnv(t, true, "svc-a")
	env.reconcile(t, aclResyncGateway)
	before := env.role(t)

	termGW := &consulv1alpha1.TerminatingGateway{}
	require.NoError(t, env.kubeClient.Get(context.Background(),
		types.NamespacedName{Namespace: aclResyncNamespace, Name: aclResyncGateway}, termGW))
	require.NoError(t, env.controller.updateACls(logr.Discard(), termGW))

	after := env.role(t)
	require.Equal(t, before.ModifyIndex, after.ModifyIndex, "an unchanged role must not be written again")
	require.ElementsMatch(t, aclResyncPolicyNames(before), aclResyncPolicyNames(after))
}
