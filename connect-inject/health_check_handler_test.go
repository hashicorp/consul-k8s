package connectinject

import (
	"strings"
	"testing"

	"github.com/hashicorp/consul-k8s/subcommand/flags"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

const (
	testServiceNameAnnotation = "test-service"
	testServiceNameReg        = "test-pod-test-service"
	testHealthCheckID         = "default_test-pod-test-service_kubernetes-health-check-ttl"
)

func testServerAgentAndHandler(t *testing.T, pod *corev1.Pod) (*testutil.TestServer, *api.Client, *HealthCheckHandler, error) {
	require := require.New(t)
	// Set up server, client
	s, err := testutil.NewTestServerConfigT(t, nil)
	require.NoError(err)

	client, err := api.NewClient(&api.Config{
		Address: s.HTTPAddr,
	})
	require.NoError(err)
	hflags := flags.HTTPFlags{}
	hflags.SetAddress(s.HTTPAddr)
	hc := HealthCheckHandler{
		Log:        hclog.Default(),
		AclConfig:  api.NamespaceACLConfig{},
		Client:     client,
		HFlags:     &hflags,
		Clientset:  fake.NewSimpleClientset(pod),
		ConsulPort: strings.Split(s.HTTPAddr, ":")[1],
	}
	return s, client, &hc, nil
}

func registerHealthCheck(t *testing.T, name string, server *testutil.TestServer, client *api.Client, h *HealthCheckHandler, initialState string) error {
	require := require.New(t)
	h.Log.Error("Registering a new check from test: ", testHealthCheckID, testServiceNameReg)
	err := client.Agent().CheckRegister(&api.AgentCheckRegistration{
		Name:      testHealthCheckID,
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
func testGetConsulAgentChecks(t *testing.T, handler *HealthCheckHandler, client *api.Client, pod *corev1.Pod) *api.AgentCheck {
	require := require.New(t)
	filter := "Name == `" + testHealthCheckID + "`"
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
						annotationInject:  "true",
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
						Type:   "Ready",
						Status: corev1.ConditionTrue,
					}},
				},
			},
			&api.AgentCheck{
				CheckID: testHealthCheckID,
				Status:  healthCheckPassing,
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
						annotationInject:  "true",
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
						Type:   "Ready",
						Status: corev1.ConditionFalse,
					}},
				},
			},
			&api.AgentCheck{
				CheckID: testHealthCheckID,
				Status:  healthCheckCritical,
			},
			"",
		},
		{
			"Reconcile existing object from passing to failing",
			true,
			healthCheckPassing,
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: "default",
					Labels:    map[string]string{labelInject: "true"},
					Annotations: map[string]string{
						annotationInject:  "true",
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
						Type:   "Ready",
						Status: corev1.ConditionFalse,
					}},
				},
			},
			&api.AgentCheck{
				CheckID: testHealthCheckID,
				Status:  healthCheckCritical,
			},
			"",
		},
		{
			"Reconcile existing object from failing to passing",
			true,
			healthCheckCritical,
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: "default",
					Labels:    map[string]string{labelInject: "true"},
					Annotations: map[string]string{
						annotationInject:  "true",
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
						Type:   "Ready",
						Status: corev1.ConditionTrue,
					}},
				},
			},
			&api.AgentCheck{
				CheckID: testHealthCheckID,
				Status:  healthCheckPassing,
			},
			"",
		},
		{
			"Reconcile pod not running no update",
			false,
			healthCheckCritical,
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: "default",
					Labels:    map[string]string{labelInject: "true"},
					Annotations: map[string]string{
						annotationInject:  "true",
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
						Type:   "Ready",
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

			server, client, handler, err := testServerAgentAndHandler(t, tt.Pod)
			require.NoError(err)
			defer server.Stop()

			// Create a passing service
			server.AddService(t, testServiceNameReg, "passing", nil)
			if tt.CreateHealthCheck {
				// register the health check if ObjectCreate didnt run
				err = registerHealthCheck(t, testHealthCheckID, server, client, handler, tt.InitialState)
				require.NoError(err)
			}
			err = handler.Reconcile()
			require.NoError(err)
			actual := testGetConsulAgentChecks(t, handler, client, tt.Pod)
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
			healthCheckPassing,
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: "default",
					Labels:    map[string]string{labelInject: "true"},
					Annotations: map[string]string{
						annotationInject:  "true",
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
					HostIP:     "127.0.0.1",
					Phase:      corev1.PodPending,
					Conditions: nil,
				},
			},
			&api.AgentCheck{
				CheckID: testHealthCheckID,
				Status:  healthCheckPassing,
			},
			"",
		},
		{
			"PodRunning Object Update to Failed",
			true,
			healthCheckPassing,
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: "default",
					Labels:    map[string]string{labelInject: "true"},
					Annotations: map[string]string{
						annotationInject:  "true",
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
						Type:   "Ready",
						Status: corev1.ConditionFalse,
					}},
				},
			},
			&api.AgentCheck{
				CheckID: testHealthCheckID,
				Status:  healthCheckCritical,
			},
			"",
		},
		{
			"PodRunning Object Update to Passing",
			true,
			healthCheckCritical,
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: "default",
					Labels:    map[string]string{labelInject: "true"},
					Annotations: map[string]string{
						annotationInject:  "true",
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
						Type:   "Ready",
						Status: corev1.ConditionTrue,
					}},
				},
			},
			&api.AgentCheck{
				CheckID: testHealthCheckID,
				Status:  healthCheckPassing,
			},
			"",
		},
		{
			"PodRunning Object Update no changes",
			true,
			healthCheckCritical,
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: "default",
					Labels:    map[string]string{labelInject: "true"},
					Annotations: map[string]string{
						annotationInject:  "true",
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
						Type:   "Ready",
						Status: corev1.ConditionTrue,
					}},
				},
			},
			&api.AgentCheck{
				CheckID: testHealthCheckID,
				Status:  healthCheckPassing,
			},
			"",
		},
	}
	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			// Get a server, client, and handler
			server, client, handler, err := testServerAgentAndHandler(t, tt.Pod)
			require.NoError(err)
			defer server.Stop()

			// Create a passing service
			server.AddService(t, testServiceNameReg, "passing", nil)
			if tt.CreateHealthCheck {
				// register the health check if ObjectCreate didnt run
				err = registerHealthCheck(t, testHealthCheckID, server, client, handler, tt.InitialState)
				require.NoError(err)
			} else {
				handler.ObjectCreated(tt.Pod)
			}
			handler.ObjectUpdated(tt.Pod)
			actual := testGetConsulAgentChecks(t, handler, client, tt.Pod)
			if tt.Expected == nil || actual == nil {
				require.Equal(tt.Expected, actual)
			} else {
				require.Equal(tt.Expected.Status, actual.Status)
			}
		})
	}
}
