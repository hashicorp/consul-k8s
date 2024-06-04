package binding

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	logrtest "github.com/go-logr/logr/testing"
	"github.com/stretchr/testify/require"

	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
	"github.com/hashicorp/consul/api"
)

func TestCleaner_Run(t *testing.T) {
	cases := map[string]struct {
		bindingRules                []*api.ACLBindingRule
		aclRole                     *api.ACLRole
		expectedDeletedACLRoleIDs   []string
		aclPolicy                   *api.ACLPolicy
		expectedDeletedACLPolicyIDs []string
		inlineCerts                 []*api.InlineCertificateConfigEntry
		expxectedDeletedCertsName   []string
		apiGateways                 []*api.APIGatewayConfigEntry
	}{
		"everything gets cleaned up": {
			bindingRules: []*api.ACLBindingRule{},
			aclRole: &api.ACLRole{
				ID:   "abcd",
				Name: oldACLRoleName,
			},
			expectedDeletedACLRoleIDs: []string{"abcd"},
			aclPolicy: &api.ACLPolicy{
				ID:   "defg",
				Name: oldACLPolicyName,
			},
			expectedDeletedACLPolicyIDs: []string{"defg"},
			inlineCerts: []*api.InlineCertificateConfigEntry{
				{
					Kind: api.InlineCertificate,
					Name: "my-inline-cert",
				},
			},
			expxectedDeletedCertsName: []string{"my-inline-cert"},
			apiGateways: []*api.APIGatewayConfigEntry{
				{
					Kind: api.APIGateway,
					Name: "my-api-gateway",
					Listeners: []api.APIGatewayListener{
						{
							Name: "listener",
							TLS: api.APIGatewayTLSConfiguration{
								Certificates: []api.ResourceReference{
									{
										Kind: api.FileSystemCertificate,
										Name: "cert",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			deletedCertsName := make([]string, 0)
			deletedACLPolicyIDs := make([]string, 0)
			deletedACLRoleIDs := make([]string, 0)

			consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				path := r.URL.Path
				method := r.Method
				switch {
				case path == "/v1/acl/binding-rules":
					val, err := json.Marshal(tc.bindingRules)
					require.NoError(t, err)
					fmt.Fprintln(w, string(val))
				case strings.HasPrefix(path, "/v1/acl/role/name/"):
					val, err := json.Marshal(tc.aclRole)
					require.NoError(t, err)
					fmt.Fprintln(w, string(val))
				case strings.HasPrefix(path, "/v1/acl/role/") && method == "DELETE":
					deletedACLRoleIDs = append(deletedACLRoleIDs, strings.TrimPrefix(path, "/v1/acl/role/"))
				case strings.HasPrefix(path, "/v1/acl/policy/name/"):
					val, err := json.Marshal(tc.aclPolicy)
					require.NoError(t, err)
					fmt.Fprintln(w, string(val))
				case strings.HasPrefix(path, "/v1/acl/policy/") && method == "DELETE":
					deletedACLPolicyIDs = append(deletedACLPolicyIDs, strings.TrimPrefix(path, "/v1/acl/policy/"))
				case path == "/v1/config/inline-certificate" && method == "GET":
					val, err := json.Marshal(tc.inlineCerts)
					require.NoError(t, err)
					fmt.Fprintln(w, string(val))
				case path == "/v1/config/api-gateway":
					val, err := json.Marshal(tc.apiGateways)
					require.NoError(t, err)
					fmt.Fprintln(w, string(val))
				case strings.HasPrefix(path, "/v1/config/inline-certificate/") && method == "DELETE":
					deletedCertsName = append(deletedCertsName, strings.TrimPrefix(path, "/v1/config/inline-certificate/"))
				default:
					w.WriteHeader(500)
					fmt.Fprintln(w, "Mock Server not configured for this route: "+r.URL.Path)
				}
			}))
			defer consulServer.Close()

			serverURL, err := url.Parse(consulServer.URL)
			require.NoError(t, err)

			port, err := strconv.Atoi(serverURL.Port())
			require.NoError(t, err)

			c := Cleaner{
				Logger: logrtest.NewTestLogger(t),
				ConsulConfig: &consul.Config{
					APIClientConfig: &api.Config{},
					HTTPPort:        port,
					GRPCPort:        port,
					APITimeout:      0,
				},
				ServerMgr:  test.MockConnMgrForIPAndPort(t, serverURL.Hostname(), port, false),
				AuthMethod: "consul-k8s-auth-method",
			}

			sleepTime = 1 * time.Second
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			go func() {
				time.Sleep(5 * time.Second)
				cancel()
			}()
			c.Run(ctx)

			require.ElementsMatch(t, tc.expectedDeletedACLRoleIDs, deletedACLRoleIDs)
			require.ElementsMatch(t, tc.expectedDeletedACLPolicyIDs, deletedACLPolicyIDs)
		})
	}
}
