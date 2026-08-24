// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	logrtest "github.com/go-logr/logr/testr"
	capi "github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul-server-connection-manager/discovery"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
)

// ── mock Consul httptest helpers ──────────────────────────────────────────────
//
// These helpers replace test.TestServerWithMockConnMgrWatcher so tests never
// require a consul binary on $PATH. The mock server accepts any ConfigEntries
// call and returns 200/OK so the controller's upsertConfigEntry / deleteConfigEntry
// calls succeed without errors. Exact response bodies are not validated here;
// unit tests for config-entry mapping are in TestToConsulConfigEntry below.

// consulMockServer starts an httptest server that responds 200 to all Consul
// API calls. It returns a consul.Config pointing at the server and a
// MockServerConnectionManager. This pattern matches the cache/consul_test.go
// approach: no consul binary needed.
func consulMockServer(t *testing.T) (*consul.Config, consul.ServerConnectionManager) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle any Consul API call generically.
		switch r.Method {
		case http.MethodPut:
			// ConfigEntries().Set() — return a minimal success payload.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"Index": 1})
		case http.MethodDelete:
			// ConfigEntries().Delete()
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			// ConfigEntries().Get() — return 404 so deleteConfigEntry treats it
			// as "already removed" and returns nil (not an error).
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("404 - Unexpected format for token"))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	host, portStr, err := net.SplitHostPort(srv.Listener.Addr().String())
	require.NoError(t, err)
	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)

	ip := net.ParseIP(host)
	if ip == nil {
		addrs, err2 := net.LookupHost(host)
		require.NoError(t, err2)
		ip = net.ParseIP(addrs[0])
	}

	cfg := &consul.Config{
		APIClientConfig: &capi.Config{
			Address: fmt.Sprintf("%s:%s", host, portStr),
			Scheme:  "http",
		},
		HTTPPort: port,
	}

	watcher := consul.NewMockServerConnectionManager(t)
	// Maybe() marks this expectation as optional so that tests which return
	// early (e.g. resource-not-found) without calling ConsulServerConnMgr.State()
	// do not fail the mock's AssertExpectations check.
	watcher.On("State").Maybe().Return(discovery.State{
		Address: discovery.Addr{
			TCPAddr: net.TCPAddr{IP: ip, Port: port},
		},
	}, nil)

	return cfg, watcher
}

// ---------------------------------------------------------------------------
// TestInferenceGatewayReconcile — happy/sad path table tests (K8s only)
// ---------------------------------------------------------------------------

func TestInferenceGatewayReconcile(t *testing.T) {
	t.Parallel()

	deletionTimestamp := metav1.Now()

	cases := []struct {
		name       string
		k8sObjects func() []runtime.Object
		expErr     string
		requeue    bool
	}{
		{
			name: "resource not found returns no error",
			k8sObjects: func() []runtime.Object {
				return []runtime.Object{}
			},
		},
		{
			name: "new resource gets finalizer and requeues",
			k8sObjects: func() []runtime.Object {
				return []runtime.Object{minimalIGW("my-gw", "default", "my-pool")}
			},
			requeue: true,
		},
		{
			name: "resource marked for deletion — finalizer is removed",
			k8sObjects: func() []runtime.Object {
				igw := minimalIGW("my-gw", "default", "my-pool")
				igw.ObjectMeta.DeletionTimestamp = &deletionTimestamp
				igw.ObjectMeta.Finalizers = []string{inferenceGatewayFinalizer}
				return []runtime.Object{igw}
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			consulCfg, watcher := consulMockServer(t)

			s := igwScheme(t)
			fakeClient := fake.NewClientBuilder().
				WithScheme(s).
				WithRuntimeObjects(tt.k8sObjects()...).
				WithStatusSubresource(&v1alpha1.InferenceGateway{}).
				Build()

			controller := igwController(t, fakeClient, consulCfg, watcher)

			resp, err := controller.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "my-gw",
					Namespace: "default",
				},
			})

			if tt.expErr != "" {
				require.EqualError(t, err, tt.expErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.requeue, resp.Requeue)
		})
	}
}

// ---------------------------------------------------------------------------
// TestInferenceGatewayReconcile_Finalizer — finalizer lifecycle
// ---------------------------------------------------------------------------

