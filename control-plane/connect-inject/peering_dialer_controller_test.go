package connectinject

import (
	"context"
	"net/http"
	"testing"

	logrtest "github.com/go-logr/logr/testing"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestReconcileDeletePeeringDialer reconciles a PeeringDialer resource that is no longer in Kubernetes, but still
// exists in Consul.
func TestReconcileDeletePeeringDialer(t *testing.T) {
	t.Parallel()
	nodeName := "test-node"
	cases := []struct {
		name                   string
		initialConsulPeerNames []string
		expErr                 string
	}{
		{
			name: "PeeringDialer no longer in K8s, still exists in Consul",
			initialConsulPeerNames: []string{
				"dialer-deleted",
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Add the default namespace.
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}

			// Create fake k8s client.
			k8sObjects := []runtime.Object{&ns}

			// Add peering types to the scheme.
			s := scheme.Scheme
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.PeeringDialer{}, &v1alpha1.PeeringDialerList{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(k8sObjects...).Build()

			// Create test consul server.
			consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				c.NodeName = nodeName
			})
			require.NoError(t, err)
			defer consul.Stop()
			consul.WaitForServiceIntentions(t)

			cfg := &api.Config{
				Address: consul.HTTPAddr,
			}
			consulClient, err := api.NewClient(cfg)
			require.NoError(t, err)

			// Add the initial peerings into Consul by calling the Generate token endpoint.
			_, _, err = consulClient.Peerings().GenerateToken(context.Background(), api.PeeringGenerateTokenRequest{PeerName: tt.initialConsulPeerNames[0]}, nil)
			require.NoError(t, err)

			// Create the peering acceptor controller.
			pdc := &PeeringDialerController{
				Client:       fakeClient,
				Log:          logrtest.TestLogger{T: t},
				ConsulClient: consulClient,
				Scheme:       s,
			}
			namespacedName := types.NamespacedName{
				Name:      "dialer-deleted",
				Namespace: "default",
			}

			// Reconcile a resource that is not in K8s, but is still in Consul.
			resp, err := pdc.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: namespacedName,
			})
			if tt.expErr != "" {
				require.EqualError(t, err, tt.expErr)
			} else {
				require.NoError(t, err)
			}
			require.False(t, resp.Requeue)

			// After reconciliation, Consul should not have the peering.
			readReq := api.PeeringReadRequest{Name: "dialer-deleted"}
			peering, _, err := consulClient.Peerings().Read(context.Background(), readReq, nil)
			var statusErr api.StatusError
			require.ErrorAs(t, err, &statusErr)
			require.Equal(t, http.StatusNotFound, statusErr.Code)
			require.Nil(t, peering)
		})
	}
}
