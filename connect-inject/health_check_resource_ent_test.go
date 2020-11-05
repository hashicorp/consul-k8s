// +build enterprise

package connectinject

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	testNamespace               = "testnamespace"
	testNamespacedHealthCheckID = "testnamespace_test-pod-test-service_kubernetes-health-check-ttl"
)

var ignoredFieldsEnterprise = []string{"Node", "Definition", "ServiceID", "ServiceName"}

var testPodWithNamespace = corev1.Pod{
	ObjectMeta: metav1.ObjectMeta{
		Namespace: testNamespace,
		Name:      testPodName,
	},
	Spec: corev1.PodSpec{},
}

// Test that when consulNamespaces are enabled, the health check is registered in the right namespace.
func TestReconcilePodWithNamespace(t *testing.T) {
	t.Parallel()
	cases := []struct {
		Name                 string
		PreCreateHealthCheck bool
		InitialState         string
		Pod                  *corev1.Pod
		Expected             *api.AgentCheck
	}{
		{
			Name:                 "reconcilePod will create check and set passed",
			PreCreateHealthCheck: false,
			InitialState:         "", // only used when precreating a health check
			Pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: testNamespace,
					Labels:    map[string]string{labelInject: "true"},
					Annotations: map[string]string{
						annotationStatus:          injected,
						annotationService:         testServiceNameAnnotation,
						annotationConsulNamespace: testNamespace,
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
			Expected: &api.AgentCheck{
				CheckID:   testNamespacedHealthCheckID,
				Status:    api.HealthPassing,
				Notes:     "",
				Output:    kubernetesSuccessReasonMsg,
				Type:      ttl,
				Name:      name,
				Namespace: testNamespace,
			},
		},
		{
			Name:                 "reconcilePod will create check and set failed",
			PreCreateHealthCheck: false,
			InitialState:         "", // only used when precreating a health check
			Pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: testNamespace,
					Labels:    map[string]string{labelInject: "true"},
					Annotations: map[string]string{
						annotationStatus:          injected,
						annotationService:         testServiceNameAnnotation,
						annotationConsulNamespace: testNamespace,
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
			Expected: &api.AgentCheck{
				CheckID:   testNamespacedHealthCheckID,
				Status:    api.HealthCritical,
				Notes:     "",
				Output:    testFailureMessage,
				Type:      ttl,
				Name:      name,
				Namespace: testNamespace,
			},
		},
		{
			Name:                 "precreate a passing pod and change to failed",
			PreCreateHealthCheck: true,
			InitialState:         api.HealthPassing,
			Pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: testNamespace,
					Labels:    map[string]string{labelInject: "true"},
					Annotations: map[string]string{
						annotationStatus:          injected,
						annotationService:         testServiceNameAnnotation,
						annotationConsulNamespace: testNamespace,
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
			Expected: &api.AgentCheck{
				CheckID:   testNamespacedHealthCheckID,
				Status:    api.HealthCritical,
				Output:    testFailureMessage,
				Type:      ttl,
				Name:      name,
				Namespace: testNamespace,
			},
		},
		{
			Name:                 "precreate failed pod and change to passing",
			PreCreateHealthCheck: true,
			InitialState:         api.HealthCritical,
			Pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: testNamespace,
					Labels:    map[string]string{labelInject: "true"},
					Annotations: map[string]string{
						annotationStatus:          injected,
						annotationService:         testServiceNameAnnotation,
						annotationConsulNamespace: testNamespace,
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
			Expected: &api.AgentCheck{
				CheckID:   testNamespacedHealthCheckID,
				Status:    api.HealthPassing,
				Output:    testCheckNotesPassing,
				Type:      ttl,
				Name:      name,
				Namespace: testNamespace,
			},
		},
		{
			Name:                 "precreate failed check, no pod changes results in no health check changes",
			PreCreateHealthCheck: true,
			InitialState:         api.HealthCritical,
			Pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: testNamespace,
					Labels:    map[string]string{labelInject: "true"},
					Annotations: map[string]string{
						annotationStatus:          injected,
						annotationService:         testServiceNameAnnotation,
						annotationConsulNamespace: testNamespace,
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
			Expected: &api.AgentCheck{
				CheckID:   testNamespacedHealthCheckID,
				Status:    api.HealthCritical,
				Output:    "", // when there is no change in status, Consul doesnt set the Output field
				Type:      ttl,
				Name:      name,
				Namespace: testNamespace,
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			// Get a server, client, and handler.
			server, client, resource := testServerAgentResourceAndControllerWithConsulNS(t, tt.Pod, testNamespace)
			defer server.Stop()
			// Create the namespace in Consul.
			_, _, err := client.Namespaces().Create(&api.Namespace{Name: testNamespace}, nil)
			require.NoError(err)

			// Register the service with Consul.
			err = client.Agent().ServiceRegister(&api.AgentServiceRegistration{
				ID:        testServiceNameReg,
				Name:      testServiceNameAnnotation,
				Namespace: testNamespace,
			})
			require.NoError(err)
			if tt.PreCreateHealthCheck {
				// Register the health check if this is not an object create path.
				registerHealthCheck(t, client, tt.InitialState)
			}
			// Upsert and Reconcile both use reconcilePod to reconcile a pod.
			err = resource.reconcilePod(tt.Pod)
			require.NoError(err)
			// Get the agent checks if they were registered.
			actual := getConsulAgentChecks(t, client, testNamespacedHealthCheckID)
			require.True(cmp.Equal(actual, tt.Expected, cmpopts.IgnoreFields(api.AgentCheck{}, ignoredFieldsEnterprise...)))
		})
	}
}