func TestInferenceGatewayReconcile_Finalizer(t *testing.T) {
	t.Parallel()

	t.Run("finalizer is added on first reconcile", func(t *testing.T) {
		consulCfg, watcher := consulMockServer(t)
		s := igwScheme(t)

		igw := minimalIGW("gw", "default", "pool")
		require.Empty(t, igw.Finalizers, "should start without finalizers")

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).WithRuntimeObjects(igw).
			WithStatusSubresource(&v1alpha1.InferenceGateway{}).Build()

		recorder := record.NewFakeRecorder(10)
		controller := igwController(t, fakeClient, consulCfg, watcher)
		controller.Recorder = recorder

		resp, err := controller.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "gw", Namespace: "default"},
		})
		require.NoError(t, err)
		require.True(t, resp.Requeue, "should requeue after adding finalizer")

		got := &v1alpha1.InferenceGateway{}
		require.NoError(t, fakeClient.Get(context.Background(),
			types.NamespacedName{Name: "gw", Namespace: "default"}, got))
		require.Contains(t, got.Finalizers, inferenceGatewayFinalizer)

		require.Len(t, recorder.Events, 1)
		require.Contains(t, <-recorder.Events, eventReasonFinalizerAdded)
	})

	t.Run("finalizer is idempotent on subsequent reconciles", func(t *testing.T) {
		consulCfg, watcher := consulMockServer(t)
		s := igwScheme(t)

		pool := enabledPool("pool", "default")
		igw := minimalIGW("gw", "default", "pool")
		igw.Finalizers = []string{inferenceGatewayFinalizer}

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).WithRuntimeObjects(igw, pool).
			WithStatusSubresource(&v1alpha1.InferenceGateway{}).Build()

		recorder := record.NewFakeRecorder(10)
		controller := igwController(t, fakeClient, consulCfg, watcher)
		controller.Recorder = recorder

		_, err := controller.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "gw", Namespace: "default"},
		})
		require.NoError(t, err)

		got := &v1alpha1.InferenceGateway{}
		require.NoError(t, fakeClient.Get(context.Background(),
			types.NamespacedName{Name: "gw", Namespace: "default"}, got))

		count := 0
		for _, f := range got.Finalizers {
			if f == inferenceGatewayFinalizer {
				count++
			}
		}
		require.Equal(t, 1, count, "finalizer must appear exactly once")

		require.Len(t, recorder.Events, 1)
		require.Contains(t, <-recorder.Events, eventReasonSynced)
	})

	t.Run("finalizer is removed on deletion", func(t *testing.T) {
		consulCfg, watcher := consulMockServer(t)
		s := igwScheme(t)

		ts := metav1.Now()
		igw := minimalIGW("gw", "default", "pool")
		igw.Finalizers = []string{inferenceGatewayFinalizer}
		igw.DeletionTimestamp = &ts

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).WithRuntimeObjects(igw).
			WithStatusSubresource(&v1alpha1.InferenceGateway{}).Build()

		recorder := record.NewFakeRecorder(10)
		controller := igwController(t, fakeClient, consulCfg, watcher)
		controller.Recorder = recorder

		_, err := controller.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "gw", Namespace: "default"},
		})
		require.NoError(t, err)

		got := &v1alpha1.InferenceGateway{}
		getErr := fakeClient.Get(context.Background(),
			types.NamespacedName{Name: "gw", Namespace: "default"}, got)
		if getErr == nil {
			require.NotContains(t, got.Finalizers, inferenceGatewayFinalizer)
		} else {
			require.True(t, k8serrors.IsNotFound(getErr),
				"expected not-found after finalizer removal, got: %v", getErr)
		}

		require.Len(t, recorder.Events, 1)
		require.Contains(t, <-recorder.Events, eventReasonFinalizerRemoved)
	})
}

// ---------------------------------------------------------------------------
// TestInferenceGatewayReconcile_PoolRef — pool resolution behaviour
// ---------------------------------------------------------------------------

func TestInferenceGatewayReconcile_PoolRef(t *testing.T) {
	t.Parallel()

	t.Run("missing pool sets PoolResolved=False and requeues", func(t *testing.T) {
		consulCfg, watcher := consulMockServer(t)
		s := igwScheme(t)

		igw := minimalIGW("gw", "default", "does-not-exist")
		igw.Finalizers = []string{inferenceGatewayFinalizer}

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).WithRuntimeObjects(igw).
			WithStatusSubresource(&v1alpha1.InferenceGateway{}).Build()

		controller := igwController(t, fakeClient, consulCfg, watcher)

		resp, err := controller.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "gw", Namespace: "default"},
		})
		require.NoError(t, err)
		require.Equal(t, 10*time.Second, resp.RequeueAfter, "should requeue after pool not found")

		got := &v1alpha1.InferenceGateway{}
		require.NoError(t, fakeClient.Get(context.Background(),
			types.NamespacedName{Name: "gw", Namespace: "default"}, got))

		cond := findCondition(got.Status.Conditions, conditionTypePoolResolved)
		require.NotNil(t, cond, "PoolResolved condition must be set")
		require.Equal(t, metav1.ConditionFalse, cond.Status)
		require.Equal(t, reasonPoolNotReady, cond.Reason)
	})

	t.Run("disabled pool sets Ready=False", func(t *testing.T) {
		consulCfg, watcher := consulMockServer(t)
		s := igwScheme(t)

		pool := enabledPool("pool", "default")
		pool.Spec.Enabled = false
		igw := minimalIGW("gw", "default", "pool")
		igw.Finalizers = []string{inferenceGatewayFinalizer}

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).WithRuntimeObjects(igw, pool).
			WithStatusSubresource(&v1alpha1.InferenceGateway{}).Build()

		controller := igwController(t, fakeClient, consulCfg, watcher)

		_, err := controller.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "gw", Namespace: "default"},
		})
		require.NoError(t, err)

		got := &v1alpha1.InferenceGateway{}
		require.NoError(t, fakeClient.Get(context.Background(),
			types.NamespacedName{Name: "gw", Namespace: "default"}, got))

		ready := findCondition(got.Status.Conditions, conditionTypeReady)
		require.NotNil(t, ready)
		require.Equal(t, metav1.ConditionFalse, ready.Status, "Ready must be False when pool is disabled")
	})
}

