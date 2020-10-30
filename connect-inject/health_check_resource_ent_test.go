package connectinject

/*
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

func TestUpsert_ABCPodWithNoServiceReturnsError(t *testing.T) {
	t.Parallel()
	t.Run("test-consul-dest-namespace", func(t *testing.T) {
		var err error
		require := require.New(t)
		// Get a server, client, and handler.
		server, client, resource := testServerAgentResourceAndController(t, tt.Pod)
		defer server.Stop()
		// Register the service with Consul.
		server.AddService(t, testServiceNameReg, api.HealthPassing, nil)
		// Register the health check if this is not an object create path.
		registerHealthCheck(t, client, tt.InitialState)
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
		actual := getConsulAgentChecks(t, client)
		require.True(cmp.Equal(actual, tt.Expected, cmpopts.IgnoreFields(api.AgentCheck{}, ignoredFields...)))
	})

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
			HostIP: "127.0.0.1",
			Phase:  corev1.PodRunning,
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

*/
