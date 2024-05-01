package configentries_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	logrtest "github.com/go-logr/logr/testing"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capi "github.com/hashicorp/consul/api"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/controllers/configentries"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

func TestReconcile(tt *testing.T) {
	deletionTime := metav1.Now()
	cases := map[string]struct {
		registration      *v1alpha1.Registration
		consulShouldError bool
	}{
		"success on registration": {
			registration: &v1alpha1.Registration{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Registration",
					APIVersion: "consul.hashicorp.com/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-registration",
				},
				Spec: v1alpha1.RegistrationSpec{
					ID:         "node-id",
					Node:       "virtual-node",
					Address:    "127.0.0.1",
					Datacenter: "dc1",
					Service: v1alpha1.Service{
						ID:      "service-id",
						Name:    "service-name",
						Port:    8080,
						Address: "127.0.0.1",
					},
				},
			},
			consulShouldError: false,
		},
		"success on deregistration": {
			registration: &v1alpha1.Registration{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Registration",
					APIVersion: "consul.hashicorp.com/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-registration",
					DeletionTimestamp: &deletionTime,
				},
				Spec: v1alpha1.RegistrationSpec{
					ID:         "node-id",
					Node:       "virtual-node",
					Address:    "127.0.0.1",
					Datacenter: "dc1",
					Service: v1alpha1.Service{
						ID:      "service-id",
						Name:    "service-name",
						Port:    8080,
						Address: "127.0.0.1",
					},
				},
			},
			consulShouldError: false,
		},
	}

	for name, tc := range cases {
		tt.Run(name, func(t *testing.T) {
			s := runtime.NewScheme()
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.Registration{})
			ctx := context.Background()

			consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.consulShouldError {
					w.WriteHeader(500)
					return
				}
				w.WriteHeader(200)
			}))

			parsedURL, err := url.Parse(consulServer.URL)
			require.NoError(t, err)
			host := strings.Split(parsedURL.Host, ":")[0]

			port, err := strconv.Atoi(parsedURL.Port())
			require.NoError(t, err)

			testClient := &test.TestServerClient{
				Cfg:     &consul.Config{APIClientConfig: &capi.Config{Address: host}, HTTPPort: port},
				Watcher: test.MockConnMgrForIPAndPort(t, host, port, false),
			}

			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tc.registration).Build()

			controller := &configentries.RegistrationsController{
				Client:              fakeClient,
				Log:                 logrtest.NewTestLogger(t),
				Scheme:              s,
				ConsulClientConfig:  testClient.Cfg,
				ConsulServerConnMgr: testClient.Watcher,
			}

			_, err = controller.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{Name: tc.registration.Name, Namespace: tc.registration.Namespace},
			})
			require.NoError(t, err)

			consulServer.Close()
		})
	}
}