// ---------------------------------------------------------------------------
// TestInferenceGatewayReconcile_ChildResources — Deployment and Service
// ---------------------------------------------------------------------------

func TestInferenceGatewayReconcile_ChildResources(t *testing.T) {
	t.Parallel()

	t.Run("Deployment and Service are created on first sync", func(t *testing.T) {
		consulCfg, watcher := consulMockServer(t)
		s := igwScheme(t)

		pool := enabledPool("pool", "default")
		igw := minimalIGW("gw", "default", "pool")
		igw.Finalizers = []string{inferenceGatewayFinalizer}

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).WithRuntimeObjects(igw, pool).
			WithStatusSubresource(&v1alpha1.InferenceGateway{}).Build()

		controller := igwController(t, fakeClient, consulCfg, watcher)

		_, err := controller.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "gw", Namespace: "default"},
		})
		require.NoError(t, err)

		// Deployment must exist.
		dep := &appsv1.Deployment{}
		require.NoError(t, fakeClient.Get(context.Background(),
			types.NamespacedName{Name: "gw", Namespace: "default"}, dep))
		require.Equal(t, "gw", dep.Name)
		require.Equal(t, "default", dep.Namespace)
		require.Equal(t, "test-gateway-image:latest", dep.Spec.Template.Spec.Containers[0].Image)
		require.Equal(t, inferenceGatewayPort, dep.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort)

		// Deployment must carry pool env vars.
		envMap := make(map[string]string)
		for _, e := range dep.Spec.Template.Spec.Containers[0].Env {
			envMap[e.Name] = e.Value
		}
		require.Equal(t, "pool", envMap["POOL_NAME"])
		require.Equal(t, "default", envMap["POOL_NAMESPACE"])
		require.Equal(t, "true", envMap["POOL_ENABLED"])

		// Service must exist.
		svc := &corev1.Service{}
		require.NoError(t, fakeClient.Get(context.Background(),
			types.NamespacedName{Name: "gw", Namespace: "default"}, svc))
		require.Equal(t, corev1.ServiceTypeClusterIP, svc.Spec.Type)
		require.Equal(t, inferenceGatewayPort, svc.Spec.Ports[0].Port)
	})

	t.Run("Deployment has owner reference pointing to InferenceGateway", func(t *testing.T) {
		consulCfg, watcher := consulMockServer(t)
		s := igwScheme(t)

		pool := enabledPool("pool", "default")
		igw := minimalIGW("gw", "default", "pool")
		igw.Finalizers = []string{inferenceGatewayFinalizer}

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).WithRuntimeObjects(igw, pool).
			WithStatusSubresource(&v1alpha1.InferenceGateway{}).Build()

		controller := igwController(t, fakeClient, consulCfg, watcher)

		_, err := controller.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "gw", Namespace: "default"},
		})
		require.NoError(t, err)

		dep := &appsv1.Deployment{}
		require.NoError(t, fakeClient.Get(context.Background(),
			types.NamespacedName{Name: "gw", Namespace: "default"}, dep))

		require.Len(t, dep.OwnerReferences, 1, "Deployment must have exactly one owner reference")
		require.Equal(t, "InferenceGateway", dep.OwnerReferences[0].Kind)
		require.Equal(t, "gw", dep.OwnerReferences[0].Name)
	})
}

// ---------------------------------------------------------------------------
// TestInferenceGatewayReconcile_StatusConditions — exact condition values
// ---------------------------------------------------------------------------

