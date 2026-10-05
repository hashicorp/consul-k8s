// Copyright IBM Corp. 2018, 2026
// SPDX-License-Identifier: MPL-2.0

package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	logrtest "github.com/go-logr/logr/testr"
	capi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

func TestInferenceGatewayIdentityUsesGatewayName(t *testing.T) {
	for _, tc := range []struct {
		name      string
		namespace string
	}{
		{"foo", "ns-a"},
		{"foo", "ns-b"},
		{"foo.bar", "ns-a"},
		{"foo-bar", "ns-a"},
		{"b-foo", "a"},
		{"foo", "a-b"},
		{strings.Repeat("a", 63), "ns-a"},
		{strings.Repeat("a", 253), strings.Repeat("n", 63)},
		{strings.Repeat("a", 252) + "b", strings.Repeat("n", 63)},
	} {
		t.Run(tc.name+"/"+tc.namespace, func(t *testing.T) {
			igw := minimalIGW(tc.name, tc.namespace, "pool")
			name := inferenceGatewayName(igw)
			require.Equal(t, tc.name, name)
			require.Empty(t, validation.IsDNS1123Subdomain(name))
			igw.UID = "replacement-uid"
			require.Equal(t, name, inferenceGatewayName(igw), "identity must be stable across recreation")
		})
	}
}

func TestInferenceGatewayConfigOwnership(t *testing.T) {
	for _, operation := range []string{"upsert", "delete"} {
		for _, tc := range []struct {
			name        string
			metaKey     string
			metaValue   string
			readStatus  int
			writeStatus int
			casResult   bool
			wantWrite   bool
			wantErr     bool
		}{
			{name: "owned", casResult: true, wantWrite: true},
			{name: "foreign namespace", metaKey: constants.MetaKeyKubeNS, metaValue: "other"},
			{name: "foreign name", metaKey: constants.MetaKeyKubeName, metaValue: "other"},
			{name: "foreign datacenter", metaKey: common.DatacenterKey, metaValue: "dc2"},
			{name: "recreated resource", metaKey: inferenceGatewayUIDKey, metaValue: "old-uid"},
			{name: "missing UID", metaKey: inferenceGatewayUIDKey},
			{name: "absent", readStatus: http.StatusNotFound, casResult: true},
			{name: "read denied", readStatus: http.StatusForbidden, wantErr: true},
			{name: "CAS conflict", wantWrite: true, wantErr: true},
			{name: "create CAS conflict", readStatus: http.StatusNotFound, wantErr: true},
			{name: "write denied", writeStatus: http.StatusForbidden, wantWrite: true, wantErr: true},
		} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				igw := minimalIGW("foo", "ns-a", "pool")
				pool := enabledPool("pool", "ns-a")
				controller := &InferenceGatewayController{Datacenter: "dc1"}
				entry := controller.toConsulConfigEntry(igw, pool).(*capi.InferenceGatewayConfigEntry)
				entry.ModifyIndex = 42
				if tc.metaKey != "" {
					entry.Meta[tc.metaKey] = tc.metaValue
				}
				var mutations atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					require.Empty(t, req.URL.Query().Get("ns"), "OSS requests must not include a namespace")
					if req.Method == http.MethodGet {
						require.Equal(t, "/v1/config/inference-gateway/"+entry.Name, req.URL.Path)
						if tc.readStatus != 0 {
							http.Error(w, http.StatusText(tc.readStatus), tc.readStatus)
							return
						}
						_ = json.NewEncoder(w).Encode(entry)
						return
					}
					mutations.Add(1)
					if tc.writeStatus != 0 {
						http.Error(w, http.StatusText(tc.writeStatus), tc.writeStatus)
						return
					}
					wantCAS := "42"
					if tc.readStatus == http.StatusNotFound {
						wantCAS = "0"
					}
					require.Equal(t, wantCAS, req.URL.Query().Get("cas"))
					if operation == "upsert" {
						require.Equal(t, http.MethodPut, req.Method)
						var written capi.InferenceGatewayConfigEntry
						require.NoError(t, json.NewDecoder(req.Body).Decode(&written))
						require.Equal(t, string(igw.UID), written.Meta[inferenceGatewayUIDKey])
						require.Equal(t, entry.Name, written.Name)
					} else {
						require.Equal(t, http.MethodDelete, req.Method)
						require.Equal(t, "/v1/config/inference-gateway/"+entry.Name, req.URL.Path)
					}
					_ = json.NewEncoder(w).Encode(tc.casResult)
				}))
				defer server.Close()
				consulClient, err := capi.NewClient(&capi.Config{Address: server.URL})
				require.NoError(t, err)
				wantWrite := tc.wantWrite || operation == "upsert" && tc.readStatus == http.StatusNotFound
				wantErr := tc.wantErr || operation == "upsert" && tc.metaKey != ""
				if operation == "delete" && tc.readStatus == http.StatusNotFound {
					wantErr = false
				}
				if operation == "upsert" {
					err = controller.upsertConfigEntry(context.Background(), consulClient, igw, pool, logrtest.New(t))
				} else {
					err = controller.deleteConfigEntry(context.Background(), consulClient, igw, logrtest.New(t))
				}
				if wantErr {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
				if wantWrite {
					require.EqualValues(t, 1, mutations.Load())
				} else {
					require.Zero(t, mutations.Load(), "foreign entries must remain untouched")
				}
			})
		}
	}
}

