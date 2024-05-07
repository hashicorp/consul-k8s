// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package configentries_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	logrtest "github.com/go-logr/logr/testing"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capi "github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-uuid"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/controllers/configentries"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

type serverResponseConfig struct {
	registering bool
	aclEnabled  bool
}

func TestReconcile_Success(tt *testing.T) {
	deletionTime := metav1.Now()
	cases := map[string]struct {
		registration         *v1alpha1.Registration
		serverResponseConfig serverResponseConfig
		expectedConditions   []v1alpha1.Condition
	}{
		"success on registration": {
			registration: &v1alpha1.Registration{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Registration",
					APIVersion: "consul.hashicorp.com/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-registration",
					Finalizers: []string{configentries.RegistrationFinalizer},
				},
				Spec: v1alpha1.RegistrationSpec{
					ID:         "node-id",
					Node:       "virtual-node",
					Address:    "127.0.0.1",
					Datacenter: "dc1",
					Service: v1alpha1.Service{
						ID:      "service-id",
						Name:    "service-name",
						Port:    8080,
						Address: "127.0.0.1",
					},
				},
			},
			serverResponseConfig: serverResponseConfig{registering: true},
			expectedConditions: []v1alpha1.Condition{{
				Type:    "Synced",
				Status:  v1.ConditionTrue,
				Reason:  "",
				Message: "",
			}},
		},
		"success on registration -- ACLs enabled": {
			registration: &v1alpha1.Registration{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Registration",
					APIVersion: "consul.hashicorp.com/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-registration",
					Finalizers: []string{configentries.RegistrationFinalizer},
				},
				Spec: v1alpha1.RegistrationSpec{
					ID:         "node-id",
					Node:       "virtual-node",
					Address:    "127.0.0.1",
					Datacenter: "dc1",
					Service: v1alpha1.Service{
						ID:      "service-id",
						Name:    "service-name",
						Port:    8080,
						Address: "127.0.0.1",
					},
				},
			},
			serverResponseConfig: serverResponseConfig{
				registering: true,
				aclEnabled:  true,
			},
			expectedConditions: []v1alpha1.Condition{{
				Type:    "Synced",
				Status:  v1.ConditionTrue,
				Reason:  "",
				Message: "",
			}},
		},
		"success on deregistration": {
			registration: &v1alpha1.Registration{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Registration",
					APIVersion: "consul.hashicorp.com/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-registration",
					Finalizers:        []string{configentries.RegistrationFinalizer},
					DeletionTimestamp: &deletionTime,
				},
				Spec: v1alpha1.RegistrationSpec{
					ID:         "node-id",
					Node:       "virtual-node",
					Address:    "127.0.0.1",
					Datacenter: "dc1",
					Service: v1alpha1.Service{
						ID:      "service-id",
						Name:    "service-name",
						Port:    8080,
						Address: "127.0.0.1",
					},
				},
			},
			serverResponseConfig: serverResponseConfig{
				registering: false,
				aclEnabled:  false,
			},
			expectedConditions: []v1alpha1.Condition{},
		},
		"success on deregistration - ACLs enabled": {
			registration: &v1alpha1.Registration{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Registration",
					APIVersion: "consul.hashicorp.com/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-registration",
					Finalizers:        []string{configentries.RegistrationFinalizer},
					DeletionTimestamp: &deletionTime,
				},
				Spec: v1alpha1.RegistrationSpec{
					ID:         "node-id",
					Node:       "virtual-node",
					Address:    "127.0.0.1",
					Datacenter: "dc1",
					Service: v1alpha1.Service{
						ID:      "service-id",
						Name:    "service-name",
						Port:    8080,
						Address: "127.0.0.1",
					},
				},
			},
			serverResponseConfig: serverResponseConfig{
				registering: false,
				aclEnabled:  true,
			},
			expectedConditions: []v1alpha1.Condition{},
		},
	}

	for name, tc := range cases {
		tc := tc
		tt.Run(name, func(t *testing.T) {
			t.Parallel()
			s := runtime.NewScheme()
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.Registration{})
			ctx := context.Background()

			consulServer, testClient := fakeConsulServer(t, tc.serverResponseConfig, tc.registration.Spec.Service.Name)
			defer consulServer.Close()

			fakeClient := fake.NewClientBuilder().
				WithScheme(s).
				WithRuntimeObjects(tc.registration).
				WithStatusSubresource(&v1alpha1.Registration{}).
				Build()

			controller := &configentries.RegistrationsController{
				Client:              fakeClient,
				Log:                 logrtest.NewTestLogger(t),
				Scheme:              s,
				ConsulClientConfig:  testClient.Cfg,
				ConsulServerConnMgr: testClient.Watcher,
			}

			_, err := controller.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{Name: tc.registration.Name, Namespace: tc.registration.Namespace},
			})
			require.NoError(t, err)

			fetchedReg := &v1alpha1.Registration{TypeMeta: metav1.TypeMeta{APIVersion: "consul.hashicorp.com/v1alpha1", Kind: "Registration"}}
			fakeClient.Get(ctx, types.NamespacedName{Name: tc.registration.Name}, fetchedReg)

			require.Len(t, fetchedReg.Status.Conditions, len(tc.expectedConditions))

			for i, c := range fetchedReg.Status.Conditions {
				if diff := cmp.Diff(c, tc.expectedConditions[i], cmpopts.IgnoreFields(v1alpha1.Condition{}, "LastTransitionTime", "Message")); diff != "" {
					t.Errorf("unexpected condition diff: %s", diff)
				}
			}
		})
	}
}