func TestInferenceGatewayReconcile_StatusConditions(t *testing.T) {
	t.Parallel()

	type condWant struct {
		condType string
		status   metav1.ConditionStatus
		reason   string
	}

	cases := []struct {
		name      string
		buildIGW  func() *v1alpha1.InferenceGateway
		buildPool func() *v1alpha1.InferencePoolConfig
		wantConds []condWant
	}{
		{
			name:      "enabled pool → PoolResolved=True, Available=True, Ready=True",
			buildIGW:  func() *v1alpha1.InferenceGateway { return minimalIGW("gw", "default", "pool") },
			buildPool: func() *v1alpha1.InferencePoolConfig { return enabledPool("pool", "default") },
			wantConds: []condWant{
				{conditionTypePoolResolved, metav1.ConditionTrue, reasonPoolResolved},
				{"Available", metav1.ConditionTrue, reasonReconciled},
				{conditionTypeReady, metav1.ConditionTrue, reasonReconciled},
			},
		},
		{
			name: "disabled pool → PoolResolved=False, Available=False, Ready=False",
			buildIGW: func() *v1alpha1.InferenceGateway { return minimalIGW("gw", "default", "pool") },
			buildPool: func() *v1alpha1.InferencePoolConfig {
				p := enabledPool("pool", "default")
				p.Spec.Enabled = false
				return p
			},
			wantConds: []condWant{
				{conditionTypePoolResolved, metav1.ConditionFalse, reasonPoolNotReady},
				{"Available", metav1.ConditionFalse, reasonReconciled},
				{conditionTypeReady, metav1.ConditionFalse, reasonReconciled},
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			consulCfg, watcher := consulMockServer(t)
			s := igwScheme(t)

			igw := tt.buildIGW()
			igw.Finalizers = []string{inferenceGatewayFinalizer}
			pool := tt.buildPool()

			fakeClient := fake.NewClientBuilder().
				WithScheme(s).WithRuntimeObjects(igw, pool).
				WithStatusSubresource(&v1alpha1.InferenceGateway{}).Build()

			controller := igwController(t, fakeClient, consulCfg, watcher)

			_, err := controller.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Name: igw.Name, Namespace: igw.Namespace},
			})
			require.NoError(t, err)

			got := &v1alpha1.InferenceGateway{}
			require.NoError(t, fakeClient.Get(context.Background(),
				types.NamespacedName{Name: igw.Name, Namespace: igw.Namespace}, got))

			require.NotNil(t, got.Status.LastSyncedTime, "LastSyncedTime must be set after reconcile")

			for _, want := range tt.wantConds {
				cond := findCondition(got.Status.Conditions, want.condType)
				require.NotNilf(t, cond, "condition %q not found", want.condType)
				require.Equalf(t, want.status, cond.Status,
					"condition %q: got %q want %q", want.condType, cond.Status, want.status)
				require.Equalf(t, want.reason, cond.Reason,
					"condition %q: got reason %q want %q", want.condType, cond.Reason, want.reason)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestInferenceGatewayReconcile_ReadyReplicas — readyReplicas propagation
// ---------------------------------------------------------------------------

func TestInferenceGatewayReconcile_ReadyReplicas(t *testing.T) {
	t.Parallel()

	t.Run("readyReplicas is zero before Deployment pods start", func(t *testing.T) {
		consulCfg, watcher := consulMockServer(t)
		s := igwScheme(t)

		pool := enabledPool("pool", "default")
		igw := minimalIGW("gw", "default", "pool")
		igw.Finalizers = []string{inferenceGatewayFinalizer}

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).WithRuntimeObjects(igw, pool).
			WithStatusSubresource(&v1alpha1.InferenceGateway{}).Build()

		controller := igwController(t, fakeClient, consulCfg, watcher)

		_, err := controller.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "gw", Namespace: "default"},
		})
		require.NoError(t, err)

		got := &v1alpha1.InferenceGateway{}
		require.NoError(t, fakeClient.Get(context.Background(),
			types.NamespacedName{Name: "gw", Namespace: "default"}, got))
		// No pods have started yet — fake client returns ReadyReplicas=0.
		require.Equal(t, int32(0), got.Status.ReadyReplicas,
			"readyReplicas should be 0 before any Deployment pods start")
	})

	t.Run("readyReplicas mirrors Deployment.Status.ReadyReplicas", func(t *testing.T) {
		consulCfg, watcher := consulMockServer(t)
		s := igwScheme(t)

		pool := enabledPool("pool", "default")
		igw := minimalIGW("gw", "default", "pool")
		igw.Finalizers = []string{inferenceGatewayFinalizer}

		// Pre-create a Deployment with ReadyReplicas=1 in the fake store.
		// The controller reads Deployment.Status.ReadyReplicas after reconcileDeployment,
		// so seeding it here simulates a running pod.
		existingDep := deploymentFor(igw, pool, "test-gateway-image:latest")
		existingDep.Status.ReadyReplicas = 1

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).
			WithRuntimeObjects(igw, pool, existingDep).
			WithStatusSubresource(&v1alpha1.InferenceGateway{}, &appsv1.Deployment{}).
			Build()

		// Update the Deployment status directly so the fake reflects ReadyReplicas=1.
		depCopy := existingDep.DeepCopy()
		depCopy.Status.ReadyReplicas = 1
		require.NoError(t, fakeClient.Status().Update(context.Background(), depCopy))

		controller := igwController(t, fakeClient, consulCfg, watcher)

		_, err := controller.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "gw", Namespace: "default"},
		})
		require.NoError(t, err)

		got := &v1alpha1.InferenceGateway{}
		require.NoError(t, fakeClient.Get(context.Background(),
			types.NamespacedName{Name: "gw", Namespace: "default"}, got))
		require.Equal(t, int32(1), got.Status.ReadyReplicas,
			"readyReplicas must mirror Deployment.Status.ReadyReplicas")
	})
}