func TestInferenceGatewayNamespaceWiring(t *testing.T) {
	for _, tc := range []struct {
		name, destination, prefix, wantNamespace, wantLogin string
		enabled, mirror                                     bool
	}{
		{name: "OSS", destination: "ignored"},
		{name: "Enterprise default", enabled: true, destination: "default", wantNamespace: "default", wantLogin: "default"},
		{name: "Enterprise destination", enabled: true, destination: "shared", wantNamespace: "shared", wantLogin: "shared"},
		{name: "Enterprise mirrored", enabled: true, mirror: true, destination: "ignored", wantNamespace: "ns-a", wantLogin: "default"},
		{name: "Enterprise mirrored prefix", enabled: true, mirror: true, prefix: "k8s-", wantNamespace: "k8s-ns-a", wantLogin: "default"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			igw := minimalIGW("foo", "ns-a", "pool")
			pool := enabledPool("pool", "ns-a")
			k8sClient := fake.NewClientBuilder().WithScheme(igwScheme(t)).Build()
			cfg, watcher := consulMockServer(t)
			controller := igwController(t, k8sClient, cfg, watcher)
			controller.EnableConsulNamespaces = tc.enabled
			controller.EnableK8SNSMirroring = tc.mirror
			controller.ConsulNamespace = tc.destination
			controller.NSMirroringPrefix = tc.prefix
			controller.AuthMethod = "k8s-auth"
			if tc.enabled {
				controller.ConsulPartition = "part-a"
			}

			entry := controller.toConsulConfigEntry(igw, pool)
			require.Equal(t, tc.wantNamespace, entry.GetNamespace())
			require.NoError(t, controller.reconcileServiceAccount(context.Background(), igw))
			require.NoError(t, controller.reconcileDeployment(context.Background(), igw, pool))
			dep := &appsv1.Deployment{}
			require.NoError(t, k8sClient.Get(context.Background(), types.NamespacedName{Namespace: igw.Namespace, Name: igw.Name}, dep))
			require.Equal(t, tc.wantNamespace, dep.Spec.Template.Annotations[constants.AnnotationGatewayNamespace])
			require.Equal(t, entry.GetName(), dep.Spec.Template.Annotations[constants.AnnotationGatewayConsulServiceName])
			require.Equal(t, entry.GetName(), dep.Spec.Template.Spec.ServiceAccountName)
			gwArgs := strings.Join(dep.Spec.Template.Spec.Containers[1].Args, " ")
			require.Contains(t, gwArgs, "-uds-path=/run/consul/ext_proc.sock")
			require.NotContains(t, gwArgs, "-config-entry=")
			require.NotContains(t, gwArgs, "-consul-http-addr=")
			require.Equal(t, sidecarUserAndGroupID, *dep.Spec.Template.Spec.Containers[0].SecurityContext.RunAsUser)
			require.Equal(t, sidecarUserAndGroupID, *dep.Spec.Template.Spec.Containers[1].SecurityContext.RunAsUser)
			require.Contains(t, dep.Spec.Template.Spec.InitContainers[0].Command[2], "-service-name="+entry.GetName())
			sa := &corev1.ServiceAccount{}
			require.NoError(t, k8sClient.Get(context.Background(), types.NamespacedName{Namespace: igw.Namespace, Name: dep.Spec.Template.Spec.ServiceAccountName}, sa))
			require.True(t, metav1.IsControlledBy(sa, igw))
			initEnv := map[string]string{}
			for _, env := range dep.Spec.Template.Spec.InitContainers[0].Env {
				initEnv[env.Name] = env.Value
			}
			require.Equal(t, tc.wantNamespace, initEnv["CONSUL_NAMESPACE"])
			require.Equal(t, tc.wantLogin, initEnv["CONSUL_LOGIN_NAMESPACE"])
			require.Equal(t, controller.ConsulPartition, initEnv["CONSUL_PARTITION"])
			require.Equal(t, controller.ConsulPartition, initEnv["CONSUL_LOGIN_PARTITION"])
			args := dep.Spec.Template.Spec.Containers[0].Args
			if tc.enabled {
				require.Contains(t, args, "-service-namespace="+tc.wantNamespace)
				require.Contains(t, args, "-login-namespace="+tc.wantLogin)
				require.Contains(t, args, "-service-partition=part-a")
				require.Contains(t, args, "-login-partition=part-a")
			} else {
				require.NotContains(t, strings.Join(args, " "), "-service-namespace")
				require.NotContains(t, strings.Join(args, " "), "-login-namespace")
			}
			require.Contains(t, dep.Spec.Template.Spec.Containers[1].Env, corev1.EnvVar{Name: "CONSUL_NAMESPACE", Value: tc.wantNamespace})
			require.Contains(t, dep.Spec.Template.Spec.Containers[1].Env, corev1.EnvVar{Name: "CONSUL_PARTITION", Value: controller.ConsulPartition})

			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasPrefix(req.URL.Path, "/v1/namespace/") {
					_ = json.NewEncoder(w).Encode(capi.Namespace{Name: tc.wantNamespace})
					return
				}
				requests.Add(1)
				require.Equal(t, tc.wantNamespace, req.URL.Query().Get("ns"))
				require.Equal(t, controller.ConsulPartition, req.URL.Query().Get("partition"))
				if req.Method == http.MethodGet {
					stored := controller.toConsulConfigEntry(igw, pool).(*capi.InferenceGatewayConfigEntry)
					stored.ModifyIndex = 42
					_ = json.NewEncoder(w).Encode(stored)
				} else {
					_ = json.NewEncoder(w).Encode(true)
				}
			}))
			defer server.Close()
			consulClient, err := capi.NewClient(&capi.Config{Address: server.URL})
			require.NoError(t, err)
			require.NoError(t, controller.upsertConfigEntry(context.Background(), consulClient, igw, pool, logrtest.New(t)))
			require.NoError(t, controller.deleteConfigEntry(context.Background(), consulClient, igw, logrtest.New(t)))
			require.EqualValues(t, 4, requests.Load())
		})
	}
}

