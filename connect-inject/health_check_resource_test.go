package connectinject

import (
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/freeport"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

const (
	testPodName               = "test-pod"
	testServiceNameAnnotation = "test-service"
	testServiceNameReg        = "test-pod-test-service"
	testHealthCheckID         = "default_test-pod-test-service_kubernetes-health-check-ttl"
	testFailureMessage        = "Kubernetes pod readiness probe failed"
	testCheckNotesPassing     = "Kubernetes Health Checks Passing"
	ttl                       = "ttl"
	name                      = "Kubernetes Health Check"
)

// Used by gocmp.
var ignoredFields = []string{"Node", "Namespace", "Definition", "ServiceID", "ServiceName"}

var testPodSpec = corev1.PodSpec{
	Containers: []corev1.Container{
		corev1.Container{
			Name: testPodName,
		},
	},
}

var completedInjectInitContainer = []corev1.ContainerStatus{
	{
		Name: InjectInitContainerName,
		State: corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{
				Reason: "Completed",
			},
		},
		Ready: true,
	},
}

func registerHealthCheck(t *testing.T, client *api.Client, initialState string) {
	require := require.New(t)
	err := client.Agent().CheckRegister(&api.AgentCheckRegistration{
		Name:      "Kubernetes Health Check",
		ID:        testHealthCheckID,
		ServiceID: testServiceNameReg,
		Notes:     "",
		AgentServiceCheck: api.AgentServiceCheck{
			TTL:    "100000h",
			Status: initialState,
			Notes:  "",
		},
	})
	require.NoError(err)
}

// We expect to already be pointed at the correct agent.
func getConsulAgentChecks(t *testing.T, client *api.Client, healthCheckID string) *api.AgentCheck {
	require := require.New(t)
	filter := fmt.Sprintf("CheckID == `%s`", healthCheckID)
	checks, err := client.Agent().ChecksWithFilter(filter)
	require.NoError(err)
	return checks[healthCheckID]
}