// ---------------------------------------------------------------------------
// TestInferenceGatewayReconcile_EnableConsulNamespaces — namespace guard
// ---------------------------------------------------------------------------

func TestInferenceGatewayReconcile_EnableConsulNamespaces(t *testing.T) {
	t.Parallel()

	t.Run("EnableConsulNamespaces=false never sends ?ns= to Consul", func(t *testing.T) {
		// This test verifies the OSS namespace guard: when EnableConsulNamespaces=false
		// the controller must not append ?ns= to any Consul API call.
		var capturedURL string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedURL = r.URL.String()
			w.Header().Set("Content-Type", "application/json")
			switch r.Method {
			case http.MethodPut:
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"Index": 1})
			case http.MethodGet:
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte("404 Not Found"))
			default:
				w.WriteHeader(http.StatusOK)
			}
		}))
		t.Cleanup(srv.Close)

		host, portStr, err := net.SplitHostPort(srv.Listener.Addr().String())
		require.NoError(t, err)
		port, err := strconv.Atoi(portStr)
		require.NoError(t, err)

		ip := net.ParseIP(host)
		if ip == nil {
			addrs, err2 := net.LookupHost(host)
			require.NoError(t, err2)
			ip = net.ParseIP(addrs[0])
		}

		cfg := &consul.Config{
			APIClientConfig: &capi.Config{
				Address: fmt.Sprintf("%s:%s", host, portStr),
				Scheme:  "http",
			},
			HTTPPort: port,
		}
		watcher := consul.NewMockServerConnectionManager(t)
		watcher.On("State").Maybe().Return(discovery.State{
			Address: discovery.Addr{TCPAddr: net.TCPAddr{IP: ip, Port: port}},
		}, nil)

		s := igwScheme(t)
		pool := enabledPool("pool", "default")
		igw := minimalIGW("gw", "default", "pool")
		igw.Finalizers = []string{inferenceGatewayFinalizer}

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).WithRuntimeObjects(igw, pool).
			WithStatusSubresource(&v1alpha1.InferenceGateway{}).Build()

		controller := igwController(t, fakeClient, cfg, watcher)
		controller.EnableConsulNamespaces = false // OSS mode — must NOT send ?ns=
		controller.ConsulNamespace = "default"    // even though ConsulNamespace is set

		_, err = controller.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "gw", Namespace: "default"},
		})
		require.NoError(t, err)

		require.NotContains(t, capturedURL, "ns=",
			"OSS: no ?ns= param must be sent to Consul when EnableConsulNamespaces=false; URL was: %s", capturedURL)
	})
}

// ---------------------------------------------------------------------------
// TestInferenceGatewayReconcile_Events — Synced event emission
// ---------------------------------------------------------------------------

func TestInferenceGatewayReconcile_Events(t *testing.T) {
	t.Parallel()

	t.Run("successful reconcile emits Synced event", func(t *testing.T) {
		consulCfg, watcher := consulMockServer(t)
		s := igwScheme(t)

		pool := enabledPool("pool", "default")
		igw := minimalIGW("gw", "default", "pool")
		igw.Finalizers = []string{inferenceGatewayFinalizer}

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).WithRuntimeObjects(igw, pool).
			WithStatusSubresource(&v1alpha1.InferenceGateway{}).Build()

		recorder := record.NewFakeRecorder(10)
		controller := igwController(t, fakeClient, consulCfg, watcher)
		controller.Recorder = recorder

		_, err := controller.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "gw", Namespace: "default"},
		})
		require.NoError(t, err)

		require.Len(t, recorder.Events, 1)
		event := <-recorder.Events
		require.Contains(t, event, string(corev1.EventTypeNormal))
		require.Contains(t, event, eventReasonSynced)
	})
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// igwScheme builds a runtime.Scheme containing all types needed by the
// InferenceGatewayController: core K8s types, apps/v1 (Deployment), and the
// consul-k8s AI CRDs.
func igwScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	require.NoError(t, appsv1.AddToScheme(s))
	require.NoError(t, v1alpha1.AddToScheme(s))
	return s
}

