// +build enterprise

package connectinject

import (
	"testing"
	"time"

	"github.com/deckarep/golang-set"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
	"k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

// This tests the checkAndCreate namespace function that is called
// in handler.Mutate. Patch generation is tested in the non-enterprise
// tests. Other namespace-specific logic is tested directly in the
// specific methods (shouldInject, consulNamespace).
func TestHandler_MutateWithNamespaces(t *testing.T) {
	t.Parallel()

	basicSpec := corev1.PodSpec{
		Containers: []corev1.Container{
			corev1.Container{
				Name: "web",
			},
		},
	}

	cases := []struct {
		Name               string
		Handler            Handler
		Req                v1beta1.AdmissionRequest
		ExpectedNamespaces []string
	}{
		{
			"single destination namespace 'default' from k8s 'default'",
			Handler{
				Log:                        hclog.Default().Named("handler"),
				AllowK8sNamespacesSet:      mapset.NewSet("*"),
				DenyK8sNamespacesSet:       mapset.NewSet(),
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "default",
			},
			v1beta1.AdmissionRequest{
				Object: encodeRaw(t, &corev1.Pod{
					Spec: basicSpec,
				}),
				Namespace: "default",
			},
			[]string{"default"},
		},

		{
			"single destination namespace 'default' from k8s 'non-default'",
			Handler{
				Log:                        hclog.Default().Named("handler"),
				AllowK8sNamespacesSet:      mapset.NewSet("*"),
				DenyK8sNamespacesSet:       mapset.NewSet(),
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "default",
			},
			v1beta1.AdmissionRequest{
				Object: encodeRaw(t, &corev1.Pod{
					Spec: basicSpec,
				}),
				Namespace: "non-default",
			},
			[]string{"default"},
		},

		{
			"single destination namespace 'dest' from k8s 'default'",
			Handler{
				Log:                        hclog.Default().Named("handler"),
				AllowK8sNamespacesSet:      mapset.NewSet("*"),
				DenyK8sNamespacesSet:       mapset.NewSet(),
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "dest",
			},
			v1beta1.AdmissionRequest{
				Object: encodeRaw(t, &corev1.Pod{
					Spec: basicSpec,
				}),
				Namespace: "default",
			},
			[]string{"default", "dest"},
		},

		{
			"single destination namespace 'dest' from k8s 'non-default'",
			Handler{
				Log:                        hclog.Default().Named("handler"),
				AllowK8sNamespacesSet:      mapset.NewSet("*"),
				DenyK8sNamespacesSet:       mapset.NewSet(),
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "dest",
			},
			v1beta1.AdmissionRequest{
				Object: encodeRaw(t, &corev1.Pod{
					Spec: basicSpec,
				}),
				Namespace: "non-default",
			},
			[]string{"default", "dest"},
		},

		{
			"mirroring from k8s 'default'",
			Handler{
				Log:                        hclog.Default().Named("handler"),
				AllowK8sNamespacesSet:      mapset.NewSet("*"),
				DenyK8sNamespacesSet:       mapset.NewSet(),
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "default", // will be overridden
				EnableK8SNSMirroring:       true,
			},
			v1beta1.AdmissionRequest{
				Object: encodeRaw(t, &corev1.Pod{
					Spec: basicSpec,
				}),
				Namespace: "default",
			},
			[]string{"default"},
		},

		{
			"mirroring from k8s 'dest'",
			Handler{
				Log:                        hclog.Default().Named("handler"),
				AllowK8sNamespacesSet:      mapset.NewSet("*"),
				DenyK8sNamespacesSet:       mapset.NewSet(),
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "default", // will be overridden
				EnableK8SNSMirroring:       true,
			},
			v1beta1.AdmissionRequest{
				Object: encodeRaw(t, &corev1.Pod{
					Spec: basicSpec,
				}),
				Namespace: "dest",
			},
			[]string{"default", "dest"},
		},

		{
			"mirroring with prefix from k8s 'default'",
			Handler{
				Log:                        hclog.Default().Named("handler"),
				AllowK8sNamespacesSet:      mapset.NewSet("*"),
				DenyK8sNamespacesSet:       mapset.NewSet(),
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "default", // will be overridden
				EnableK8SNSMirroring:       true,
				K8SNSMirroringPrefix:       "k8s-",
			},
			v1beta1.AdmissionRequest{
				Object: encodeRaw(t, &corev1.Pod{
					Spec: basicSpec,
				}),
				Namespace: "default",
			},
			[]string{"default", "k8s-default"},
		},

		{
			"mirroring with prefix from k8s 'dest'",
			Handler{
				Log:                        hclog.Default().Named("handler"),
				AllowK8sNamespacesSet:      mapset.NewSet("*"),
				DenyK8sNamespacesSet:       mapset.NewSet(),
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "default", // will be overridden
				EnableK8SNSMirroring:       true,
				K8SNSMirroringPrefix:       "k8s-",
			},
			v1beta1.AdmissionRequest{
				Object: encodeRaw(t, &corev1.Pod{
					Spec: basicSpec,
				}),
				Namespace: "dest",
			},
			[]string{"default", "k8s-dest"},
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)

			// Set up consul server
			a, err := testutil.NewTestServerT(t)
			require.NoError(err)
			defer a.Stop()

			// Set up consul client
			client, err := api.NewClient(&api.Config{
				Address: a.HTTPAddr,
			})
			require.NoError(err)

			// Add the client to the test's handler
			tt.Handler.ConsulClient = client

			// Mutate!
			resp := tt.Handler.Mutate(&tt.Req)
			require.Equal(resp.Allowed, true)

			// Check all the namespace things
			// Check that we have the right number of namespaces
			namespaces, _, err := client.Namespaces().List(&api.QueryOptions{})
			require.NoError(err)
			require.Len(namespaces, len(tt.ExpectedNamespaces))

			// Check the namespace details
			for _, ns := range tt.ExpectedNamespaces {
				actNamespace, _, err := client.Namespaces().Read(ns, &api.QueryOptions{})
				require.NoErrorf(err, "error getting namespace %s", ns)
				require.NotNilf(actNamespace, "namespace %s was nil", ns)
				require.Equalf(ns, actNamespace.Name, "namespace %s was improperly named", ns)

				// Check created namespace properties
				if ns != "default" {
					require.Equalf("Auto-generated by a Connect Injector", actNamespace.Description,
						"wrong namespace description for namespace %s", ns)
					require.Containsf(actNamespace.Meta, "external-source",
						"namespace %s does not contain external-source metadata key", ns)
					require.Equalf("kubernetes", actNamespace.Meta["external-source"],
						"namespace %s has wrong value for external-source metadata key", ns)
				}

			}
		})
	}
}