func TestReconcilePod(t *testing.T) {
	t.Parallel()
	cases := []struct {
		Name                 string
		PreCreateHealthCheck bool
		InitialState         string
		Pod                  *corev1.Pod
		Expected             *api.AgentCheck
		Err                  string
	}{
		{
			"inject init container has completed but containers not yet running",
			false,
			api.HealthPassing,
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: "default",
					Labels:    map[string]string{labelInject: "true"},
					Annotations: map[string]string{
						annotationStatus:  injected,
						annotationService: testServiceNameAnnotation,
					},
				},
				Spec: testPodSpec,
				Status: corev1.PodStatus{
					Phase:                 corev1.PodPending,
					InitContainerStatuses: completedInjectInitContainer,
					Conditions: []corev1.PodCondition{{
						Type:   corev1.PodReady,
						Status: corev1.ConditionFalse,
					}},
				},
			},
			&api.AgentCheck{
				CheckID: testHealthCheckID,
				Status:  api.HealthCritical,
				Notes:   "",
				Output:  "Pod is pending",
				Type:    ttl,
				Name:    name,
			},
			"",
		},
		{
			"reconcilePod will create check and set passed",
			false,
			api.HealthPassing,
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: "default",
					Labels:    map[string]string{labelInject: "true"},
					Annotations: map[string]string{
						annotationStatus:  injected,
						annotationService: testServiceNameAnnotation,
					},
				},
				Spec: testPodSpec,
				Status: corev1.PodStatus{
					HostIP:                "127.0.0.1",
					Phase:                 corev1.PodRunning,
					InitContainerStatuses: completedInjectInitContainer,
					Conditions: []corev1.PodCondition{{
						Type:   corev1.PodReady,
						Status: corev1.ConditionTrue,
					}},
				},
			},
			&api.AgentCheck{
				CheckID: testHealthCheckID,
				Status:  api.HealthPassing,
				Notes:   "",
				Output:  kubernetesSuccessReasonMsg,
				Type:    ttl,
				Name:    name,
			},
			"",
		},
		{
			"reconcilePod will create check and set failed",
			false,
			api.HealthPassing,
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: "default",
					Labels:    map[string]string{labelInject: "true"},
					Annotations: map[string]string{
						annotationStatus:  injected,
						annotationService: testServiceNameAnnotation,
					},
				},
				Spec: testPodSpec,
				Status: corev1.PodStatus{
					HostIP:                "127.0.0.1",
					Phase:                 corev1.PodRunning,
					InitContainerStatuses: completedInjectInitContainer,
					Conditions: []corev1.PodCondition{{
						Type:    corev1.PodReady,
						Status:  corev1.ConditionFalse,
						Message: testFailureMessage,
					}},
				},
			},
			&api.AgentCheck{
				CheckID: testHealthCheckID,
				Status:  api.HealthCritical,
				Notes:   "",
				Output:  testFailureMessage,
				Type:    ttl,
				Name:    name,
			},
			"",
		},
		{
			"precreate a passing pod and change to failed",
			true,
			api.HealthPassing,
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: "default",
					Labels:    map[string]string{labelInject: "true"},
					Annotations: map[string]string{
						annotationStatus:  injected,
						annotationService: testServiceNameAnnotation,
					},
				},
				Spec: testPodSpec,
				Status: corev1.PodStatus{
					HostIP:                "127.0.0.1",
					Phase:                 corev1.PodRunning,
					InitContainerStatuses: completedInjectInitContainer,
					Conditions: []corev1.PodCondition{{
						Type:    corev1.PodReady,
						Status:  corev1.ConditionFalse,
						Message: testFailureMessage,
					}},
				},
			},
			&api.AgentCheck{
				CheckID: testHealthCheckID,
				Status:  api.HealthCritical,
				Output:  testFailureMessage,
				Type:    ttl,
				Name:    name,
			},
			"",
		},
		{
			"precreate failed pod and change to passing",
			true,
			api.HealthCritical,
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: "default",
					Labels:    map[string]string{labelInject: "true"},
					Annotations: map[string]string{
						annotationStatus:  injected,
						annotationService: testServiceNameAnnotation,
					},
				},
				Spec: testPodSpec,
				Status: corev1.PodStatus{
					HostIP:                "127.0.0.1",
					Phase:                 corev1.PodRunning,
					InitContainerStatuses: completedInjectInitContainer,
					Conditions: []corev1.PodCondition{{
						Type:   corev1.PodReady,
						Status: corev1.ConditionTrue,
					}},
				},
			},
			&api.AgentCheck{
				CheckID: testHealthCheckID,
				Status:  api.HealthPassing,
				Output:  testCheckNotesPassing,
				Type:    ttl,
				Name:    name,
			},
			"",
		},
		{
			"precreate failed check, no pod changes results in no healthcheck changes",
			true,
			api.HealthCritical,
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: "default",
					Labels:    map[string]string{labelInject: "true"},
					Annotations: map[string]string{
						annotationStatus:  injected,
						annotationService: testServiceNameAnnotation,
					},
				},
				Spec: testPodSpec,
				Status: corev1.PodStatus{
					HostIP:                "127.0.0.1",
					Phase:                 corev1.PodRunning,
					InitContainerStatuses: completedInjectInitContainer,
					Conditions: []corev1.PodCondition{{
						Type:   corev1.PodReady,
						Status: corev1.ConditionFalse,
					}},
				},
			},
			&api.AgentCheck{
				CheckID: testHealthCheckID,
				Status:  api.HealthCritical,
				Output:  "", // when there is no change in status, Consul doesnt set the Output field
				Type:    ttl,
				Name:    name,
			},
			"",
		},
		{
			"PodRunning no annotations will be ignored for processing",
			false,
			"",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: "default",
					Labels:    map[string]string{labelInject: "true"},
				},
				Spec: testPodSpec,
				Status: corev1.PodStatus{
					HostIP:                "127.0.0.1",
					Phase:                 corev1.PodRunning,
					InitContainerStatuses: completedInjectInitContainer,
					Conditions: []corev1.PodCondition{{
						Type:   corev1.PodReady,
						Status: corev1.ConditionTrue,
					}},
				},
			},
			nil,
			"",
		},
		{
			"PodRunning no Ready Status will be ignored for processing",
			false,
			"",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: "default",
					Labels:    map[string]string{labelInject: "true"},
				},
				Spec: testPodSpec,
				Status: corev1.PodStatus{
					HostIP:                "127.0.0.1",
					Phase:                 corev1.PodRunning,
					InitContainerStatuses: completedInjectInitContainer,
				},
			},
			nil,
			"",
		},
	}
	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			var err error
			require := require.New(t)
			// Get a server, client, and handler.
			server, client, resource := testServerAgentResourceAndController(t, tt.Pod)
			defer server.Stop()
			// Register the service with Consul.
			server.AddService(t, testServiceNameReg, api.HealthPassing, nil)
			if tt.PreCreateHealthCheck {
				// Register the health check if this is not an object create path.
				registerHealthCheck(t, client, tt.InitialState)
			}
			// Upsert and Reconcile both use reconcilePod to reconcile a pod.
			err = resource.reconcilePod(tt.Pod)
			// If we're expecting any error from reconcilePod.
			if tt.Err != "" {
				// used in the cases where we're expecting an error from
				// the controller/handler, in which case do not check agent
				// checks as they're not relevant/created.
				require.Error(err, tt.Err)
				return
			}
			require.NoError(err)
			// Get the agent checks if they were registered.
			actual := getConsulAgentChecks(t, client, testHealthCheckID)

			cmpOpts := cmpopts.IgnoreFields(api.AgentCheck{}, ignoredFields...)
			require.True(cmp.Equal(actual, tt.Expected, cmpOpts),
				cmp.Diff(actual, tt.Expected, cmpOpts))
			require.True(cmp.Equal(actual, tt.Expected, cmpopts.IgnoreFields(api.AgentCheck{}, ignoredFields...)))
		})
	}
}