// igwController constructs an InferenceGatewayController wired to the given
// K8s client and Consul test-server config.
func igwController(
	t *testing.T,
	k8sClient client.Client,
	consulCfg *consul.Config,
	watcher consul.ServerConnectionManager,
) *InferenceGatewayController {
	t.Helper()
	return &InferenceGatewayController{
		Client:              k8sClient,
		Log:                 logrtest.New(t),
		Recorder:            record.NewFakeRecorder(10),
		GatewayImage:        "test-gateway-image:latest",
		ConsulClientConfig:  consulCfg,
		ConsulServerConnMgr: watcher,
		Datacenter:          "dc1",
		// EnableConsulNamespaces defaults to false (OSS mode) — safe default.
	}
}

// minimalIGW returns the minimum valid InferenceGateway pointing at poolName.
func minimalIGW(name, namespace, poolName string) *v1alpha1.InferenceGateway {
	return &v1alpha1.InferenceGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha1.InferenceGatewaySpec{
			PoolRef: v1alpha1.InferencePoolRef{Name: poolName},
		},
	}
}

// enabledPool returns a minimal enabled InferencePoolConfig.
func enabledPool(name, namespace string) *v1alpha1.InferencePoolConfig {
	return &v1alpha1.InferencePoolConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha1.InferencePoolConfigSpec{
			Enabled: true,
			ParentRefs: []v1alpha1.InferencePoolParentRef{
				{Kind: v1alpha1.InferenceGatewayKind, Name: name},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// TestToConsulConfigEntry — config-entry mapping
// ---------------------------------------------------------------------------

func TestToConsulConfigEntry(t *testing.T) {
	t.Parallel()

	t.Run("pool with stateStore maps to capi.AIGatewayStateStore", func(t *testing.T) {
		igw := minimalIGW("gw", "default", "pool")
		pool := enabledPool("pool", "default")
		pool.Spec.StateStore = &v1alpha1.InferencePoolStateStore{
			Service:       "valkey",
			LocalBindPort: 6379,
		}

		r := &InferenceGatewayController{Datacenter: "dc1"}
		entry := r.toConsulConfigEntry(igw, pool)

		aige := entry.(*capi.AIGatewayConfigEntry)
		require.NotNil(t, aige.StateStore,
			"StateStore must be mapped when pool.Spec.StateStore is set")
		require.Equal(t, "valkey", aige.StateStore.Service)
		require.Equal(t, 6379, aige.StateStore.LocalBindPort)
	})

	t.Run("pool without stateStore sends nil StateStore to Consul", func(t *testing.T) {
		igw := minimalIGW("gw", "default", "pool")
		pool := enabledPool("pool", "default") // no stateStore

		r := &InferenceGatewayController{Datacenter: "dc1"}
		entry := r.toConsulConfigEntry(igw, pool)

		aige := entry.(*capi.AIGatewayConfigEntry)
		require.Nil(t, aige.StateStore,
			"StateStore must be nil when pool.Spec.StateStore is not set")
	})

	t.Run("minimal pool produces AIGatewayConfigEntry with correct kind and name", func(t *testing.T) {
		igw := minimalIGW("my-gw", "default", "my-pool")
		pool := enabledPool("my-pool", "default")

		r := &InferenceGatewayController{
			Datacenter:      "dc1",
			ConsulPartition: "",
			ConsulNamespace: "",
		}
		entry := r.toConsulConfigEntry(igw, pool)

		require.Equal(t, capi.AIGateway, entry.GetKind())
		require.Equal(t, "my-gw", entry.GetName())
		require.Equal(t, "dc1", entry.GetMeta()["consul.hashicorp.com/source-datacenter"])

		aige, ok := entry.(*capi.AIGatewayConfigEntry)
		require.True(t, ok, "entry must be *capi.AIGatewayConfigEntry")
		require.Equal(t, []string{"my-gw"}, aige.ApplyTo)
		// No routing or rate-limit on a minimal pool.
		require.Nil(t, aige.RateLimit)
	})

	t.Run("pool with routing maps MatchRules and Fallback", func(t *testing.T) {
		igw := minimalIGW("gw", "default", "pool")
		pool := enabledPool("pool", "default")
		pool.Spec.Routing = &v1alpha1.InferencePoolRouting{
			MatchRules: []v1alpha1.InferencePoolMatchRule{
				{
					When:       v1alpha1.InferencePoolMatch{Path: "/v1/chat/completions"},
					Candidates: []string{"gpt-4o", "gpt-4"},
				},
			},
			Fallback: &v1alpha1.InferencePoolFallback{
				RetryOn:       []string{"5xx", "reset"},
				MaxTiers:      3,
				PerTryTimeout: "30s",
			},
			Retry: &v1alpha1.InferencePoolRetry{
				MaxAttempts: 2,
				RetryOn:     []string{"5xx"},
			},
			Timeout: &v1alpha1.InferencePoolTimeout{
				Connect: "5s",
				Request: "120s",
			},
		}

		r := &InferenceGatewayController{Datacenter: "dc1"}
		entry := r.toConsulConfigEntry(igw, pool)

		aige := entry.(*capi.AIGatewayConfigEntry)
		require.Len(t, aige.Routing.MatchRules, 1)
		require.Equal(t, "/v1/chat/completions", aige.Routing.MatchRules[0].When.Path)
		require.Equal(t, []string{"gpt-4o", "gpt-4"}, aige.Routing.MatchRules[0].Candidates)

		require.NotNil(t, aige.Routing.Fallback)
		require.Equal(t, []string{"5xx", "reset"}, aige.Routing.Fallback.RetryOn)
		require.Equal(t, 3, aige.Routing.Fallback.MaxTiers)
		require.Equal(t, "30s", aige.Routing.Fallback.PerTryTimeout)

		require.NotNil(t, aige.Routing.Retry)
		require.Equal(t, 2, aige.Routing.Retry.MaxAttempts)

		require.NotNil(t, aige.Routing.Timeout)
		require.Equal(t, "5s", aige.Routing.Timeout.Connect)
		require.Equal(t, "120s", aige.Routing.Timeout.Request)
	})

	t.Run("pool with rate-limit maps all RateLimit fields", func(t *testing.T) {
		igw := minimalIGW("gw", "default", "pool")
		pool := enabledPool("pool", "default")
		pool.Spec.RateLimit = &v1alpha1.InferencePoolRateLimit{
			Enabled:     true,
			Enforcement: "deny",
			Mode:        "soft",
			CountMode:   "total",
			Dimensions:  []string{"tier", "global"},
			DegradeMode: "fail_closed",
			Default: &v1alpha1.InferencePoolLimitPair{
				Requests: &v1alpha1.InferencePoolLimit{Count: 100, Window: "minute"},
				Tokens:   &v1alpha1.InferencePoolLimit{Count: 50000, Window: "minute"},
			},
			TierLimits: []v1alpha1.InferencePoolTierLimit{
				{
					Tier:                   "premium",
					Requests:               &v1alpha1.InferencePoolLimit{Count: 500, Window: "minute"},
					MaxCompletionTokensCap: 4096,
				},
			},
			TierBindings: []v1alpha1.InferencePoolTierBinding{
				{
					Tier:      "premium",
					SPIFFEIDs: []string{"spiffe://dc1/ns/default/dc/dc1/svc/my-app"},
				},
			},
		}

		r := &InferenceGatewayController{Datacenter: "dc1"}
		entry := r.toConsulConfigEntry(igw, pool)

		aige := entry.(*capi.AIGatewayConfigEntry)
		require.NotNil(t, aige.RateLimit)
		require.True(t, aige.RateLimit.Enabled)
		require.Equal(t, "deny", aige.RateLimit.Enforcement)
		require.Equal(t, "soft", aige.RateLimit.Mode)
		require.Equal(t, []string{"tier", "global"}, aige.RateLimit.Dimensions)
		require.Equal(t, "fail_closed", aige.RateLimit.DegradeMode)

		require.NotNil(t, aige.RateLimit.Default)
		require.Equal(t, 100, aige.RateLimit.Default.Requests.Count)
		require.Equal(t, "minute", aige.RateLimit.Default.Requests.Unit)

		require.Len(t, aige.RateLimit.TierLimits, 1)
		require.Equal(t, "premium", aige.RateLimit.TierLimits[0].Tier)
		require.Equal(t, 4096, aige.RateLimit.TierLimits[0].MaxCompletionTokensCap)

		require.Len(t, aige.RateLimit.TierBindings, 1)
		require.Equal(t, "premium", aige.RateLimit.TierBindings[0].Tier)
		require.Equal(t, []string{"spiffe://dc1/ns/default/dc/dc1/svc/my-app"}, aige.RateLimit.TierBindings[0].SPIFFEIDs)
	})

	t.Run("pool with identity match rule maps identity fields", func(t *testing.T) {
		igw := minimalIGW("gw", "default", "pool")
		pool := enabledPool("pool", "default")
		pool.Spec.Routing = &v1alpha1.InferencePoolRouting{
			MatchRules: []v1alpha1.InferencePoolMatchRule{
				{
					When: v1alpha1.InferencePoolMatch{
						Identity: &v1alpha1.InferencePoolIdentityMatch{
							Service:   "my-app",
							Partition: "default",
							Namespace: "default",
						},
					},
					Candidates: []string{"claude-3-5-sonnet"},
				},
			},
		}

		r := &InferenceGatewayController{Datacenter: "dc1"}
		entry := r.toConsulConfigEntry(igw, pool)

		aige := entry.(*capi.AIGatewayConfigEntry)
		require.Len(t, aige.Routing.MatchRules, 1)
		id := aige.Routing.MatchRules[0].When.Identity
		require.NotNil(t, id)
		require.Equal(t, "my-app", id.Service)
		require.Equal(t, "default", id.Partition)
		require.Equal(t, "default", id.Namespace)
	})

	t.Run("EnableConsulNamespaces=false does not set Namespace on entry", func(t *testing.T) {
		igw := minimalIGW("gw", "default", "pool")
		pool := enabledPool("pool", "default")

		r := &InferenceGatewayController{
			Datacenter:             "dc1",
			ConsulNamespace:        "my-ns",
			EnableConsulNamespaces: false, // OSS mode
		}
		entry := r.toConsulConfigEntry(igw, pool)

		aige := entry.(*capi.AIGatewayConfigEntry)
		require.Equal(t, "", aige.Namespace,
			"OSS mode: entry.Namespace must be empty when EnableConsulNamespaces=false")
	})

	t.Run("EnableConsulNamespaces=true sets Namespace on entry", func(t *testing.T) {
		igw := minimalIGW("gw", "default", "pool")
		pool := enabledPool("pool", "default")

		r := &InferenceGatewayController{
			Datacenter:             "dc1",
			ConsulNamespace:        "my-ns",
			EnableConsulNamespaces: true, // Enterprise mode
		}
		entry := r.toConsulConfigEntry(igw, pool)

		aige := entry.(*capi.AIGatewayConfigEntry)
		require.Equal(t, "my-ns", aige.Namespace,
			"Enterprise mode: entry.Namespace must be set when EnableConsulNamespaces=true")
	})
}

// ---------------------------------------------------------------------------
// TestNormaliseWindow — normaliseWindow covers every alias and edge case
// ---------------------------------------------------------------------------

func TestNormaliseWindow(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input string
		want  string
	}{
		// ── Already-canonical values pass through unchanged ──────────────────
		{"second", "second"},
		{"minute", "minute"},
		{"hour", "hour"},
		{"day", "day"},
		// ── Go-duration shorthand (the bug: objects stored before the enum fix) ──
		{"1s", "second"},
		{"s", "second"},
		{"1m", "minute"},
		{"m", "minute"},
		{"1h", "hour"},
		{"h", "hour"},
		{"1d", "day"},
		{"d", "day"},
		// ── Common English aliases ────────────────────────────────────────────
		{"sec", "second"},
		{"secs", "second"},
		{"min", "minute"},
		{"mins", "minute"},
		{"hr", "hour"},
		{"hrs", "hour"},
		// ── Case-insensitive ─────────────────────────────────────────────────
		{"MINUTE", "minute"},
		{"Hour", "hour"},
		{"1M", "minute"},
		// ── Empty / unknown → Consul default (minute) ────────────────────────
		{"", "minute"},
		{"15m", "minute"}, // multi-digit duration string → fallback
		{"forever", "minute"},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := normaliseWindow(tc.input)
			require.Equalf(t, tc.want, got,
				"normaliseWindow(%q) = %q, want %q", tc.input, got, tc.want)
		})
	}
}

// TestToConsulLimit_NormaliseIntegration verifies that toConsulLimit
// normalises the Window field before it reaches Consul — the end-to-end
// path that caused the HTTP 500.
func TestToConsulLimit_NormaliseIntegration(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		poolLimit v1alpha1.InferencePoolLimit
		wantUnit  string
	}{
		{"canonical minute", v1alpha1.InferencePoolLimit{Count: 100, Window: "minute"}, "minute"},
		{"legacy 1m", v1alpha1.InferencePoolLimit{Count: 100, Window: "1m"}, "minute"},
		{"legacy 1h", v1alpha1.InferencePoolLimit{Count: 100, Window: "1h"}, "hour"},
		{"legacy 1s", v1alpha1.InferencePoolLimit{Count: 100, Window: "1s"}, "second"},
		{"empty defaults to minute", v1alpha1.InferencePoolLimit{Count: 100, Window: ""}, "minute"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := toConsulLimit(&tc.poolLimit)
			require.NotNil(t, got)
			require.Equal(t, tc.wantUnit, got.Unit,
				"toConsulLimit window=%q → Unit should be %q, got %q",
				tc.poolLimit.Window, tc.wantUnit, got.Unit)
		})
	}
}
