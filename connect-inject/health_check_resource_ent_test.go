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

func TestReconcilePodWithNamespace(t *testing.T) {
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
			"reconcilePod will create check and set passed",
			false,
			api.HealthPassing,
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: testNamespace,
					Labels:    map[string]string{labelInject: "true"},
					Annotations: map[string]string{
						annotationStatus:                     injected,
						annotationService:                    testServiceNameAnnotation,
						annotationConsulDestinationNamespace: testNamespace,
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
				CheckID:   testNamespacedHealthCheckID,
				Status:    api.HealthPassing,
				Notes:     "",
				Output:    kubernetesSuccessReasonMsg,
				Type:      ttl,
				Name:      name,
				Namespace: testNamespace,
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
					Namespace: testNamespace,
					Labels:    map[string]string{labelInject: "true"},
					Annotations: map[string]string{
						annotationStatus:                     injected,
						annotationService:                    testServiceNameAnnotation,
						annotationConsulDestinationNamespace: testNamespace,
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
				CheckID:   testNamespacedHealthCheckID,
				Status:    api.HealthCritical,
				Notes:     "",
				Output:    testFailureMessage,
				Type:      ttl,
				Name:      name,
				Namespace: testNamespace,
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
					Namespace: testNamespace,
					Labels:    map[string]string{labelInject: "true"},
					Annotations: map[string]string{
						annotationStatus:                     injected,
						annotationService:                    testServiceNameAnnotation,
						annotationConsulDestinationNamespace: testNamespace,
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
				CheckID:   testNamespacedHealthCheckID,
				Status:    api.HealthCritical,
				Output:    testFailureMessage,
				Type:      ttl,
				Name:      name,
				Namespace: testNamespace,
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
					Namespace: testNamespace,
					Labels:    map[string]string{labelInject: "true"},
					Annotations: map[string]string{
						annotationStatus:                     injected,
						annotationService:                    testServiceNameAnnotation,
						annotationConsulDestinationNamespace: testNamespace,
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
				CheckID:   testNamespacedHealthCheckID,
				Status:    api.HealthPassing,
				Output:    testCheckNotesPassing,
				Type:      ttl,
				Name:      name,
				Namespace: testNamespace,
			},
			"",
		},
		{
			"precreacte failed check, no pod changes results in no healthcheck changes",
			true,
			api.HealthCritical,
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: testNamespace,
					Labels:    map[string]string{labelInject: "true"},
					Annotations: map[string]string{
						annotationStatus:                     injected,
						annotationService:                    testServiceNameAnnotation,
						annotationConsulDestinationNamespace: testNamespace,
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
				CheckID:   testNamespacedHealthCheckID,
				Status:    api.HealthCritical,
				Output:    "", // when there is no change in status, Consul doesnt set the Output field
				Type:      ttl,
				Name:      name,
				Namespace: testNamespace,
			},
			"",
		},
		{
			"PodNotRunning will be ignored for processing",
			false,
			"",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: testNamespace,
					Labels:    map[string]string{labelInject: "true"},
					Annotations: map[string]string{
						annotationStatus:                     injected,
						annotationService:                    testServiceNameAnnotation,
						annotationConsulDestinationNamespace: testNamespace,
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
			"PodRunning no annotations will be ignored for processing",
			false,
			"",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: testNamespace,
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
			"PodRunning no Ready Status will be ignored for processing",
			false,
			"",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: testNamespace,
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
	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			var err error
			require := require.New(t)
			// Get a server, client, and handler.
			server, client, resource := testServerAgentResourceAndController(t, tt.Pod)
			defer server.Stop()
			// Register the service with Consul.
			_, _, err = client.Namespaces().Create(&api.Namespace{Name: testNamespace}, nil)
			require.NoError(err)
			err = client.Agent().ServiceRegister(&api.AgentServiceRegistration{
				ID:        testServiceNameReg,
				Name:      testServiceNameAnnotation,
				Namespace: testNamespace,
			})
			require.NoError(err)
			if tt.PreCreateHealthCheck {
				// Register the health check if this is not an object create path.
				registerHealthCheck(t, client, tt.Pod, tt.InitialState)
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
			actual := getConsulAgentChecks(t, client, testNamespacedHealthCheckID)
			require.True(cmp.Equal(actual, tt.Expected, cmpopts.IgnoreFields(api.AgentCheck{}, ignoredFieldsEnterprise...)))
		})
	}
}