func TestUpsert_PodWithNoServiceReturnsError(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testPodName,
			Namespace: "default",
			Labels:    map[string]string{labelInject: "true"},
			Annotations: map[string]string{
				annotationStatus:  injected,
				annotationService: testServiceNameAnnotation,
			},
		},
		Spec: testPodSpec,
		Status: corev1.PodStatus{
			HostIP:                "127.0.0.1",
			Phase:                 corev1.PodRunning,
			InitContainerStatuses: completedInjectInitContainer,
			Conditions: []corev1.PodCondition{{
				Type:   corev1.PodReady,
				Status: corev1.ConditionTrue,
			}},
		},
	}
	server, _, resource := testServerAgentResourceAndController(t, pod)
	defer server.Stop()
	// Start Upsert, it will attempt to reconcile the Pod but the service doesnt exist in Consul so will fail.
	err := resource.Upsert("", pod)
	require.Contains(err.Error(), "test-pod-test-service\" does not exist)")
}

func TestReconcile_IgnorePodsWithoutInjectLabel(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testPodName,
			Namespace: "default",
			Annotations: map[string]string{
				annotationStatus:  injected,
				annotationService: testServiceNameAnnotation,
			},
		},
		Spec: testPodSpec,
		Status: corev1.PodStatus{
			HostIP:                "127.0.0.1",
			Phase:                 corev1.PodRunning,
			InitContainerStatuses: completedInjectInitContainer,
			Conditions: []corev1.PodCondition{{
				Type:   corev1.PodReady,
				Status: corev1.ConditionTrue,
			}},
		},
	}
	server, client, resource := testServerAgentResourceAndController(t, pod)
	defer server.Stop()
	// Start the reconciler, it should not create a health check.
	err := resource.Reconcile()
	require.NoError(err)
	actual := getConsulAgentChecks(t, client, testHealthCheckID)
	require.Nil(actual)
}

