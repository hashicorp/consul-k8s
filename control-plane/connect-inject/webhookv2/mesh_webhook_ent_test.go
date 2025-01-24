// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

//go:build enterprise

package webhookv2

import (
	"context"
	"testing"

	"github.com/deckarep/golang-set"
	logrtest "github.com/go-logr/logr/testing"
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
