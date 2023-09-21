// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

//go:build enterprise

package pod

import (
	"context"
	"testing"

	mapset "github.com/deckarep/golang-set"
	logrtest "github.com/go-logr/logr/testr"
	capi "github.com/hashicorp/consul/api"
	pbcatalog "github.com/hashicorp/consul/proto-public/pbcatalog/v1alpha1"
	pbmesh "github.com/hashicorp/consul/proto-public/pbmesh/v1alpha1"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/metrics"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

const (
	testPodName   = "foo"
	testPartition = "my-partition"
)

type testCase struct {
	name         string
	podName      string // This needs to be aligned with the pod created in `k8sObjects`
	podNamespace string // Defaults to metav1.NamespaceDefault if empty.
	partition    string

	k8sObjects func() []runtime.Object // testing node is injected separately

	// Pod Controller Settings
	acls            bool
	tproxy          bool
	overwriteProbes bool
	metrics         bool
	telemetry       bool

	namespaceMirroring   bool
	namespaceDestination string
	namespacePrefix      string

	// Initial Consul state.
	existingConsulNamespace    string // This namespace will be populated before the test is executed.
	existingWorkload           *pbcatalog.Workload
	existingHealthStatus       *pbcatalog.HealthStatus
	existingProxyConfiguration *pbmesh.ProxyConfiguration
	existingUpstreams          *pbmesh.Upstreams

	// Expected Consul state.
	expectedConsulNamespace    string // This namespace will be used to query Consul for the results
	expectedWorkload           *pbcatalog.Workload
	expectedHealthStatus       *pbcatalog.HealthStatus
	expectedProxyConfiguration *pbmesh.ProxyConfiguration
	expectedUpstreams          *pbmesh.Upstreams

	// Reconcile loop outputs
	expErr     string
	expRequeue bool // The response from the reconcile function
}

// TestReconcileCreatePodWithMirrorNamespaces creates a Pod object in a non-default NS and Partition
// with namespaces set to mirroring
func TestReconcileCreatePodWithMirrorNamespaces(t *testing.T) {
	t.Parallel()

	testCases := []testCase{
		{
			name:      "kitchen sink new pod, ns and partition",
			podName:   testPodName,
			partition: constants.DefaultConsulPartition,

			k8sObjects: func() []runtime.Object {
				pod := createPod(testPodName, metav1.NamespaceDefault, true, true)
				addProbesAndOriginalPodAnnotation(pod)

				return []runtime.Object{pod}
			},
			tproxy:          true,
			telemetry:       true,
			metrics:         true,
			overwriteProbes: true,

			namespaceMirroring: true,

			expectedConsulNamespace:    constants.DefaultConsulNS,
			expectedWorkload:           createWorkload(),
			expectedHealthStatus:       createPassingHealthStatus(),
			expectedProxyConfiguration: createProxyConfiguration(testPodName, pbmesh.ProxyMode_PROXY_MODE_TRANSPARENT),
		},
		{
			name:         "kitchen sink new pod, non-default ns and partition",
			podName:      testPodName,
			podNamespace: "bar",
			partition:    testPartition,

			k8sObjects: func() []runtime.Object {
				pod := createPod(testPodName, "bar", true, true)
				addProbesAndOriginalPodAnnotation(pod)

				return []runtime.Object{pod}
			},
			tproxy:          true,
			telemetry:       true,
			metrics:         true,
			overwriteProbes: true,

			namespaceMirroring: true,

			existingConsulNamespace: "bar",

			expectedConsulNamespace:    "bar",
			expectedWorkload:           createWorkload(),
			expectedHealthStatus:       createPassingHealthStatus(),
			expectedProxyConfiguration: createProxyConfiguration(testPodName, pbmesh.ProxyMode_PROXY_MODE_TRANSPARENT),
		},
		{
			name:         "new pod with namespace prefix",
			podName:      testPodName,
			podNamespace: "bar",
			partition:    testPartition,

			k8sObjects: func() []runtime.Object {
				pod := createPod(testPodName, "bar", true, true)
				addProbesAndOriginalPodAnnotation(pod)

				return []runtime.Object{pod}
			},

			namespaceMirroring: true,
			namespacePrefix:    "foo-",

			existingConsulNamespace: "foo-bar",

			expectedConsulNamespace: "foo-bar",
			expectedWorkload:        createWorkload(),
			expectedHealthStatus:    createPassingHealthStatus(),
		},
		{
			name:         "namespace mirroring overrides destination namespace",
			podName:      testPodName,
			podNamespace: "bar",
			partition:    testPartition,

			k8sObjects: func() []runtime.Object {
				pod := createPod(testPodName, "bar", true, true)
				addProbesAndOriginalPodAnnotation(pod)

				return []runtime.Object{pod}
			},

			namespaceMirroring:   true,
			namespaceDestination: "supernova",

			existingConsulNamespace: "bar",

			expectedConsulNamespace: "bar",
			expectedWorkload:        createWorkload(),
			expectedHealthStatus:    createPassingHealthStatus(),
		},
		{
			name:      "new pod with explicit upstreams, ns and partition",
			podName:   testPodName,
			partition: constants.DefaultConsulPartition,

			k8sObjects: func() []runtime.Object {
				pod := createPod(testPodName, metav1.NamespaceDefault, true, true)
				addProbesAndOriginalPodAnnotation(pod)
				pod.Annotations[constants.AnnotationMeshDestinations] = "myPort.port.mySVC.svc:24601"
				return []runtime.Object{pod}
			},
			tproxy:          false,
			telemetry:       true,
			metrics:         true,
			overwriteProbes: true,

			namespaceMirroring: true,

			expectedConsulNamespace:    constants.DefaultConsulNS,
			expectedWorkload:           createWorkload(),
			expectedHealthStatus:       createPassingHealthStatus(),
			expectedProxyConfiguration: createProxyConfiguration(testPodName, pbmesh.ProxyMode_PROXY_MODE_DEFAULT),
			expectedUpstreams:          createUpstreams(),
		},
		{
			name:         "namespace in Consul does not exist",
			podName:      testPodName,
			podNamespace: "bar",
			partition:    testPartition,

			k8sObjects: func() []runtime.Object {
				pod := createPod(testPodName, "bar", true, true)
				return []runtime.Object{pod}
			},

			namespaceMirroring: true,

			// The equivalent namespace in Consul does not exist, so requeue for backoff.
			expRequeue: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			runControllerTest(t, tc)
		})
	}
}

