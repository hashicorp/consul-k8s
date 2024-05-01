package configentries_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	logrtest "github.com/go-logr/logr/testing"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capi "github.com/hashicorp/consul/api"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/controllers/configentries"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

func TestReconcile(tt *testing.T) {
	cases := map[string]struct {
		regName           string
		regID             string
		consulShouldError bool
	}{
		"success": {
			regName:           "test-reg",
			regID:             "test-reg-id",
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
			defer consulServer.Close()

			parsedURL, err := url.Parse(consulServer.URL)
			require.NoError(t, err)

			port, err := strconv.Atoi(parsedURL.Port())
			require.NoError(t, err)

			testClient := &test.TestServerClient{
				Cfg:     &consul.Config{APIClientConfig: &capi.Config{}, HTTPPort: port},
				Watcher: test.MockConnMgrForIPAndPort(t, parsedURL.Host, port, false),
			}

			reg := &v1alpha1.Registration{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Registration",
					APIVersion: "consul.hashicorp.com/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      tc.regName,
					Namespace: name,
				},
				Spec: v1alpha1.RegistrationSpec{
					ID:         "node-id",
					Node:       "virtual-node",
					Address:    "127.0.0.1",
					Datacenter: "dc1",
					Service: v1alpha1.Service{
						ID:      tc.regID,
						Name:    tc.regName,
						Port:    8080,
						Address: "127.0.0.1",
					},
				},
			}

			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(reg).Build()

			controller := &configentries.RegistrationsController{
				Client:              fakeClient,
				Log:                 logrtest.NewTestLogger(t),
				Scheme:              s,
				ConsulClientConfig:  testClient.Cfg,
				ConsulServerConnMgr: testClient.Watcher,
			}

			_, err = controller.Reconcile(ctx, ctrl.Request{})
			require.NoError(t, err)
		})
	}
}
