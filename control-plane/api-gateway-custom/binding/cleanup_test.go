// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

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

	mapset "github.com/deckarep/golang-set/v2"
	logrtest "github.com/go-logr/logr/testing"
	"github.com/stretchr/testify/require"

	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
	"github.com/hashicorp/consul/api"
)

func TestCleaner_Run(t *testing.T) {
	cases := map[string]struct {
		bindingRules                     []*api.ACLBindingRule
		expectedDeletedACLBindingRuleIDs mapset.Set[string]
		aclRole                          *api.ACLRole
		expectedDeletedACLRoleIDs        mapset.Set[string]
		aclPolicy                        *api.ACLPolicy
		expectedDeletedACLPolicyIDs      mapset.Set[string]
		inlineCerts                      []*api.InlineCertificateConfigEntry
		expxectedDeletedCertsName        mapset.Set[string]
		apiGateways                      []*api.APIGatewayConfigEntry
	}{
		// add binding rules that match on selector and name to be cleaned up
		"all old roles/policies/bindingrules and inline certs get cleaned up": {
			bindingRules: []*api.ACLBindingRule{
				{
					ID:       "1223445",
					BindName: "totally-valid-name",
					Selector: "non-matching selector",
				},
				{
					ID:       "1234",
					BindName: oldACLRoleName,
					Selector: "matching selector",
				},
				{
					ID:       "4567",
					BindName: "new role",
					Selector: "matching selector",
				},
			},
			expectedDeletedACLBindingRuleIDs: mapset.NewSet("1234"),
			aclRole: &api.ACLRole{
				ID:   "abcd",
				Name: oldACLRoleName,
			},
			expectedDeletedACLRoleIDs: mapset.NewSet("abcd"),
			aclPolicy: &api.ACLPolicy{
				ID:   "defg",
				Name: oldACLPolicyName,
			},
			expectedDeletedACLPolicyIDs: mapset.NewSet("defg"),
			inlineCerts: []*api.InlineCertificateConfigEntry{
				{
					Kind: api.InlineCertificate,
					Name: "my-inline-cert",
				},
			},
			expxectedDeletedCertsName: mapset.NewSet("my-inline-cert"),
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
		"acl roles/policies/binding-rules do not get cleaned up because they are still being referenced": {
			bindingRules: []*api.ACLBindingRule{
				{
					ID:       "1234",
					BindName: oldACLRoleName,
					Selector: "matching selector",
				},
				{
					ID:       "1223445",
					BindName: "totally-valid-name",
					Selector: "non-matching selector",
				},
			},
			expectedDeletedACLBindingRuleIDs: mapset.NewSet[string](),
			aclRole: &api.ACLRole{
				ID:   "abcd",
				Name: oldACLRoleName,
			},
			expectedDeletedACLRoleIDs: mapset.NewSet[string](),
			aclPolicy: &api.ACLPolicy{
				ID:   "defg",
				Name: oldACLPolicyName,
			},
			expectedDeletedACLPolicyIDs: mapset.NewSet[string](),
			inlineCerts: []*api.InlineCertificateConfigEntry{
				{
					Kind: api.InlineCertificate,
					Name: "my-inline-cert",
				},
			},
			expxectedDeletedCertsName: mapset.NewSet("my-inline-cert"),
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
		"acl roles/policies aren't deleted because one binding-rule still references them": {
			bindingRules: []*api.ACLBindingRule{
				{
					ID:       "1234",
					BindName: oldACLRoleName,
					Selector: "matching selector",
				},
				{
					ID:       "5678",
					BindName: "new-name",
					Selector: "matching selector",
				},
				{
					ID:       "101010",
					BindName: oldACLRoleName,
					Selector: "selector to another gateway",
				},
				{
					ID:       "1223445",
					BindName: "totally-valid-name",
					Selector: "non-matching selector",
				},
			},
			expectedDeletedACLBindingRuleIDs: mapset.NewSet("1234"),
			aclRole: &api.ACLRole{
				ID:   "abcd",
				Name: oldACLRoleName,
			},
			expectedDeletedACLRoleIDs: mapset.NewSet[string](),
			aclPolicy: &api.ACLPolicy{
				ID:   "defg",
				Name: oldACLPolicyName,
			},
			expectedDeletedACLPolicyIDs: mapset.NewSet[string](),
			inlineCerts: []*api.InlineCertificateConfigEntry{
				{
					Kind: api.InlineCertificate,
					Name: "my-inline-cert",
				},
			},
			expxectedDeletedCertsName: mapset.NewSet("my-inline-cert"),
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
		"inline cert does not get cleaned up because it is still being referenced": {
			bindingRules: []*api.ACLBindingRule{
				{
					ID:       "1223445",
					BindName: "totally-valid-name",
					Selector: "non-matching selector",
				},
				{
					ID:       "1234",
					BindName: oldACLRoleName,
					Selector: "matching selector",
				},
				{
					ID:       "4567",
					BindName: "new role",
					Selector: "matching selector",
				},
			},
			expectedDeletedACLBindingRuleIDs: mapset.NewSet("1234"),
			aclRole: &api.ACLRole{
				ID:   "abcd",
				Name: oldACLRoleName,
			},
			expectedDeletedACLRoleIDs: mapset.NewSet("abcd"),
			aclPolicy: &api.ACLPolicy{
				ID:   "defg",
				Name: oldACLPolicyName,
			},
			expectedDeletedACLPolicyIDs: mapset.NewSet("defg"),
			inlineCerts: []*api.InlineCertificateConfigEntry{
				{
					Kind: api.InlineCertificate,
					Name: "my-inline-cert",
				},
			},
			expxectedDeletedCertsName: mapset.NewSet[string](),
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
				{
					Kind: api.APIGateway,
					Name: "my-api-gateway-2",
					Listeners: []api.APIGatewayListener{
						{
							Name: "listener",
							TLS: api.APIGatewayTLSConfiguration{
								Certificates: []api.ResourceReference{
									{
										Kind: api.InlineCertificate,
										Name: "my-inline-cert",
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
			deletedCertsName := mapset.NewSet[string]()
			deletedACLPolicyIDs := mapset.NewSet[string]()
			deletedACLRoleIDs := mapset.NewSet[string]()
			deletedACLBindingRuleIDs := mapset.NewSet[string]()

			consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				path := r.URL.Path
				method := r.Method
				switch {
				case path == "/v1/acl/binding-rules":
					val, err := json.Marshal(tc.bindingRules)
					require.NoError(t, err)
					fmt.Fprintln(w, string(val))
				case strings.HasPrefix(path, "/v1/acl/binding-rule/") && method == "DELETE":
					deletedACLBindingRuleIDs.Add(strings.TrimPrefix(path, "/v1/acl/binding-rule/"))
				case strings.HasPrefix(path, "/v1/acl/role/name/"):
					val, err := json.Marshal(tc.aclRole)
					require.NoError(t, err)
					fmt.Fprintln(w, string(val))
				case strings.HasPrefix(path, "/v1/acl/role/") && method == "DELETE":
					deletedACLRoleIDs.Add(strings.TrimPrefix(path, "/v1/acl/role/"))
				case strings.HasPrefix(path, "/v1/acl/policy/name/"):
					val, err := json.Marshal(tc.aclPolicy)
					require.NoError(t, err)
					fmt.Fprintln(w, string(val))
				case strings.HasPrefix(path, "/v1/acl/policy/") && method == "DELETE":
					deletedACLPolicyIDs.Add(strings.TrimPrefix(path, "/v1/acl/policy/"))
				case path == "/v1/config/inline-certificate" && method == "GET":
					val, err := json.Marshal(tc.inlineCerts)
					require.NoError(t, err)
					fmt.Fprintln(w, string(val))
				case path == "/v1/config/api-gateway":
					val, err := json.Marshal(tc.apiGateways)
					require.NoError(t, err)
					fmt.Fprintln(w, string(val))
				case strings.HasPrefix(path, "/v1/config/inline-certificate/") && method == "DELETE":
					deletedCertsName.Add(strings.TrimPrefix(path, "/v1/config/inline-certificate/"))
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

			// if these get flakey increase the times here
			sleepTime = 50 * time.Millisecond
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			c.Run(ctx)
			cancel()
			require.ElementsMatch(t, mapset.Sorted(tc.expectedDeletedACLBindingRuleIDs), mapset.Sorted(deletedACLBindingRuleIDs))
			require.ElementsMatch(t, mapset.Sorted(tc.expectedDeletedACLRoleIDs), mapset.Sorted(deletedACLRoleIDs))
			require.ElementsMatch(t, mapset.Sorted(tc.expectedDeletedACLPolicyIDs), mapset.Sorted(deletedACLPolicyIDs))
			require.ElementsMatch(t, mapset.Sorted(tc.expxectedDeletedCertsName), mapset.Sorted(deletedCertsName))
		})
	}
}