// TestReconcileUpdatePodWithMirrorNamespaces updates a Pod object in a non-default NS and Partition
// with namespaces set to mirroring.
func TestReconcileUpdatePodWithMirrorNamespaces(t *testing.T) {
	t.Parallel()

	testCases := []testCase{
		{
			name:         "update pod health",
			podName:      testPodName,
			podNamespace: "bar",
			partition:    testPartition,

			k8sObjects: func() []runtime.Object {
				pod := createPod(testPodName, "bar", true, false) // failing
				return []runtime.Object{pod}
			},

			namespaceMirroring: true,
			namespacePrefix:    "foo-",

			existingConsulNamespace: "foo-bar",
			existingWorkload:        createWorkload(),
			existingHealthStatus:    createPassingHealthStatus(),

			expectedConsulNamespace: "foo-bar",
			expectedWorkload:        createWorkload(),
			expectedHealthStatus:    createCriticalHealthStatus(testPodName, "bar"),
		},
		{
			name:         "duplicated pod event",
			podName:      testPodName,
			podNamespace: "bar",
			partition:    testPartition,

			k8sObjects: func() []runtime.Object {
				pod := createPod(testPodName, "bar", true, true)
				addProbesAndOriginalPodAnnotation(pod)

				return []runtime.Object{pod}
			},

			namespaceMirroring: true,

			tproxy:          true,
			telemetry:       true,
			metrics:         true,
			overwriteProbes: true,

			existingConsulNamespace:    "bar",
			existingWorkload:           createWorkload(),
			existingHealthStatus:       createPassingHealthStatus(),
			existingProxyConfiguration: createProxyConfiguration(testPodName, pbmesh.ProxyMode_PROXY_MODE_TRANSPARENT),

			expectedConsulNamespace:    "bar",
			expectedWorkload:           createWorkload(),
			expectedHealthStatus:       createPassingHealthStatus(),
			expectedProxyConfiguration: createProxyConfiguration(testPodName, pbmesh.ProxyMode_PROXY_MODE_TRANSPARENT),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			runControllerTest(t, tc)
		})
	}
}