func TestInferenceGatewayDeploymentNamespaceMigration(t *testing.T) {
	ctx := context.Background()
	igw := minimalIGW("foo", "ns-a", "pool")
	pool := enabledPool("pool", "ns-a")
	dep := deploymentFor(igw, pool, "dp", "k8s", "igw", 8443, corev1.ResourceRequirements{}, sidecarUserAndGroupID, sidecarUserAndGroupID, deploymentConsulConfig{})
	dep.UID = "existing-deployment"
	dep.Spec.Template.Spec.ServiceAccountName = igw.Name
	dep.Spec.Template.Annotations[constants.AnnotationGatewayConsulServiceName] = igw.Name
	dep.Spec.Template.Annotations[constants.AnnotationGatewayNamespace] = "default"
	dep.Spec.Template.Annotations["custom-annotation"] = "preserve"
	k8sClient := fake.NewClientBuilder().WithScheme(igwScheme(t)).WithObjects(dep).Build()
	cfg, watcher := consulMockServer(t)
	controller := igwController(t, k8sClient, cfg, watcher)
	controller.EnableConsulNamespaces = true
	controller.EnableK8SNSMirroring = true
	controller.NSMirroringPrefix = "k8s-"
	require.NoError(t, controller.reconcileDeployment(ctx, igw, pool))
	got := &appsv1.Deployment{}
	require.NoError(t, k8sClient.Get(ctx, types.NamespacedName{Namespace: igw.Namespace, Name: igw.Name}, got))
	require.Equal(t, dep.UID, got.UID, "migration must patch the Deployment rather than delete it")
	require.Equal(t, inferenceGatewayName(igw), got.Spec.Template.Spec.ServiceAccountName)
	require.Equal(t, inferenceGatewayName(igw), got.Spec.Template.Annotations[constants.AnnotationGatewayConsulServiceName])
	require.Equal(t, "k8s-ns-a", got.Spec.Template.Annotations[constants.AnnotationGatewayNamespace])
	require.Contains(t, got.Spec.Template.Spec.Containers[0].Args, "-service-namespace=k8s-ns-a")
	require.Equal(t, "preserve", got.Spec.Template.Annotations["custom-annotation"])
}

