// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

//go:build enterprise

package webhook

import (
	"context"
	"testing"

	"github.com/deckarep/golang-set"
	logrtest "github.com/go-logr/logr/testing"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

// This tests the checkAndCreate namespace function that is called
// in meshWebhook.Mutate. Patch generation is tested in the non-enterprise
// tests. Other namespace-specific logic is tested directly in the
// specific methods (shouldInject, consulNamespace).
func TestHandler_MutateWithNamespaces(t *testing.T) {
	t.Parallel()

	basicSpec := corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Name: "web",
			},
		},
	}
	s := runtime.NewScheme()
	s.AddKnownTypes(schema.GroupVersion{Group: "", Version: "v1"}, &corev1.Pod{})
	decoder := admission.NewDecoder(s)

	cases := []struct {
		Name               string
		Webhook            MeshWebhook
		Req                admission.Request
		ExpectedNamespaces []string
	}{
		{
			Name: "single destination namespace 'default' from k8s 'default'",
			Webhook: MeshWebhook{
				Log:                        logrtest.NewTestLogger(t),
				AllowK8sNamespacesSet:      mapset.NewSet("*"),
				DenyK8sNamespacesSet:       mapset.NewSet(),
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "default",
				decoder:                    decoder,
				Clientset:                  defaultTestClientWithNamespace(),
			},
			Req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Object: encodeRaw(t, &corev1.Pod{
						Spec: basicSpec,
					}),
					Namespace: "default",
				},
			},
			ExpectedNamespaces: []string{"default"},
		},

		{
			Name: "single destination namespace 'default' from k8s 'non-default'",
			Webhook: MeshWebhook{
				Log:                        logrtest.NewTestLogger(t),
				AllowK8sNamespacesSet:      mapset.NewSet("*"),
				DenyK8sNamespacesSet:       mapset.NewSet(),
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "default",
				decoder:                    decoder,
				Clientset:                  clientWithNamespace("non-default"),
			},
			Req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Object: encodeRaw(t, &corev1.Pod{
						Spec: basicSpec,
					}),
					Namespace: "non-default",
				},
			},
			ExpectedNamespaces: []string{"default"},
		},

		{
			Name: "single destination namespace 'dest' from k8s 'default'",
			Webhook: MeshWebhook{
				Log:                        logrtest.NewTestLogger(t),
				AllowK8sNamespacesSet:      mapset.NewSet("*"),
				DenyK8sNamespacesSet:       mapset.NewSet(),
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "dest",
				decoder:                    decoder,
				Clientset:                  defaultTestClientWithNamespace(),
			},
			Req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Object: encodeRaw(t, &corev1.Pod{
						Spec: basicSpec,
					}),
					Namespace: "default",
				},
			},
			ExpectedNamespaces: []string{"default", "dest"},
		},

		{
			Name: "single destination namespace 'dest' from k8s 'non-default'",
			Webhook: MeshWebhook{
				Log:                        logrtest.NewTestLogger(t),
				AllowK8sNamespacesSet:      mapset.NewSet("*"),
				DenyK8sNamespacesSet:       mapset.NewSet(),
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "dest",
				decoder:                    decoder,
				Clientset:                  clientWithNamespace("non-default"),
			},
			Req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Object: encodeRaw(t, &corev1.Pod{
						Spec: basicSpec,
					}),
					Namespace: "non-default",
				},
			},
			ExpectedNamespaces: []string{"default", "dest"},
		},

		{
			Name: "mirroring from k8s 'default'",
			Webhook: MeshWebhook{
				Log:                        logrtest.NewTestLogger(t),
				AllowK8sNamespacesSet:      mapset.NewSet("*"),
				DenyK8sNamespacesSet:       mapset.NewSet(),
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "default", // will be overridden
				EnableK8SNSMirroring:       true,
				decoder:                    decoder,
				Clientset:                  defaultTestClientWithNamespace(),
			},
			Req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Object: encodeRaw(t, &corev1.Pod{
						Spec: basicSpec,
					}),
					Namespace: "default",
				},
			},
			ExpectedNamespaces: []string{"default"},
		},

		{
			Name: "mirroring from k8s 'dest'",
			Webhook: MeshWebhook{
				Log:                        logrtest.NewTestLogger(t),
				AllowK8sNamespacesSet:      mapset.NewSet("*"),
				DenyK8sNamespacesSet:       mapset.NewSet(),
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "default", // will be overridden
				EnableK8SNSMirroring:       true,
				decoder:                    decoder,
				Clientset:                  clientWithNamespace("dest"),
			},
			Req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Object: encodeRaw(t, &corev1.Pod{
						Spec: basicSpec,
					}),
					Namespace: "dest",
				},
			},
			ExpectedNamespaces: []string{"default", "dest"},
		},

		{
			Name: "mirroring with prefix from k8s 'default'",
			Webhook: MeshWebhook{
				Log:                        logrtest.NewTestLogger(t),
				AllowK8sNamespacesSet:      mapset.NewSet("*"),
				DenyK8sNamespacesSet:       mapset.NewSet(),
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "default", // will be overridden
				EnableK8SNSMirroring:       true,
				K8SNSMirroringPrefix:       "k8s-",
				decoder:                    decoder,
				Clientset:                  defaultTestClientWithNamespace(),
			},
			Req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Object: encodeRaw(t, &corev1.Pod{
						Spec: basicSpec,
					}),
					Namespace: "default",
				},
			},
			ExpectedNamespaces: []string{"default", "k8s-default"},
		},

		{
			Name: "mirroring with prefix from k8s 'dest'",
			Webhook: MeshWebhook{
				Log:                        logrtest.NewTestLogger(t),
				AllowK8sNamespacesSet:      mapset.NewSet("*"),
				DenyK8sNamespacesSet:       mapset.NewSet(),
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "default", // will be overridden
				EnableK8SNSMirroring:       true,
				K8SNSMirroringPrefix:       "k8s-",
				decoder:                    decoder,
				Clientset:                  clientWithNamespace("dest"),
			},
			Req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Object: encodeRaw(t, &corev1.Pod{
						Spec: basicSpec,
					}),
					Namespace: "dest",
				},
			},
			ExpectedNamespaces: []string{"default", "k8s-dest"},
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
			client := testClient.APIClient

			// Add the client config and watcher to the test's meshWebhook
			tt.Webhook.ConsulConfig = testClient.Cfg
			tt.Webhook.ConsulServerConnMgr = testClient.Watcher

			// Mutate!
			resp := tt.Webhook.Handle(context.Background(), tt.Req)
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
					require.Equalf(t, "Auto-generated by consul-k8s", actNamespace.Description,
						"wrong namespace description for namespace %s", ns)
					require.Containsf(t, actNamespace.Meta, "external-source",
						"namespace %s does not contain external-source metadata key", ns)
					require.Equalf(t, "kubernetes", actNamespace.Meta["external-source"],
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
			{
				Name: "web",
			},
		},
	}

	s := runtime.NewScheme()
	s.AddKnownTypes(schema.GroupVersion{Group: "", Version: "v1"}, &corev1.Pod{})
	decoder := admission.NewDecoder(s)

	cases := []struct {
		Name               string
		Webhook            MeshWebhook
		Req                admission.Request
		ExpectedNamespaces []string
	}{
		{
			Name: "acls + single destination namespace 'default' from k8s 'default'",
			Webhook: MeshWebhook{
				Log:                        logrtest.NewTestLogger(t),
				AllowK8sNamespacesSet:      mapset.NewSet("*"),
				DenyK8sNamespacesSet:       mapset.NewSet(),
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "default",
				CrossNamespaceACLPolicy:    "cross-namespace-policy",
				decoder:                    decoder,
				Clientset:                  defaultTestClientWithNamespace(),
			},
			Req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Object: encodeRaw(t, &corev1.Pod{
						Spec: basicSpec,
					}),
					Namespace: "default",
				},
			},
			ExpectedNamespaces: []string{"default"},
		},

		{
			Name: "acls + single destination namespace 'default' from k8s 'non-default'",
			Webhook: MeshWebhook{
				Log:                        logrtest.NewTestLogger(t),
				AllowK8sNamespacesSet:      mapset.NewSet("*"),
				DenyK8sNamespacesSet:       mapset.NewSet(),
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "default",
				CrossNamespaceACLPolicy:    "cross-namespace-policy",
				decoder:                    decoder,
				Clientset:                  clientWithNamespace("non-default"),
			},
			Req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Object: encodeRaw(t, &corev1.Pod{
						Spec: basicSpec,
					}),
					Namespace: "non-default",
				},
			},
			ExpectedNamespaces: []string{"default"},
		},

		{
			Name: "acls + single destination namespace 'dest' from k8s 'default'",
			Webhook: MeshWebhook{
				Log:                        logrtest.NewTestLogger(t),
				AllowK8sNamespacesSet:      mapset.NewSet("*"),
				DenyK8sNamespacesSet:       mapset.NewSet(),
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "dest",
				CrossNamespaceACLPolicy:    "cross-namespace-policy",
				decoder:                    decoder,
				Clientset:                  defaultTestClientWithNamespace(),
			},
			Req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Object: encodeRaw(t, &corev1.Pod{
						Spec: basicSpec,
					}),
					Namespace: "default",
				},
			},
			ExpectedNamespaces: []string{"default", "dest"},
		},

		{
			Name: "acls + single destination namespace 'dest' from k8s 'non-default'",
			Webhook: MeshWebhook{
				Log:                        logrtest.NewTestLogger(t),
				AllowK8sNamespacesSet:      mapset.NewSet("*"),
				DenyK8sNamespacesSet:       mapset.NewSet(),
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "dest",
				CrossNamespaceACLPolicy:    "cross-namespace-policy",
				decoder:                    decoder,
				Clientset:                  clientWithNamespace("non-default"),
			},
			Req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Object: encodeRaw(t, &corev1.Pod{
						Spec: basicSpec,
					}),
					Namespace: "non-default",
				},
			},
			ExpectedNamespaces: []string{"default", "dest"},
		},

		{
			Name: "acls + mirroring from k8s 'default'",
			Webhook: MeshWebhook{
				Log:                        logrtest.NewTestLogger(t),
				AllowK8sNamespacesSet:      mapset.NewSet("*"),
				DenyK8sNamespacesSet:       mapset.NewSet(),
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "default", // will be overridden
				EnableK8SNSMirroring:       true,
				CrossNamespaceACLPolicy:    "cross-namespace-policy",
				decoder:                    decoder,
				Clientset:                  defaultTestClientWithNamespace(),
			},
			Req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Object: encodeRaw(t, &corev1.Pod{
						Spec: basicSpec,
					}),
					Namespace: "default",
				},
			},
			ExpectedNamespaces: []string{"default"},
		},

		{
			Name: "acls + mirroring from k8s 'dest'",
			Webhook: MeshWebhook{
				Log:                        logrtest.NewTestLogger(t),
				AllowK8sNamespacesSet:      mapset.NewSet("*"),
				DenyK8sNamespacesSet:       mapset.NewSet(),
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "default", // will be overridden
				EnableK8SNSMirroring:       true,
				CrossNamespaceACLPolicy:    "cross-namespace-policy",
				decoder:                    decoder,
				Clientset:                  clientWithNamespace("dest"),
			},
			Req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Object: encodeRaw(t, &corev1.Pod{
						Spec: basicSpec,
					}),
					Namespace: "dest",
				},
			},
			ExpectedNamespaces: []string{"default", "dest"},
		},

		{
			Name: "acls + mirroring with prefix from k8s 'default'",
			Webhook: MeshWebhook{
				Log:                        logrtest.NewTestLogger(t),
				AllowK8sNamespacesSet:      mapset.NewSet("*"),
				DenyK8sNamespacesSet:       mapset.NewSet(),
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "default", // will be overridden
				EnableK8SNSMirroring:       true,
				K8SNSMirroringPrefix:       "k8s-",
				CrossNamespaceACLPolicy:    "cross-namespace-policy",
				decoder:                    decoder,
				Clientset:                  defaultTestClientWithNamespace(),
			},
			Req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Object: encodeRaw(t, &corev1.Pod{
						Spec: basicSpec,
					}),
					Namespace: "default",
				},
			},
			ExpectedNamespaces: []string{"default", "k8s-default"},
		},

		{
			Name: "acls + mirroring with prefix from k8s 'dest'",
			Webhook: MeshWebhook{
				Log:                        logrtest.NewTestLogger(t),
				AllowK8sNamespacesSet:      mapset.NewSet("*"),
				DenyK8sNamespacesSet:       mapset.NewSet(),
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "default", // will be overridden
				EnableK8SNSMirroring:       true,
				K8SNSMirroringPrefix:       "k8s-",
				CrossNamespaceACLPolicy:    "cross-namespace-policy",
				decoder:                    decoder,
				Clientset:                  clientWithNamespace("dest"),
			},
			Req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Object: encodeRaw(t, &corev1.Pod{
						Spec: basicSpec,
					}),
					Namespace: "dest",
				},
			},
			ExpectedNamespaces: []string{"default", "k8s-dest"},
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			// Set up consul server
			adminToken := "123e4567-e89b-12d3-a456-426614174000"
			testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
				c.ACL.Enabled = true
				c.ACL.Tokens.InitialManagement = adminToken
			})
			client := testClient.APIClient

			// Add the client config and watcher to the test's meshWebhook
			tt.Webhook.ConsulConfig = testClient.Cfg
			tt.Webhook.ConsulServerConnMgr = testClient.Watcher

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

			_, _, err := client.ACL().PolicyCreate(&policyTmpl, &api.WriteOptions{})
			require.NoError(t, err)

			// Mutate!
			resp := tt.Webhook.Handle(context.Background(), tt.Req)
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
					require.Equalf(t, "Auto-generated by consul-k8s", actNamespace.Description,
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

// Test that the annotation for the Consul namespace is added.
func TestHandler_MutateWithNamespaces_Annotation(t *testing.T) {
	t.Parallel()
	sourceKubeNS := "kube-ns"

	cases := map[string]struct {
		ConsulDestinationNamespace string
		Mirroring                  bool
		MirroringPrefix            string
		ExpNamespaceAnnotation     string
	}{
		"dest: default": {
			ConsulDestinationNamespace: "default",
			ExpNamespaceAnnotation:     "default",
		},
		"dest: foo": {
			ConsulDestinationNamespace: "foo",
			ExpNamespaceAnnotation:     "foo",
		},
		"mirroring": {
			Mirroring:              true,
			ExpNamespaceAnnotation: sourceKubeNS,
		},
		"mirroring with prefix": {
			Mirroring:              true,
			MirroringPrefix:        "prefix-",
			ExpNamespaceAnnotation: "prefix-" + sourceKubeNS,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)

			s := runtime.NewScheme()
			s.AddKnownTypes(schema.GroupVersion{Group: "", Version: "v1"}, &corev1.Pod{})
			decoder := admission.NewDecoder(s)

			webhook := MeshWebhook{
				Log:                        logrtest.NewTestLogger(t),
				AllowK8sNamespacesSet:      mapset.NewSet("*"),
				DenyK8sNamespacesSet:       mapset.NewSet(),
				EnableNamespaces:           true,
				ConsulDestinationNamespace: c.ConsulDestinationNamespace,
				EnableK8SNSMirroring:       c.Mirroring,
				K8SNSMirroringPrefix:       c.MirroringPrefix,
				ConsulConfig:               testClient.Cfg,
				ConsulServerConnMgr:        testClient.Watcher,
				decoder:                    decoder,
				Clientset:                  clientWithNamespace(sourceKubeNS),
			}

			pod := corev1.Pod{
				ObjectMeta: v1.ObjectMeta{
					Namespace: sourceKubeNS,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "web",
						},
					},
				},
			}
			request := admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Object:    encodeRaw(t, &pod),
					Namespace: sourceKubeNS,
				},
			}
			resp := webhook.Handle(context.Background(), request)
			require.Equal(t, resp.Allowed, true)

			// Check that the annotation was added as a patch.
			var consulNamespaceAnnotationValue string
			for _, patch := range resp.Patches {
				if patch.Path == "/metadata/annotations" {
					for annotationName, annotationValue := range patch.Value.(map[string]interface{}) {
						if annotationName == constants.AnnotationConsulNamespace {
							consulNamespaceAnnotationValue = annotationValue.(string)
						}
					}
				}
			}
			require.NotEmpty(t, consulNamespaceAnnotationValue, "no namespace annotation set")
			require.Equal(t, c.ExpNamespaceAnnotation, consulNamespaceAnnotationValue)
		})
	}
}
