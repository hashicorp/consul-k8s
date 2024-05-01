// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package configentries_test

import (
	"context"
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

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/controllers/configentries"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

func TestReconcile_Success(tt *testing.T) {
	deletionTime := metav1.Now()
	cases := map[string]struct {
		registration       *v1alpha1.Registration
		expectedConditions []v1alpha1.Condition
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
			expectedConditions: []v1alpha1.Condition{},
		},
	}

	for name, tc := range cases {
		tt.Run(name, func(t *testing.T) {
			t.Parallel()
			s := runtime.NewScheme()
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.Registration{})
			ctx := context.Background()

			consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
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

			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tc.registration).Build()

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

			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tc.registration).Build()

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