func TestInferenceGatewayReconcileClaimsIdentityBeforeCreatingWorkloads(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		wantError string
	}{
		{name: "conflicting owner", wantError: "refusing to overwrite"},
		{name: "Consul unavailable", status: http.StatusServiceUnavailable, wantError: "ConfigEntries().Get"},
		{name: "unsupported config entry", status: http.StatusBadRequest, wantError: "ConfigEntries().Get"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			igw := minimalIGW("foo", "ns-b", "pool")
			igw.Finalizers = []string{inferenceGatewayFinalizer}
			pool := enabledPool("pool", "ns-b")
			k8sClient := fake.NewClientBuilder().WithScheme(igwScheme(t)).
				WithObjects(igw, pool).WithStatusSubresource(igw).Build()
			cfg, watcher := consulMockServerWithHandler(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				require.Equal(t, http.MethodGet, req.Method, "must not mutate an unclaimed identity")
				if tc.status != 0 {
					http.Error(w, "invalid config entry kind: inference-gateway", tc.status)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				owner := minimalIGW("foo", "ns-a", "pool")
				entry := (&InferenceGatewayController{Datacenter: "dc1"}).toConsulConfigEntry(owner, pool)
				require.NoError(t, json.NewEncoder(w).Encode(entry))
			}))
			controller := igwController(t, k8sClient, cfg, watcher)
			_, err := controller.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Name: igw.Name, Namespace: igw.Namespace},
			})
			require.ErrorContains(t, err, tc.wantError)
			deployments := &appsv1.DeploymentList{}
			require.NoError(t, k8sClient.List(context.Background(), deployments))
			require.Empty(t, deployments.Items)
			services := &corev1.ServiceList{}
			require.NoError(t, k8sClient.List(context.Background(), services))
			require.Empty(t, services.Items)
			accounts := &corev1.ServiceAccountList{}
			require.NoError(t, k8sClient.List(context.Background(), accounts))
			require.Empty(t, accounts.Items)
		})
	}
}