// Test pod statuses that the reconciler should ignore.
// These test cases are based on actual observed startup and termination phases.
func TestReconcile_IgnoreStatuses(t *testing.T) {
	t.Parallel()
	cases := map[string]corev1.PodStatus{
		"not scheduled": {
			Phase: corev1.PodPending,
		},
		"scheduled and pending": {
			Phase: corev1.PodPending,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodScheduled,
					Status: corev1.ConditionTrue,
				},
			},
		},
		"inject init container initializing": {
			Phase: corev1.PodPending,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodScheduled,
					Status: corev1.ConditionTrue,
				},
				{
					Type:   corev1.PodInitialized,
					Status: corev1.ConditionFalse,
				},
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionFalse,
				},
				{
					Type:   corev1.ContainersReady,
					Status: corev1.ConditionFalse,
				},
			},
			InitContainerStatuses: []corev1.ContainerStatus{
				{
					Name: InjectInitContainerName,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason: "Initializing",
						},
					},
					Ready: false,
				},
			},
			ContainerStatuses: unreadyAppContainers,
		},
		"inject init container running (but not terminated)": {
			Phase: corev1.PodPending,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodScheduled,
					Status: corev1.ConditionTrue,
				},
				{
					Type:   corev1.PodInitialized,
					Status: corev1.ConditionFalse,
				},
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionFalse,
				},
				{
					Type:   corev1.ContainersReady,
					Status: corev1.ConditionFalse,
				},
			},
			InitContainerStatuses: []corev1.ContainerStatus{
				{
					Name: InjectInitContainerName,
					State: corev1.ContainerState{
						Waiting: nil,
						Running: &corev1.ContainerStateRunning{StartedAt: metav1.Now()},
					},
					Ready: false,
				},
			},
			ContainerStatuses: unreadyAppContainers,
		},
		"pod is terminating": {
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodScheduled,
					Status: corev1.ConditionTrue,
				},
				{
					Type:   corev1.PodInitialized,
					Status: corev1.ConditionTrue,
				},
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionFalse,
				},
				{
					Type:   corev1.ContainersReady,
					Status: corev1.ConditionFalse,
				},
			},
			InitContainerStatuses: []corev1.ContainerStatus{
				{
					Name: InjectInitContainerName,
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{StartedAt: metav1.Now()},
					},
					Ready: true,
				},
			},
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "consul-connect-envoy-sidecar",
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							ExitCode:   0,
							Reason:     "Completed",
							StartedAt:  metav1.Time{},
							FinishedAt: metav1.Time{},
						},
					},
					Ready: false,
				},
				{
					Name: "consul-connect-lifecycle-sidecar",
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							ExitCode:   2,
							Reason:     "Error",
							StartedAt:  metav1.Time{},
							FinishedAt: metav1.Time{},
						},
					},
					Ready: false,
				},
				{
					Name: "app",
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							ExitCode:   137,
							Reason:     "Error",
							StartedAt:  metav1.Time{},
							FinishedAt: metav1.Time{},
						},
					},
					Ready: false,
				},
			},
		},
	}
	for name, podStatus := range cases {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: "default",
					Annotations: map[string]string{
						annotationStatus:  injected,
						annotationService: testServiceNameAnnotation,
					},
				},
				Spec:   testPodSpec,
				Status: podStatus,
			}
			server, _, resource := testServerAgentResourceAndController(t, pod)
			defer server.Stop()

			// We would expect an error if the reconciler actually tried to
			// register a health check because the underlying service hasn't
			// been created.
			require.NoError(resource.reconcilePod(pod))
		})
	}
}

// Test that stopch works for Reconciler.
func TestReconcilerShutdown(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	k8sclientset := fake.NewSimpleClientset()
	healthResource := HealthCheckResource{
		Log:                 hclog.Default().Named("healthCheckResource"),
		KubernetesClientset: k8sclientset,
		ConsulUrl:           nil,
		ReconcilePeriod:     5 * time.Second,
	}

	reconcilerRunningCtx := make(chan struct{})
	reconcilerShutdownSuccess := make(chan bool)
	go func() {
		// Starting the reconciler.
		healthResource.Run(reconcilerRunningCtx)
		close(reconcilerShutdownSuccess)
	}()
	// Trigger shutdown of the reconciler.
	close(reconcilerRunningCtx)

	select {
	case <-reconcilerShutdownSuccess:
		// The function is expected to exit gracefully so no assertion needed.
		return
	case <-time.After(time.Second * 1):
		// Fail if the stopCh was not caught.
		require.Fail("timeout waiting for reconciler to shutdown")
	}
}