// Tests that the correct cross-namespace policy is
// added to created namespaces.
func TestHandler_MutateWithNamespaces_ACLs(t *testing.T) {
	basicSpec := corev1.PodSpec{
		Containers: []corev1.Container{
			corev1.Container{
				Name: "web",
			},
		},
	}

	cases := []struct {
		Name               string
		Handler            Handler
		Req                v1beta1.AdmissionRequest
		ExpectedNamespaces []string
	}{
		{
			"acls + single destination namespace 'default' from k8s 'default'",
			Handler{
				Log:                        hclog.Default().Named("handler"),
				AllowK8sNamespacesSet:      mapset.NewSet("*"),
				DenyK8sNamespacesSet:       mapset.NewSet(),
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "default",
				CrossNamespaceACLPolicy:    "cross-namespace-policy",
			},
			v1beta1.AdmissionRequest{
				Object: encodeRaw(t, &corev1.Pod{
					Spec: basicSpec,
				}),
				Namespace: "default",
			},
			[]string{"default"},
		},

		{
			"acls + single destination namespace 'default' from k8s 'non-default'",
			Handler{
				Log:                        hclog.Default().Named("handler"),
				AllowK8sNamespacesSet:      mapset.NewSet("*"),
				DenyK8sNamespacesSet:       mapset.NewSet(),
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "default",
				CrossNamespaceACLPolicy:    "cross-namespace-policy",
			},
			v1beta1.AdmissionRequest{
				Object: encodeRaw(t, &corev1.Pod{
					Spec: basicSpec,
				}),
				Namespace: "non-default",
			},
			[]string{"default"},
		},

		{
			"acls + single destination namespace 'dest' from k8s 'default'",
			Handler{
				Log:                        hclog.Default().Named("handler"),
				AllowK8sNamespacesSet:      mapset.NewSet("*"),
				DenyK8sNamespacesSet:       mapset.NewSet(),
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "dest",
				CrossNamespaceACLPolicy:    "cross-namespace-policy",
			},
			v1beta1.AdmissionRequest{
				Object: encodeRaw(t, &corev1.Pod{
					Spec: basicSpec,
				}),
				Namespace: "default",
			},
			[]string{"default", "dest"},
		},

		{
			"acls + single destination namespace 'dest' from k8s 'non-default'",
			Handler{
				Log:                        hclog.Default().Named("handler"),
				AllowK8sNamespacesSet:      mapset.NewSet("*"),
				DenyK8sNamespacesSet:       mapset.NewSet(),
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "dest",
				CrossNamespaceACLPolicy:    "cross-namespace-policy",
			},
			v1beta1.AdmissionRequest{
				Object: encodeRaw(t, &corev1.Pod{
					Spec: basicSpec,
				}),
				Namespace: "non-default",
			},
			[]string{"default", "dest"},
		},

		{
			"acls + mirroring from k8s 'default'",
			Handler{
				Log:                        hclog.Default().Named("handler"),
				AllowK8sNamespacesSet:      mapset.NewSet("*"),
				DenyK8sNamespacesSet:       mapset.NewSet(),
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "default", // will be overridden
				EnableK8SNSMirroring:       true,
				CrossNamespaceACLPolicy:    "cross-namespace-policy",
			},
			v1beta1.AdmissionRequest{
				Object: encodeRaw(t, &corev1.Pod{
					Spec: basicSpec,
				}),
				Namespace: "default",
			},
			[]string{"default"},
		},

		{
			"acls + mirroring from k8s 'dest'",
			Handler{
				Log:                        hclog.Default().Named("handler"),
				AllowK8sNamespacesSet:      mapset.NewSet("*"),
				DenyK8sNamespacesSet:       mapset.NewSet(),
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "default", // will be overridden
				EnableK8SNSMirroring:       true,
				CrossNamespaceACLPolicy:    "cross-namespace-policy",
			},
			v1beta1.AdmissionRequest{
				Object: encodeRaw(t, &corev1.Pod{
					Spec: basicSpec,
				}),
				Namespace: "dest",
			},
			[]string{"default", "dest"},
		},

		{
			"acls + mirroring with prefix from k8s 'default'",
			Handler{
				Log:                        hclog.Default().Named("handler"),
				AllowK8sNamespacesSet:      mapset.NewSet("*"),
				DenyK8sNamespacesSet:       mapset.NewSet(),
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "default", // will be overridden
				EnableK8SNSMirroring:       true,
				K8SNSMirroringPrefix:       "k8s-",
				CrossNamespaceACLPolicy:    "cross-namespace-policy",
			},
			v1beta1.AdmissionRequest{
				Object: encodeRaw(t, &corev1.Pod{
					Spec: basicSpec,
				}),
				Namespace: "default",
			},
			[]string{"default", "k8s-default"},
		},

		{
			"acls + mirroring with prefix from k8s 'dest'",
			Handler{
				Log:                        hclog.Default().Named("handler"),
				AllowK8sNamespacesSet:      mapset.NewSet("*"),
				DenyK8sNamespacesSet:       mapset.NewSet(),
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "default", // will be overridden
				EnableK8SNSMirroring:       true,
				K8SNSMirroringPrefix:       "k8s-",
				CrossNamespaceACLPolicy:    "cross-namespace-policy",
			},
			v1beta1.AdmissionRequest{
				Object: encodeRaw(t, &corev1.Pod{
					Spec: basicSpec,
				}),
				Namespace: "dest",
			},
			[]string{"default", "k8s-dest"},
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			// Set up consul server
			a, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				c.ACL.Enabled = true
			})
			require.NoError(t, err)
			defer a.Stop()

			// Set up a client for bootstrapping
			bootClient, err := api.NewClient(&api.Config{
				Address: a.HTTPAddr,
			})
			require.NoError(t, err)

			// Bootstrap the server and get the bootstrap token
			var bootstrapResp *api.ACLToken
			timer := &retry.Timer{Timeout: 10 * time.Second, Wait: 500 * time.Millisecond}
			retry.RunWith(timer, t, func(r *retry.R) {
				bootstrapResp, _, err = bootClient.ACL().Bootstrap()
				require.NoError(r, err)
			})
			bootstrapToken := bootstrapResp.SecretID
			require.NotEmpty(t, bootstrapToken)

			// Set up consul client
			client, err := api.NewClient(&api.Config{
				Address: a.HTTPAddr,
				Token:   bootstrapToken,
			})
			require.NoError(t, err)

			// Add the client to the test's handler
			tt.Handler.ConsulClient = client

			// Create cross namespace policy
			// This would have been created by the acl bootstrapper in the
			// default namespace to be attached to all created namespaces.
			crossNamespaceRules := `namespace_prefix "" {
  service_prefix "" {
    policy = "read"
  }
  node_prefix "" {
    policy = "read"
  }
} `

			policyTmpl := api.ACLPolicy{
				Name:        "cross-namespace-policy",
				Description: "Policy to allow permissions to cross Consul namespaces for k8s services",
				Rules:       crossNamespaceRules,
			}

			_, _, err = client.ACL().PolicyCreate(&policyTmpl, &api.WriteOptions{})
			require.NoError(t, err)

			// Mutate!
			resp := tt.Handler.Mutate(&tt.Req)
			require.Equal(t, resp.Allowed, true)

			// Check all the namespace things
			// Check that we have the right number of namespaces
			namespaces, _, err := client.Namespaces().List(&api.QueryOptions{})
			require.NoError(t, err)
			require.Len(t, namespaces, len(tt.ExpectedNamespaces))

			// Check the namespace details
			for _, ns := range tt.ExpectedNamespaces {
				actNamespace, _, err := client.Namespaces().Read(ns, &api.QueryOptions{})
				require.NoErrorf(t, err, "error getting namespace %s", ns)
				require.NotNilf(t, actNamespace, "namespace %s was nil", ns)
				require.Equalf(t, ns, actNamespace.Name, "namespace %s was improperly named", ns)

				// Check created namespace properties
				if ns != "default" {
					require.Equalf(t, "Auto-generated by a Connect Injector", actNamespace.Description,
						"wrong namespace description for namespace %s", ns)
					require.Containsf(t, actNamespace.Meta, "external-source",
						"namespace %s does not contain external-source metadata key", ns)
					require.Equalf(t, "kubernetes", actNamespace.Meta["external-source"],
						"namespace %s has wrong value for external-source metadata key", ns)

					// Check for ACL policy things
					// The acl bootstrapper will update the `default` namespace, so that
					// can't be tested here.
					require.NotNilf(t, actNamespace.ACLs, "ACLs was nil for namespace %s", ns)
					require.Lenf(t, actNamespace.ACLs.PolicyDefaults, 1, "wrong length for PolicyDefaults in namespace %s", ns)
					require.Equalf(t, "cross-namespace-policy", actNamespace.ACLs.PolicyDefaults[0].Name,
						"wrong policy name for namespace %s", ns)
				}

			}
		})
	}
}