func TestReconcile_Failure(tt *testing.T) {
	deletionTime := metav1.Now()
	cases := map[string]struct {
		registration       *v1alpha1.Registration
		expectedConditions []v1alpha1.Condition
	}{
		"failure on registration": {
			registration: &v1alpha1.Registration{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Registration",
					APIVersion: "consul.hashicorp.com/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-registration",
					Finalizers: []string{configentries.RegistrationFinalizer},
				},
				Spec: v1alpha1.RegistrationSpec{
					ID:         "node-id",
					Node:       "virtual-node",
					Address:    "127.0.0.1",
					Datacenter: "dc1",
					Service: v1alpha1.Service{
						ID:      "service-id",
						Name:    "service-name",
						Port:    8080,
						Address: "127.0.0.1",
					},
				},
			},
			expectedConditions: []v1alpha1.Condition{{
				Type:    "Synced",
				Status:  v1.ConditionFalse,
				Reason:  "ConsulErrorRegistration",
				Message: "",
			}},
		},
		"failure on deregistration": {
			registration: &v1alpha1.Registration{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Registration",
					APIVersion: "consul.hashicorp.com/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-registration",
					Finalizers:        []string{configentries.RegistrationFinalizer},
					DeletionTimestamp: &deletionTime,
				},
				Spec: v1alpha1.RegistrationSpec{
					ID:         "node-id",
					Node:       "virtual-node",
					Address:    "127.0.0.1",
					Datacenter: "dc1",
					Service: v1alpha1.Service{
						ID:      "service-id",
						Name:    "service-name",
						Port:    8080,
						Address: "127.0.0.1",
					},
				},
			},
			expectedConditions: []v1alpha1.Condition{{
				Type:    "Synced",
				Status:  v1.ConditionFalse,
				Reason:  "ConsulErrorDeregistration",
				Message: "",
			}},
		},
	}

	for name, tc := range cases {
		tc := tc
		tt.Run(name, func(t *testing.T) {
			t.Parallel()
			s := runtime.NewScheme()
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.Registration{})
			ctx := context.Background()

			consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(500)
			}))
			defer consulServer.Close()

			parsedURL, err := url.Parse(consulServer.URL)
			require.NoError(t, err)
			host := strings.Split(parsedURL.Host, ":")[0]

			port, err := strconv.Atoi(parsedURL.Port())
			require.NoError(t, err)

			testClient := &test.TestServerClient{
				Cfg:     &consul.Config{APIClientConfig: &capi.Config{Address: host}, HTTPPort: port},
				Watcher: test.MockConnMgrForIPAndPort(t, host, port, false),
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(s).
				WithRuntimeObjects(tc.registration).
				WithStatusSubresource(&v1alpha1.Registration{}).
				Build()

			controller := &configentries.RegistrationsController{
				Client:              fakeClient,
				Log:                 logrtest.NewTestLogger(t),
				Scheme:              s,
				ConsulClientConfig:  testClient.Cfg,
				ConsulServerConnMgr: testClient.Watcher,
			}

			_, err = controller.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{Name: tc.registration.Name, Namespace: tc.registration.Namespace},
			})
			require.Error(t, err)

			fetchedReg := &v1alpha1.Registration{TypeMeta: metav1.TypeMeta{APIVersion: "consul.hashicorp.com/v1alpha1", Kind: "Registration"}}
			fakeClient.Get(ctx, types.NamespacedName{Name: tc.registration.Name}, fetchedReg)

			require.Len(t, fetchedReg.Status.Conditions, len(tc.expectedConditions))

			for i, c := range fetchedReg.Status.Conditions {
				if diff := cmp.Diff(c, tc.expectedConditions[i], cmpopts.IgnoreFields(v1alpha1.Condition{}, "LastTransitionTime", "Message")); diff != "" {
					t.Errorf("unexpected condition diff: %s", diff)
				}
			}
		})
	}
}

