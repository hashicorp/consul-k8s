package connectinject

import (
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/helper/controller"
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
	testDoNotRegister         = "do not register"

	testCheckNotesPassing  = "Kubernetes Health Checks Passing"
	testTypesBoth          = "both"
	testTypesUpsertOnly    = "upsert"
	testTypesReconcileOnly = "reconcile"
	testUpsert             = "upsert"
	testReconcile          = "reconcile"
)

func getSupportedTestTypes(testTypes string) map[string]bool {
	switch testTypes {
	case testTypesBoth:
		return map[string]bool{testUpsert: true, testReconcile: true}
	case testTypesReconcileOnly:
		return map[string]bool{testReconcile: true}
	case testTypesUpsertOnly:
		return map[string]bool{testUpsert: true}
	}
	return nil
}

var testPodSpec = corev1.PodSpec{
	Containers: []corev1.Container{
		corev1.Container{
			Name: testPodName,
		},
	},
}

func testServerAgentResourceAndController(t *testing.T, pod *corev1.Pod) (*testutil.TestServer, *api.Client, *HealthCheckResource) {
	require := require.New(t)
	// Set up server, client
	s, err := testutil.NewTestServerConfigT(t, nil)
	require.NoError(err)

	clientConfig := &api.Config{Address: s.HTTPAddr}
	require.NoError(err)
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

func registerHealthCheck(t *testing.T, client *api.Client, initialState, reason string) {
	require := require.New(t)
	err := client.Agent().CheckRegister(&api.AgentCheckRegistration{
		Name:      "Kubernetes Health Check",
		ID:        testHealthCheckID,
		ServiceID: testServiceNameReg,
		Notes:     reason,
		AgentServiceCheck: api.AgentServiceCheck{
			TTL:    "100000h",
			Status: initialState,
			Notes:  reason,
		},
	})
	require.NoError(err)
}

// We expect to already be pointed at the correct agent
func getConsulAgentChecks(t *testing.T, client *api.Client) *api.AgentCheck {
	require := require.New(t)
	filter := fmt.Sprintf("CheckID == `%s`", testHealthCheckID)
	checks, err := client.Agent().ChecksWithFilter(filter)
	require.NoError(err)
	return checks[testHealthCheckID]
}

func TestHealthCheckHandlers(t *testing.T) {
	cases := []struct {
		Name                 string
		ValidTests           map[string]bool
		PreCreateHealthCheck bool
		InitialState         string
		Pod                  *corev1.Pod
		Expected             *api.AgentCheck
		Err                  string
	}{
		{
			"PodRunning Object Created passing - create check and set passing",
			getSupportedTestTypes(testTypesBoth),
			false,
			api.HealthPassing,
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: "default",
					Labels:    map[string]string{labelInject: "true"},
					Annotations: map[string]string{
						annotationStatus:  "injected",
						annotationService: testServiceNameAnnotation,
					},
				},
				Spec: testPodSpec,
				Status: corev1.PodStatus{
					HostIP: "127.0.0.1",
					Phase:  corev1.PodRunning,
					Conditions: []corev1.PodCondition{{
						Type:   corev1.PodReady,
						Status: corev1.ConditionTrue,
					}},
				},
			},
			&api.AgentCheck{
				CheckID: testHealthCheckID,
				Status:  api.HealthPassing,
				Notes:   testCheckNotesPassing,
			},
			"",
		},
		{
			"PodRunning Object Created failed - create check and set failed",
			getSupportedTestTypes(testTypesBoth),
			false,
			api.HealthPassing,
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: "default",
					Labels:    map[string]string{labelInject: "true"},
					Annotations: map[string]string{
						annotationStatus:  "injected",
						annotationService: testServiceNameAnnotation,
					},
				},
				Spec: testPodSpec,
				Status: corev1.PodStatus{
					HostIP: "127.0.0.1",
					Phase:  corev1.PodRunning,
					Conditions: []corev1.PodCondition{{
						Type:   corev1.PodReady,
						Status: corev1.ConditionTrue,
					}},
				},
			},
			&api.AgentCheck{
				CheckID: testHealthCheckID,
				Status:  api.HealthPassing,
				Notes:   testCheckNotesPassing,
			},
			"",
		},
		{
			"PodRunning change to Failed with failure message - change to failed",
			getSupportedTestTypes(testTypesBoth),
			true,
			api.HealthPassing,
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: "default",
					Labels:    map[string]string{labelInject: "true"},
					Annotations: map[string]string{
						annotationStatus:  "injected",
						annotationService: testServiceNameAnnotation,
					},
				},
				Spec: testPodSpec,
				Status: corev1.PodStatus{
					HostIP: "127.0.0.1",
					Phase:  corev1.PodRunning,
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
			},
			"",
		},
		{
			"PodRunning failed to passing - change to passing",
			getSupportedTestTypes(testTypesBoth),
			true,
			api.HealthCritical,
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: "default",
					Labels:    map[string]string{labelInject: "true"},
					Annotations: map[string]string{
						annotationStatus:  "injected",
						annotationService: testServiceNameAnnotation,
					},
				},
				Spec: testPodSpec,
				Status: corev1.PodStatus{
					HostIP: "127.0.0.1",
					Phase:  corev1.PodRunning,
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
			},
			"",
		},
		{
			"PodRunning but with no changes - no change",
			getSupportedTestTypes(testTypesBoth),
			true,
			api.HealthCritical,
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: "default",
					Labels:    map[string]string{labelInject: "true"},
					Annotations: map[string]string{
						annotationStatus:  "injected",
						annotationService: testServiceNameAnnotation,
					},
				},
				Spec: testPodSpec,
				Status: corev1.PodStatus{
					HostIP: "127.0.0.1",
					Phase:  corev1.PodRunning,
					Conditions: []corev1.PodCondition{{
						Type:   corev1.PodReady,
						Status: corev1.ConditionFalse,
					}},
				},
			},
			&api.AgentCheck{
				CheckID: testHealthCheckID,
				Status:  api.HealthCritical,
				Notes:   testFailureMessage,
				Output:  testFailureMessage,
			},
			"",
		},
		{
			"PodNotRunning - no changes",
			getSupportedTestTypes(testTypesBoth),
			false,
			"",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: "default",
					Labels:    map[string]string{labelInject: "true"},
					Annotations: map[string]string{
						annotationStatus:  "injected",
						annotationService: testServiceNameAnnotation,
					},
				},
				Spec: testPodSpec,
				Status: corev1.PodStatus{
					HostIP: "127.0.0.1",
					Phase:  corev1.PodPending,
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
			"PodRunning no annotations - no change",
			getSupportedTestTypes(testTypesBoth),
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
					HostIP: "127.0.0.1",
					Phase:  corev1.PodRunning,
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
			"PodRunning service not registered causes error",
			getSupportedTestTypes(testTypesUpsertOnly),
			false,
			testDoNotRegister,
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: "default",
					Labels:    map[string]string{labelInject: "true"},
					Annotations: map[string]string{
						annotationStatus:  "injected",
						annotationService: testServiceNameAnnotation,
					},
				},
				Spec: testPodSpec,
				Status: corev1.PodStatus{
					HostIP: "127.0.0.1",
					Phase:  corev1.PodRunning,
					Conditions: []corev1.PodCondition{{
						Type:   corev1.PodReady,
						Status: corev1.ConditionTrue,
					}},
				},
			},
			nil,
			"ServiceID \"test-pod-test-service\" does not exist",
		},
		{
			"PodRunning no label - no change",
			getSupportedTestTypes(testTypesReconcileOnly),
			false,
			"",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: "default",
					Annotations: map[string]string{
						annotationStatus:  "injected",
						annotationService: testServiceNameAnnotation,
					},
				},
				Spec: testPodSpec,
				Status: corev1.PodStatus{
					HostIP: "127.0.0.1",
					Phase:  corev1.PodRunning,
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
			"PodRunning no Ready Status - no change",
			getSupportedTestTypes(testTypesBoth),
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
					HostIP: "127.0.0.1",
					Phase:  corev1.PodRunning,
				},
			},
			nil,
			"",
		},
	}
	for _, work := range []string{testReconcile, testUpsert} {
		for _, tt := range cases {
			if _, ok := tt.ValidTests[work]; !ok {
				continue
			}
			//if tt.Name != "PodRunning change to Failed with failure message - change to failed" {
			//	continue
			//}
			t.Run(work+" "+tt.Name, func(t *testing.T) {
				var err error

				require := require.New(t)
				// Get a server, client, and handler
				server, client, resource := testServerAgentResourceAndController(t, tt.Pod)
				defer server.Stop()

				if tt.InitialState != testDoNotRegister {
					// Create a passing service
					server.AddService(t, testServiceNameReg, api.HealthPassing, nil)
				}
				if tt.PreCreateHealthCheck {
					// register the health check if this is not an object create path
					if tt.InitialState == api.HealthPassing {
						registerHealthCheck(t, client, tt.InitialState, testCheckNotesPassing)
					} else {
						registerHealthCheck(t, client, tt.InitialState, testFailureMessage)
					}
				}
				if work == testUpsert {
					err = resource.Upsert("", tt.Pod)
				} else if work == testReconcile {
					err = resource.Reconcile()
				}
				if tt.Err != "" {
					// used in the cases where we're expecting an error from
					// the controller/handler, in which case do not check agent
					// checks as they're relevant/created.
					require.Error(err, tt.Err)
					return
				}
				require.NoError(err)
				actual := getConsulAgentChecks(t, client)
				if tt.Expected == nil || actual == nil {
					require.Equal(tt.Expected, actual)
				} else {
					if actual.Status != tt.InitialState {
						require.Equal(tt.Expected.Output, actual.Output)
					} else {
						// no update called
						require.Equal(tt.Expected.Notes, actual.Notes)
					}
					require.Equal("Kubernetes Health Check", actual.Name)
					require.Equal(tt.Expected.CheckID, actual.CheckID)
					require.Equal(tt.Expected.Status, actual.Status)
					require.Equal("ttl", actual.Type)
				}
			})
		}
	}
}

