// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package configentries

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-logr/logr"
	capi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	consulv1alpha1 "github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
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