// TestReconcileDeletePodWithMirrorNamespaces deletes a Pod object in a non-default NS and Partition
// with namespaces set to mirroring.
func TestReconcileDeletePodWithMirrorNamespaces(t *testing.T) {
	t.Parallel()

	testCases := []testCase{
		{
			name:         "delete kitchen sink pod",
			podName:      testPodName,
			podNamespace: "bar",
			partition:    testPartition,

			tproxy:          true,
			telemetry:       true,
			metrics:         true,
			overwriteProbes: true,

			namespaceMirroring: true,

			existingConsulNamespace:    "bar",
			existingWorkload:           createWorkload(),
			existingHealthStatus:       createPassingHealthStatus(),
			existingProxyConfiguration: createProxyConfiguration(testPodName, pbmesh.ProxyMode_PROXY_MODE_TRANSPARENT),

			expectedConsulNamespace: "bar",
		},
		{
			name:         "delete pod w/ explicit upstreams",
			podName:      testPodName,
			podNamespace: "bar",
			partition:    testPartition,

			telemetry:       true,
			metrics:         true,
			overwriteProbes: true,

			namespaceMirroring: true,

			existingConsulNamespace:    "bar",
			existingWorkload:           createWorkload(),
			existingHealthStatus:       createPassingHealthStatus(),
			existingProxyConfiguration: createProxyConfiguration(testPodName, pbmesh.ProxyMode_PROXY_MODE_DEFAULT),
			existingUpstreams:          createUpstreams(),

			expectedConsulNamespace: "bar",
		},
		{
			name:         "delete pod with namespace prefix",
			podName:      testPodName,
			podNamespace: "bar",
			partition:    testPartition,

			namespaceMirroring: true,
			namespacePrefix:    "foo-",

			existingConsulNamespace: "foo-bar",
			existingWorkload:        createWorkload(),
			existingHealthStatus:    createPassingHealthStatus(),

			expectedConsulNamespace: "foo-bar",
		},
		{
			name:         "resources are already gone in Consul",
			podName:      testPodName,
			podNamespace: "bar",
			partition:    testPartition,

			tproxy:          true,
			telemetry:       true,
			metrics:         true,
			overwriteProbes: true,

			namespaceMirroring: true,

			existingConsulNamespace: "bar",

			expectedConsulNamespace: "bar",
		},
		{
			name:         "namespace is already missing in Consul",
			podName:      testPodName,
			podNamespace: "bar",
			partition:    testPartition,

			namespaceMirroring: true,

			expectedConsulNamespace: "bar",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			runControllerTest(t, tc)
		})
	}
}