// Test that if the agent is unavailable reconcile will fail on the pod
// and once the agent becomes available reconcile will correctly
// update the checks after its loop timer passes
func TestReconcileRun(t *testing.T) {
	var err error
	require := require.New(t)

	// Start the clientset with a Pod that is failed
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testPodName,
			Namespace: "default",
			Labels:    map[string]string{labelInject: "true"},
			Annotations: map[string]string{
				annotationStatus:  "injected",
				annotationService: testServiceNameAnnotation,
			},
		},
		Spec: testPodSpec,
		Status: corev1.PodStatus{
			HostIP: "127.0.0.1",
			Phase:  corev1.PodRunning,
			Conditions: []corev1.PodCondition{{
				Type:   corev1.PodReady,
				Status: corev1.ConditionFalse,
			}},
		},
	}
	k8sclientset := fake.NewSimpleClientset(pod)
	randomPorts := freeport.MustTake(6)

	schema := "http://"
	serverAddress := fmt.Sprintf("%s%s:%d", schema, "127.0.0.1", randomPorts[1])

	// setup consul client connection
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
		ReconcilePeriod:     5 * time.Second,
	}
	ctl := &controller.Controller{
		Log:      hclog.Default().Named("healthCheckController"),
		Resource: &healthResource,
	}
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	// start the controller
	go func() {
		ctl.Run(ctx.Done())
	}()
	time.Sleep(time.Second * 1)

	testServerReady := make(chan bool)
	var srv *testutil.TestServer
	go func() {
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
		close(testServerReady)
	}()
	// Wait for server to come up
	select {
	case <-testServerReady:
		defer srv.Stop()
	}
	// validate that there is no health check created by reconciler
	check := getConsulAgentChecks(t, client)
	require.Nil(check)
	// Add the service
	// TODO: can this be done inside the cfg func when creating the server instance?
	srv.AddService(t, testServiceNameReg, api.HealthPassing, nil)

	timer := &retry.Timer{Timeout: 15 * time.Second, Wait: 5 * time.Second}
	var actual *api.AgentCheck
	retry.RunWith(timer, t, func(r *retry.R) {
		actual = getConsulAgentChecks(t, client)
		if actual == nil {
			r.Error("check = nil")
		}
	})

	expectedCheck := &api.AgentCheck{
		CheckID: testHealthCheckID,
		Status:  api.HealthCritical,
		Notes:   testFailureMessage,
		Output:  testFailureMessage,
	}

	require.Equal("Kubernetes Health Check", actual.Name)
	require.Equal(expectedCheck.CheckID, actual.CheckID)
	require.Equal(expectedCheck.Status, actual.Status)
	require.Equal("ttl", actual.Type)
}
