package connectinject

import (
	"fmt"
	"net/url"
	"testing"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
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
)

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

func registerHealthCheck(t *testing.T, client *api.Client, initialState string) error {
	require := require.New(t)
	err := client.Agent().CheckRegister(&api.AgentCheckRegistration{
		Name:      "K8s health check",
		ID:        testHealthCheckID,
		ServiceID: testServiceNameReg,
		AgentServiceCheck: api.AgentServiceCheck{
			TTL:    "100000h",
			Status: initialState,
		},
	})
	require.NoError(err)
	return nil
}

// We expect to already be pointed at the correct agent
func testGetConsulAgentChecks(t *testing.T, client *api.Client) *api.AgentCheck {
	require := require.New(t)
	filter := fmt.Sprintf("CheckID == `%s`", testHealthCheckID)
	checks, err := client.Agent().ChecksWithFilter(filter)
	require.NoError(err)
	return checks[testHealthCheckID]
}

func TestHealthCheckHandlerReconcile(t *testing.T) {
	cases := []struct {
		Name              string
		CreateHealthCheck bool
		InitialState      string
		Pod               *corev1.Pod
		Expected          *api.AgentCheck
		Err               string
	}{
		{
			"Reconcile new Object Create Passing",
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
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						corev1.Container{
							Name: testPodName,
						},
					},
				},
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
			},
			"",
		},
		{
			"Reconcile new Object Create Critical",
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
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						corev1.Container{
							Name: testPodName,
						},
					},
				},
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
			},
			"",
		},
		{
			"Reconcile existing object from passing to failing",
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
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						corev1.Container{
							Name: testPodName,
						},
					},
				},
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
			},
			"",
		},
		{
			"Reconcile existing object from failing to passing",
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
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						corev1.Container{
							Name: testPodName,
						},
					},
				},
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
			},
			"",
		},
		{
			"Reconcile pod not running no update",
			false,
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
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						corev1.Container{
							Name: testPodName,
						},
					},
				},
				Status: corev1.PodStatus{
					HostIP: "127.0.0.1",
					Phase:  corev1.PodFailed,
					Conditions: []corev1.PodCondition{{
						Type:   corev1.PodReady,
						Status: corev1.ConditionFalse,
					}},
				},
			},
			nil,
			"",
		},
	}
	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)

			server, client, resource := testServerAgentResourceAndController(t, tt.Pod)
			defer server.Stop()

			// Create a passing service
			server.AddService(t, testServiceNameReg, "passing", nil)
			if tt.CreateHealthCheck {
				// register the health check if ObjectCreate didnt run
				err := registerHealthCheck(t, client, tt.InitialState)
				require.NoError(err)
			}

			err := resource.Reconcile()
			require.NoError(err)
			actual := testGetConsulAgentChecks(t, client)
			if tt.Expected == nil || actual == nil {
				require.Equal(tt.Expected, actual)
			} else {
				require.Equal(tt.Expected.Status, actual.Status)
			}
		})
	}
}

func TestHealthCheckHandlerStandard(t *testing.T) {
	cases := []struct {
		Name              string
		CreateHealthCheck bool
		InitialState      string
		Pod               *corev1.Pod
		Expected          *api.AgentCheck
		Err               string
	}{
		{
			"PodRunning Object Create",
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
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						corev1.Container{
							Name: testPodName,
						},
					},
				},
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
			},
			"",
		},
		{
			"PodRunning Upsert to Failed",
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
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						corev1.Container{
							Name: testPodName,
						},
					},
				},
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
			},
			"",
		},
		{
			"PodRunning Upsert to Passing",
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
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						corev1.Container{
							Name: testPodName,
						},
					},
				},
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
			},
			"",
		},
		{
			"PodRunning Upsert no changes",
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
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						corev1.Container{
							Name: testPodName,
						},
					},
				},
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
			},
			"",
		},
		{
			"PodNotRunning no changes",
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
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						corev1.Container{
							Name: testPodName,
						},
					},
				},
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
			"PodRunning no annotations",
			false,
			"",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: "default",
					Labels:    map[string]string{labelInject: "true"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						corev1.Container{
							Name: testPodName,
						},
					},
				},
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
			"PodRunning no Ready Status",
			false,
			"",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: "default",
					Labels:    map[string]string{labelInject: "true"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						corev1.Container{
							Name: testPodName,
						},
					},
				},
				Status: corev1.PodStatus{
					HostIP: "127.0.0.1",
					Phase:  corev1.PodRunning,
				},
			},
			nil,
			"",
		},
	}
	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			// Get a server, client, and handler
			server, client, resource := testServerAgentResourceAndController(t, tt.Pod)
			defer server.Stop()

			server.AddService(t, testServiceNameReg, "passing", nil)
			// Create a passing service
			if tt.CreateHealthCheck {
				// register the health check if ObjectCreate didnt run
				err := registerHealthCheck(t, client, tt.InitialState)
				require.NoError(err)
			}
			err := resource.Upsert("", tt.Pod)
			require.NoError(err)
			actual := testGetConsulAgentChecks(t, client)
			if tt.Expected == nil || actual == nil {
				require.Equal(tt.Expected, actual)
			} else {
				require.Equal(tt.Expected.Status, actual.Status)
			}
		})
	}
}