// Test that if the agent is unavailable reconcile will fail on the pod
// and once the agent becomes available reconcile will correctly
// update the checks after its loop timer passes.
func TestReconcileRun(t *testing.T) {
	t.Parallel()
	var err error
	require := require.New(t)

	// Start the clientset with a Pod that is failed.
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testPodName,
			Namespace: "default",
			Labels:    map[string]string{labelInject: "true"},
			Annotations: map[string]string{
				annotationStatus:  injected,
				annotationService: testServiceNameAnnotation,
			},
		},
		Spec: testPodSpec,
		Status: corev1.PodStatus{
			HostIP:                "127.0.0.1",
			Phase:                 corev1.PodRunning,
			InitContainerStatuses: completedInjectInitContainer,
			Conditions: []corev1.PodCondition{{
				Type:    corev1.PodReady,
				Status:  corev1.ConditionFalse,
				Message: testFailureMessage,
			}},
		},
	}
	k8sclientset := fake.NewSimpleClientset(pod)
	randomPorts := freeport.MustTake(6)
	schema := "http://"
	serverAddress := fmt.Sprintf("%s%s:%d", schema, "127.0.0.1", randomPorts[1])

	// Setup consul client connection.
	clientConfig := &api.Config{Address: serverAddress}
	require.NoError(err)
	client, err := api.NewClient(clientConfig)
	require.NoError(err)
	consulUrl, err := url.Parse(serverAddress)
	require.NoError(err)

	healthResource := HealthCheckResource{
		Log:                 hclog.Default().Named("healthCheckResource"),
		KubernetesClientset: k8sclientset,
		ConsulUrl:           consulUrl,
		ReconcilePeriod:     100 * time.Millisecond,
	}
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	// Start the reconciler.
	go func() {
		healthResource.Run(ctx.Done())
	}()
	// Let reconcile run at least once.
	time.Sleep(time.Millisecond * 300)

	var srv *testutil.TestServer
	srv, err = testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
		c.Ports = &testutil.TestPortConfig{
			DNS:     randomPorts[0],
			HTTP:    randomPorts[1],
			HTTPS:   randomPorts[2],
			SerfLan: randomPorts[3],
			SerfWan: randomPorts[4],
			Server:  randomPorts[5],
		}
	})
	require.NoError(err)
	// Validate that there is no health check created by reconciler.
	check := getConsulAgentChecks(t, client, testHealthCheckID)
	require.Nil(check)
	// Add the service - only now will a health check have a service to register against.
	srv.AddService(t, testServiceNameReg, api.HealthPassing, nil)

	// Retry so we can cover time period when reconciler is already running vs
	// when it will run next based on the loop.
	timer := &retry.Timer{Timeout: 5 * time.Second, Wait: 1 * time.Second}
	var actual *api.AgentCheck
	retry.RunWith(timer, t, func(r *retry.R) {
		actual = getConsulAgentChecks(t, client, testHealthCheckID)
		// The assertion is not on actual != nil, but below
		// against an expected check.
		if actual == nil || actual.Output == "" {
			r.Error("check = nil")
		}
	})

	expectedCheck := &api.AgentCheck{
		CheckID: testHealthCheckID,
		Status:  api.HealthCritical,
		Output:  testFailureMessage,
		Type:    ttl,
		Name:    name,
	}
	// Validate the checks are set.
	require.True(cmp.Equal(actual, expectedCheck, cmpopts.IgnoreFields(api.AgentCheck{}, ignoredFields...)))
}

func testServerAgentResourceAndController(t *testing.T, pod *corev1.Pod) (*testutil.TestServer, *api.Client, *HealthCheckResource) {
	return testServerAgentResourceAndControllerWithConsulNS(t, pod, "")
}

func testServerAgentResourceAndControllerWithConsulNS(t *testing.T, pod *corev1.Pod, consulNS string) (*testutil.TestServer, *api.Client, *HealthCheckResource) {
	require := require.New(t)
	// Setup server & client.
	s, err := testutil.NewTestServerConfigT(t, nil)
	require.NoError(err)

	clientConfig := &api.Config{Address: s.HTTPAddr, Namespace: consulNS}
	client, err := api.NewClient(clientConfig)
	require.NoError(err)

	schema := "http://"
	consulUrl, err := url.Parse(schema + s.HTTPAddr)
	require.NoError(err)

	healthResource := HealthCheckResource{
		Log:                 hclog.Default().Named("healthCheckResource"),
		KubernetesClientset: fake.NewSimpleClientset(pod),
		ConsulUrl:           consulUrl,
		ReconcilePeriod:     0,
	}
	return s, client, &healthResource
}

// unreadyAppContainers are the container statuses of an example connect pod's
// non-init containers when init containers are still running.
var unreadyAppContainers = []corev1.ContainerStatus{
	{
		Name: "consul-connect-envoy-sidecar",
		State: corev1.ContainerState{
			Waiting: &corev1.ContainerStateWaiting{
				Reason: "PodInitializing",
			},
		},
		Ready: false,
	},
	{
		Name: "consul-connect-lifecycle-sidecar",
		State: corev1.ContainerState{
			Waiting: &corev1.ContainerStateWaiting{
				Reason: "PodInitializing",
			},
		},
		Ready: false,
	},
	{
		Name: "app",
		State: corev1.ContainerState{
			Waiting: &corev1.ContainerStateWaiting{
				Reason: "PodInitializing",
			},
		},
		Ready: false,
	},
}