// TestReconcileCreatePodWithDestinationNamespace creates a Pod object in a non-default NS and Partition
// with namespaces set to a destination.
func TestReconcileCreatePodWithDestinationNamespace(t *testing.T) {
	t.Parallel()

	testCases := []testCase{
		{
			name:      "kitchen sink new pod, ns and partition",
			podName:   testPodName,
			partition: constants.DefaultConsulPartition,

			k8sObjects: func() []runtime.Object {
				pod := createPod(testPodName, metav1.NamespaceDefault, true, true)
				addProbesAndOriginalPodAnnotation(pod)

				return []runtime.Object{pod}
			},
			tproxy:          true,
			telemetry:       true,
			metrics:         true,
			overwriteProbes: true,

			namespaceDestination: constants.DefaultConsulNS,

			existingConsulNamespace: constants.DefaultConsulNS,

			expectedConsulNamespace:    constants.DefaultConsulNS,
			expectedWorkload:           createWorkload(),
			expectedHealthStatus:       createPassingHealthStatus(),
			expectedProxyConfiguration: createProxyConfiguration(testPodName, pbmesh.ProxyMode_PROXY_MODE_TRANSPARENT),
		},
		{
			name:      "new pod with explicit upstreams, ns and partition",
			podName:   testPodName,
			partition: constants.DefaultConsulPartition,

			k8sObjects: func() []runtime.Object {
				pod := createPod(testPodName, metav1.NamespaceDefault, true, true)
				addProbesAndOriginalPodAnnotation(pod)
				pod.Annotations[constants.AnnotationMeshDestinations] = "myPort.port.mySVC.svc:24601"
				return []runtime.Object{pod}
			},
			telemetry:       true,
			metrics:         true,
			overwriteProbes: true,

			namespaceDestination: constants.DefaultConsulNS,

			existingConsulNamespace: constants.DefaultConsulNS,

			expectedConsulNamespace:    constants.DefaultConsulNS,
			expectedWorkload:           createWorkload(),
			expectedHealthStatus:       createPassingHealthStatus(),
			expectedProxyConfiguration: createProxyConfiguration(testPodName, pbmesh.ProxyMode_PROXY_MODE_DEFAULT),
			expectedUpstreams:          createUpstreams(),
		},
		{
			name:         "kitchen sink new pod, non-default ns and partition",
			podName:      testPodName,
			podNamespace: "bar",
			partition:    testPartition,

			k8sObjects: func() []runtime.Object {
				pod := createPod(testPodName, "bar", true, true)
				addProbesAndOriginalPodAnnotation(pod)

				return []runtime.Object{pod}
			},
			tproxy:          true,
			telemetry:       true,
			metrics:         true,
			overwriteProbes: true,

			namespaceDestination: "a-penguin-walks-into-a-bar",

			existingConsulNamespace: "a-penguin-walks-into-a-bar",

			expectedConsulNamespace:    "a-penguin-walks-into-a-bar",
			expectedWorkload:           createWorkload(),
			expectedHealthStatus:       createPassingHealthStatus(),
			expectedProxyConfiguration: createProxyConfiguration(testPodName, pbmesh.ProxyMode_PROXY_MODE_TRANSPARENT),
		},
		{
			name:         "namespace in Consul does not exist",
			podName:      testPodName,
			podNamespace: "bar",
			partition:    testPartition,

			k8sObjects: func() []runtime.Object {
				pod := createPod(testPodName, "bar", true, true)
				return []runtime.Object{pod}
			},

			namespaceDestination: "a-penguin-walks-into-a-bar",

			// The equivalent namespace in Consul does not exist, so requeue for backoff.
			expRequeue: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			runControllerTest(t, tc)
		})
	}
}

// TestReconcileUpdatePodWithDestinationNamespace updates a Pod object in a non-default NS and Partition
// with namespaces set to a destination.
func TestReconcileUpdatePodWithDestinationNamespace(t *testing.T) {
	t.Parallel()

	testCases := []testCase{
		{
			name:         "update pod health",
			podName:      testPodName,
			podNamespace: "bar",
			partition:    testPartition,

			k8sObjects: func() []runtime.Object {
				pod := createPod(testPodName, "bar", true, false) // failing
				return []runtime.Object{pod}
			},

			namespaceDestination: "a-penguin-walks-into-a-bar",

			existingConsulNamespace: "a-penguin-walks-into-a-bar",
			existingWorkload:        createWorkload(),
			existingHealthStatus:    createPassingHealthStatus(),

			expectedConsulNamespace: "a-penguin-walks-into-a-bar",
			expectedWorkload:        createWorkload(),
			expectedHealthStatus:    createCriticalHealthStatus(testPodName, "bar"),
		},
		{
			name:         "duplicated pod event",
			podName:      testPodName,
			podNamespace: "bar",
			partition:    testPartition,

			k8sObjects: func() []runtime.Object {
				pod := createPod(testPodName, "bar", true, true)
				addProbesAndOriginalPodAnnotation(pod)

				return []runtime.Object{pod}
			},

			namespaceDestination: "a-penguin-walks-into-a-bar",

			tproxy:          true,
			telemetry:       true,
			metrics:         true,
			overwriteProbes: true,

			existingConsulNamespace:    "a-penguin-walks-into-a-bar",
			existingWorkload:           createWorkload(),
			existingHealthStatus:       createPassingHealthStatus(),
			existingProxyConfiguration: createProxyConfiguration(testPodName, pbmesh.ProxyMode_PROXY_MODE_TRANSPARENT),

			expectedConsulNamespace:    "a-penguin-walks-into-a-bar",
			expectedWorkload:           createWorkload(),
			expectedHealthStatus:       createPassingHealthStatus(),
			expectedProxyConfiguration: createProxyConfiguration(testPodName, pbmesh.ProxyMode_PROXY_MODE_TRANSPARENT),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			runControllerTest(t, tc)
		})
	}
}