func TestInferenceGatewayConfigIsolation(t *testing.T) {
	for _, mode := range []string{"OSS", "shared namespace", "mirrored namespaces"} {
		t.Run(mode, func(t *testing.T) {
			var mu sync.Mutex
			entries := map[string]*capi.InferenceGatewayConfigEntry{}
			var index uint64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				if strings.HasPrefix(req.URL.Path, "/v1/namespace/") {
					_ = json.NewEncoder(w).Encode(capi.Namespace{Name: strings.TrimPrefix(req.URL.Path, "/v1/namespace/")})
					return
				}
				namespace := req.URL.Query().Get("ns")
				name := strings.TrimPrefix(req.URL.Path, "/v1/config/inference-gateway/")
				if req.Method == http.MethodPut {
					var entry capi.InferenceGatewayConfigEntry
					if err := json.NewDecoder(req.Body).Decode(&entry); err != nil {
						http.Error(w, err.Error(), http.StatusBadRequest)
						return
					}
					key := namespace + "/" + entry.Name
					expected := "0"
					if old := entries[key]; old != nil {
						expected = strconv.FormatUint(old.ModifyIndex, 10)
					}
					if req.URL.Query().Get("cas") != expected {
						_ = json.NewEncoder(w).Encode(false)
						return
					}
					index++
					entry.ModifyIndex = index
					entries[key] = &entry
					_ = json.NewEncoder(w).Encode(true)
					return
				}
				key := namespace + "/" + name
				entry := entries[key]
				if entry == nil {
					http.Error(w, "not found", http.StatusNotFound)
					return
				}
				switch req.Method {
				case http.MethodGet:
					_ = json.NewEncoder(w).Encode(entry)
				case http.MethodDelete:
					if req.URL.Query().Get("cas") != strconv.FormatUint(entry.ModifyIndex, 10) {
						_ = json.NewEncoder(w).Encode(false)
						return
					}
					delete(entries, key)
					_ = json.NewEncoder(w).Encode(true)
				default:
					http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
				}
			}))
			defer server.Close()
			consulClient, err := capi.NewClient(&capi.Config{Address: server.URL})
			require.NoError(t, err)
			controller := &InferenceGatewayController{Datacenter: "dc1", ConsulNamespace: "shared",
				EnableConsulNamespaces: mode != "OSS", EnableK8SNSMirroring: mode == "mirrored namespaces", NSMirroringPrefix: "k8s-"}
			gateways := []*v1alpha1.InferenceGateway{
				minimalIGW("foo", "ns-a", "pool"),
				minimalIGW("foo", "ns-b", "pool"),
				minimalIGW("bar", "ns-a", "pool"),
			}
			ctx := context.Background()
			log := logrtest.New(t)
			for _, igw := range gateways {
				err := controller.upsertConfigEntry(ctx, consulClient, igw, enabledPool("pool", igw.Namespace), log)
				if igw == gateways[1] && mode != "mirrored namespaces" {
					require.ErrorContains(t, err, "refusing to overwrite")
					continue
				}
				require.NoError(t, err)
			}
			if mode != "mirrored namespaces" {
				require.NoError(t, controller.deleteConfigEntry(ctx, consulClient, gateways[1], log))
				entry, _, err := consulClient.ConfigEntries().Get(capi.InferenceGateway, gateways[0].Name,
					&capi.QueryOptions{Namespace: controller.consulNamespace(gateways[0].Namespace)})
				require.NoError(t, err)
				require.Equal(t, "ns-a", entry.GetMeta()[constants.MetaKeyKubeNS])
				require.Equal(t, "closed", entry.(*capi.InferenceGatewayConfigEntry).Processor.FailureMode)
				return
			}
			updatedPool := enabledPool("pool", "ns-b")
			updatedPool.Spec.Processor = &v1alpha1.InferencePoolProcessor{FailureMode: "open"}
			require.NoError(t, controller.upsertConfigEntry(ctx, consulClient, gateways[1], updatedPool, log))
			require.NoError(t, controller.deleteConfigEntry(ctx, consulClient, gateways[0], log))
			for i, igw := range gateways {
				entry, _, err := consulClient.ConfigEntries().Get(capi.InferenceGateway, inferenceGatewayName(igw),
					&capi.QueryOptions{Namespace: controller.consulNamespace(igw.Namespace)})
				if i == 0 {
					require.Error(t, err)
					require.True(t, isConsulNotFoundErr(err))
					continue
				}
				require.NoError(t, err)
				require.Equal(t, string(igw.UID), entry.GetMeta()[inferenceGatewayUIDKey])
				wantMode := "closed"
				if i == 1 {
					wantMode = "open"
				}
				require.Equal(t, wantMode, entry.(*capi.InferenceGatewayConfigEntry).Processor.FailureMode)
			}
		})
	}
}