func fakeConsulServer(t *testing.T, serverResponseConfig serverResponseConfig, serviceName string) (*httptest.Server, *test.TestServerClient) {
	t.Helper()
	mux := buildMux(t, serverResponseConfig, serviceName)
	consulServer := httptest.NewServer(mux)

	parsedURL, err := url.Parse(consulServer.URL)
	require.NoError(t, err)
	host := strings.Split(parsedURL.Host, ":")[0]

	port, err := strconv.Atoi(parsedURL.Port())
	require.NoError(t, err)

	cfg := &consul.Config{APIClientConfig: &capi.Config{Address: host}, HTTPPort: port}
	if serverResponseConfig.aclEnabled {
		cfg.APIClientConfig.Token = "test-token"
	}

	testClient := &test.TestServerClient{
		Cfg:     cfg,
		Watcher: test.MockConnMgrForIPAndPort(t, host, port, false),
	}

	return consulServer, testClient
}

func buildMux(t *testing.T, cfg serverResponseConfig, serviceName string) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/catalog/register", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})

	mux.HandleFunc("/v1/catalog/deregister", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})

	policyID, err := uuid.GenerateUUID()
	require.NoError(t, err)

	mux.HandleFunc("/v1/acl/roles", func(w http.ResponseWriter, r *http.Request) {
		termGWPolicies := []*capi.ACLLink{
			{
				ID:   "b7e377d9-5e2b-b99c-3f06-139584cf47f8",
				Name: "terminating-gateway-policy",
			},
		}

		if !cfg.registering {
			termGWPolicies = append(termGWPolicies, &capi.ACLLink{
				ID:   policyID,
				Name: fmt.Sprintf("%s-write-policy", serviceName),
			})
		}

		entries := []*capi.ACLRole{
			{
				ID:          "754a8717-46e9-9f18-7f76-28dc0afafd19",
				Name:        "consul-consul-connect-inject-acl-role",
				Description: "ACL Role for consul-consul-connect-injector",
				Policies: []*capi.ACLLink{
					{
						ID:   "38511a9f-a309-11e2-7f67-7fea12056e7c",
						Name: "connect-inject-policy",
					},
				},
			},
			{
				ID:          "61fc5051-96e9-7b67-69b5-98f7f6682563",
				Name:        "consul-consul-terminating-gateway-acl-role",
				Description: "ACL Role for consul-consul-terminating-gateway",
				Policies:    termGWPolicies,
			},
		}
		val, err := json.Marshal(entries)
		if err != nil {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
		w.Write(val)
	})

	mux.HandleFunc("/v1/acl/role/", func(w http.ResponseWriter, r *http.Request) {
		role := &capi.ACLRole{
			ID:          "61fc5051-96e9-7b67-69b5-98f7f6682563",
			Name:        "consul-consul-terminating-gateway-acl-role",
			Description: "ACL Role for consul-consul-terminating-gateway",
			Policies: []*capi.ACLLink{
				{
					ID:   "b7e377d9-5e2b-b99c-3f06-139584cf47f8",
					Name: "terminating-gateway-policy",
				},
				{
					ID:   policyID,
					Name: fmt.Sprintf("%s-write-policy", serviceName),
				},
			},
		}
		val, err := json.Marshal(role)
		if err != nil {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
		w.Write(val)
	})

	mux.HandleFunc("/v1/acl/policy/name/", func(w http.ResponseWriter, r *http.Request) {
		policy := &capi.ACLPolicy{
			ID:   policyID,
			Name: fmt.Sprintf("%s-write-policy", serviceName),
		}
		val, err := json.Marshal(policy)
		if err != nil {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
		w.Write(val)
	})

	mux.HandleFunc("/v1/acl/policy/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})

	return mux
}