// TestReconcileDeletePodWithDestinationNamespace deletes a Pod object in a non-default NS and Partition
// with namespaces set to a destination.
func TestReconcileDeletePodWithDestinationNamespace(t *testing.T) {
	t.Parallel()

	testCases := []testCase{
		{
			name:         "delete kitchen sink pod",
			podName:      testPodName,
			podNamespace: "bar",
			partition:    testPartition,

			tproxy:          true,
			telemetry:       true,
			metrics:         true,
			overwriteProbes: true,

			namespaceDestination: "a-penguin-walks-into-a-bar",

			existingConsulNamespace:    "a-penguin-walks-into-a-bar",
			existingWorkload:           createWorkload(),
			existingHealthStatus:       createPassingHealthStatus(),
			existingProxyConfiguration: createProxyConfiguration(testPodName, pbmesh.ProxyMode_PROXY_MODE_TRANSPARENT),

			expectedConsulNamespace: "a-penguin-walks-into-a-bar",
		},
		{
			name:         "delete pod with explicit upstreams",
			podName:      testPodName,
			podNamespace: "bar",
			partition:    testPartition,

			telemetry:       true,
			metrics:         true,
			overwriteProbes: true,

			namespaceDestination: "a-penguin-walks-into-a-bar",

			existingConsulNamespace:    "a-penguin-walks-into-a-bar",
			existingWorkload:           createWorkload(),
			existingHealthStatus:       createPassingHealthStatus(),
			existingProxyConfiguration: createProxyConfiguration(testPodName, pbmesh.ProxyMode_PROXY_MODE_DEFAULT),
			existingUpstreams:          createUpstreams(),

			expectedConsulNamespace: "a-penguin-walks-into-a-bar",
		},
		{
			name:         "resources are already gone in Consul",
			podName:      testPodName,
			podNamespace: "bar",
			partition:    testPartition,

			tproxy:          true,
			telemetry:       true,
			metrics:         true,
			overwriteProbes: true,

			namespaceDestination: "a-penguin-walks-into-a-bar",

			existingConsulNamespace: "a-penguin-walks-into-a-bar",

			expectedConsulNamespace: "a-penguin-walks-into-a-bar",
		},
		{
			name:         "namespace is already missing in Consul",
			podName:      testPodName,
			podNamespace: "bar",
			partition:    testPartition,

			namespaceDestination: "a-penguin-walks-into-a-bar",

			expectedConsulNamespace: "a-penguin-walks-into-a-bar",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			runControllerTest(t, tc)
		})
	}
}

