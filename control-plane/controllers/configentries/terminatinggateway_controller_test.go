// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package configentries

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/hashicorp/consul-server-connection-manager/discovery"
	capi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	consulv1alpha1 "github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/controllers/helmvalues"
)

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