func TestInferenceGatewayOpenShiftSharesNamespaceUID(t *testing.T) {
	igw := minimalIGW("foo", "ns-a", "pool")
	pool := enabledPool("pool", "ns-a")
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ns-a",
			Annotations: map[string]string{
				constants.AnnotationOpenShiftUIDRange: "1000700000/5",
				constants.AnnotationOpenShiftGroups:   "1000700000/5",
			},
		},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(igwScheme(t)).WithObjects(ns).Build()
	cfg, watcher := consulMockServer(t)
	controller := igwController(t, k8sClient, cfg, watcher)
	controller.EnableOpenShift = true

	require.NoError(t, controller.reconcileDeployment(context.Background(), igw, pool))
	dep := &appsv1.Deployment{}
	require.NoError(t, k8sClient.Get(context.Background(), types.NamespacedName{Namespace: igw.Namespace, Name: igw.Name}, dep))
	// 1000700000/5 is 1000700000..1000700004; mesh sidecar identity is the second-to-last id.
	const want int64 = 1000700003
	for _, c := range dep.Spec.Template.Spec.Containers {
		require.Equal(t, want, *c.SecurityContext.RunAsUser)
		require.Equal(t, want, *c.SecurityContext.RunAsGroup)
	}
	require.NotEqual(t, sidecarUserAndGroupID, want)
}

func TestInferenceGatewayOpenShiftRequiresNamespaceRange(t *testing.T) {
	igw := minimalIGW("foo", "ns-a", "pool")
	pool := enabledPool("pool", "ns-a")
	cfg, watcher := consulMockServer(t)

	t.Run("missing namespace", func(t *testing.T) {
		k8sClient := fake.NewClientBuilder().WithScheme(igwScheme(t)).Build()
		controller := igwController(t, k8sClient, cfg, watcher)
		controller.EnableOpenShift = true
		err := controller.reconcileDeployment(context.Background(), igw, pool)
		require.Error(t, err)
		require.Contains(t, err.Error(), "reading namespace")
	})

	t.Run("invalid range", func(t *testing.T) {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			Name:        "ns-a",
			Annotations: map[string]string{constants.AnnotationOpenShiftUIDRange: "not-a-range"},
		}}
		k8sClient := fake.NewClientBuilder().WithScheme(igwScheme(t)).WithObjects(ns).Build()
		controller := igwController(t, k8sClient, cfg, watcher)
		controller.EnableOpenShift = true
		err := controller.reconcileDeployment(context.Background(), igw, pool)
		require.Error(t, err)
		require.Contains(t, err.Error(), "openshift sidecar uid")
	})

	t.Run("invalid group range", func(t *testing.T) {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			Name: "ns-a",
			Annotations: map[string]string{
				constants.AnnotationOpenShiftUIDRange: "1000700000/5",
				constants.AnnotationOpenShiftGroups:   "not-a-range",
			},
		}}
		k8sClient := fake.NewClientBuilder().WithScheme(igwScheme(t)).WithObjects(ns).Build()
		controller := igwController(t, k8sClient, cfg, watcher)
		controller.EnableOpenShift = true
		err := controller.reconcileDeployment(context.Background(), igw, pool)
		require.Error(t, err)
		require.Contains(t, err.Error(), "openshift sidecar gid")
	})
}