func runControllerTest(t *testing.T, tc testCase) {

	ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: metav1.NamespaceDefault,
	}}
	nsBar := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: "bar",
	}}
	node := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}

	k8sObjects := []runtime.Object{
		&ns,
		&nsBar,
		&node,
	}
	if tc.k8sObjects != nil {
		k8sObjects = append(k8sObjects, tc.k8sObjects()...)
	}

	fakeClient := fake.NewClientBuilder().WithRuntimeObjects(k8sObjects...).Build()

	// Create test consulServer server.
	adminToken := "123e4567-e89b-12d3-a456-426614174000"
	testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
		c.Experiments = []string{"resource-apis"}
		if tc.acls {
			c.ACL.Enabled = tc.acls
			c.ACL.Tokens.InitialManagement = adminToken
		}
	})

	resourceClient, err := consul.NewResourceServiceClient(testClient.Watcher)
	require.NoError(t, err)

	// Create the partition in Consul.
	if tc.partition != "" {
		testClient.Cfg.APIClientConfig.Partition = tc.partition

		partition := &capi.Partition{
			Name: tc.partition,
		}
		_, _, err := testClient.APIClient.Partitions().Create(context.Background(), partition, nil)
		require.NoError(t, err)
	}

	// Create the namespace in Consul if specified.
	if tc.existingConsulNamespace != "" {
		namespace := &capi.Namespace{
			Name:      tc.existingConsulNamespace,
			Partition: tc.partition,
		}

		_, _, err := testClient.APIClient.Namespaces().Create(namespace, nil)
		require.NoError(t, err)
	}

	// Create the pod controller.
	pc := &Controller{
		Client:              fakeClient,
		Log:                 logrtest.New(t),
		ConsulClientConfig:  testClient.Cfg,
		ConsulServerConnMgr: testClient.Watcher,
		K8sNamespaceConfig: common.K8sNamespaceConfig{
			AllowK8sNamespacesSet: mapset.NewSetWith("*"),
			DenyK8sNamespacesSet:  mapset.NewSetWith(),
		},
		ConsulTenancyConfig: common.ConsulTenancyConfig{
			EnableConsulNamespaces:     true,
			NSMirroringPrefix:          tc.namespacePrefix,
			EnableNSMirroring:          tc.namespaceMirroring,
			ConsulDestinationNamespace: tc.namespaceDestination,
			EnableConsulPartitions:     true,
			ConsulPartition:            tc.partition,
		},
		TProxyOverwriteProbes:    tc.overwriteProbes,
		EnableTransparentProxy:   tc.tproxy,
		EnableTelemetryCollector: tc.telemetry,
	}
	if tc.metrics {
		pc.MetricsConfig = metrics.Config{
			DefaultEnableMetrics:        true,
			DefaultPrometheusScrapePort: "1234",
		}
	}
	if tc.acls {
		pc.AuthMethod = test.AuthMethod
	}

	podNamespace := tc.podNamespace
	if podNamespace == "" {
		podNamespace = metav1.NamespaceDefault
	}

	workloadID := getWorkloadID(tc.podName, tc.expectedConsulNamespace, tc.partition)
	loadResource(t, resourceClient, workloadID, tc.existingWorkload, nil)
	loadResource(
		t,
		resourceClient,
		getHealthStatusID(tc.podName, tc.expectedConsulNamespace, tc.partition),
		tc.existingHealthStatus,
		workloadID)
	loadResource(
		t,
		resourceClient,
		getProxyConfigurationID(tc.podName, tc.expectedConsulNamespace, tc.partition),
		tc.existingProxyConfiguration,
		nil)
	loadResource(
		t,
		resourceClient,
		getUpstreamsID(tc.podName, tc.expectedConsulNamespace, tc.partition),
		tc.existingUpstreams,
		nil)

	namespacedName := types.NamespacedName{
		Namespace: podNamespace,
		Name:      tc.podName,
	}

	resp, err := pc.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: namespacedName,
	})
	if tc.expErr != "" {
		require.EqualError(t, err, tc.expErr)
	} else {
		require.NoError(t, err)
	}

	require.Equal(t, tc.expRequeue, resp.Requeue)

	wID := getWorkloadID(tc.podName, tc.expectedConsulNamespace, tc.partition)
	expectedWorkloadMatches(t, resourceClient, wID, tc.expectedWorkload)

	hsID := getHealthStatusID(tc.podName, tc.expectedConsulNamespace, tc.partition)
	expectedHealthStatusMatches(t, resourceClient, hsID, tc.expectedHealthStatus)

	pcID := getProxyConfigurationID(tc.podName, tc.expectedConsulNamespace, tc.partition)
	expectedProxyConfigurationMatches(t, resourceClient, pcID, tc.expectedProxyConfiguration)

	uID := getUpstreamsID(tc.podName, metav1.NamespaceDefault, constants.DefaultConsulPartition)
	expectedUpstreamMatches(t, resourceClient, uID, tc.expectedUpstreams)
}
