package connectinject

import (
	"context"
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

// TestReconcileCreateTerminatingGatewayService creates a terminating gateway service
func TestReconcileCreateTerminatingGatewayService(t *testing.T) {
	nodeName := "test-node"
	cases := []struct {
		name           string
		k8sObjects     func() []runtime.Object
		expErr         string
		expectedStatus *v1alpha1.TerminatingGatewayServiceStatus
	}{
		{
			name: "New TerminatingGatewayService registers an external service with Consul and [updates the terminating gateway ACL token]",
			k8sObjects: func() []runtime.Object {
				terminatingGatewayService := &v1alpha1.TerminatingGatewayService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "terminating gateway service-created",
						Namespace: "default",
					},
					Spec: v1alpha1.TerminatingGatewayServiceSpec{
						Service: &v1alpha1.ServiceConfig{
							ID:      "terminating gateway service-created-service",
							Service: "terminating gateway service-created-service",
							Port:    9003,
						},
					},
				}
				return []runtime.Object{terminatingGatewayService}
			},
			expectedStatus: &v1alpha1.TerminatingGatewayServiceStatus{
				ServiceInfoRef: &v1alpha1.ServiceInfoRefStatus{
					ServiceConfig: v1alpha1.ServiceConfig{
						ID:      "terminating gateway service-created-service",
						Service: "terminating gateway service-created-service",
						Port:    9003,
					},
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Add the default namespace.
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			// Create fake k8s client
			k8sObjects := append(tt.k8sObjects(), &ns)

			s := scheme.Scheme
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.TerminatingGatewayService{}, &v1alpha1.TerminatingGatewayServiceList{})
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

			// create the terminating gateway service controller
			controller := &TerminatingGatewayServiceController{
				Client:       fakeClient,
				Log:          logrtest.TestLogger{T: t},
				ConsulClient: consulClient,
				Scheme:       s,
			}
			namespacedName := types.NamespacedName{
				Name:      "terminating gateway service-created",
				Namespace: "default",
			}

			resp, err := controller.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: namespacedName,
			})
			if tt.expErr != "" {
				require.EqualError(t, err, tt.expErr)
			} else {
				require.NoError(t, err)
			}
			require.False(t, resp.Requeue)

			// Get the reconciled TerminatingGatewayService and make assertions on the status
			terminatingGatewayService := &v1alpha1.TerminatingGatewayService{}
			err = fakeClient.Get(context.Background(), namespacedName, terminatingGatewayService)
			require.NoError(t, err)
			if tt.expectedStatus != nil {
				require.Equal(t, tt.expectedStatus.ServiceInfoRef.ID, terminatingGatewayService.ServiceInfoRef().ID)
				require.Equal(t, tt.expectedStatus.ServiceInfoRef.Service, terminatingGatewayService.ServiceInfoRef().Service)
				require.Equal(t, tt.expectedStatus.ServiceInfoRef.Port, terminatingGatewayService.ServiceInfoRef().Port)
			}
		})
	}
}
