package v1alpha1

import (
	"context"
	"encoding/json"
	"testing"

	logrtest "github.com/go-logr/logr/testing"
	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestValidatePartitionExports(t *testing.T) {
	otherNS := "other"
	otherPartition := "other"

	cases := map[string]struct {
		existingResources []runtime.Object
		newResource       *PartitionExports
		consulMeta        common.ConsulMeta
		expAllow          bool
		expErrMessage     string
	}{
		"no duplicates, valid": {
			existingResources: nil,
			newResource: &PartitionExports{
				ObjectMeta: metav1.ObjectMeta{
					Name: otherPartition,
				},
				Spec: PartitionExportsSpec{
					Services: []ExportedService{
						{
							Name:      "service",
							Namespace: "service-ns",
							Consumers: []ServiceConsumer{{Partition: "other"}},
						},
					},
				},
			},
			consulMeta: common.ConsulMeta{
				PartitionsEnabled: true,
				Partition:         otherPartition,
			},
			expAllow: true,
		},
		"partitionexports exists": {
			existingResources: []runtime.Object{&PartitionExports{
				ObjectMeta: metav1.ObjectMeta{
					Name: otherPartition,
				},
			}},
			newResource: &PartitionExports{
				ObjectMeta: metav1.ObjectMeta{
					Name: otherPartition,
				},
				Spec: PartitionExportsSpec{
					Services: []ExportedService{
						{
							Name:      "service",
							Namespace: "service-ns",
							Consumers: []ServiceConsumer{{Partition: "other"}},
						},
					},
				},
			},
			consulMeta: common.ConsulMeta{
				PartitionsEnabled: true,
				Partition:         otherPartition,
			},
			expAllow:      false,
			expErrMessage: "partitionexports resource already defined - only one partitionexports entry is supported per Kubernetes cluster",
		},
		"name not partition name": {
			existingResources: []runtime.Object{},
			newResource: &PartitionExports{
				ObjectMeta: metav1.ObjectMeta{
					Name: "local",
				},
				Spec: PartitionExportsSpec{
					Services: []ExportedService{
						{
							Name:      "service",
							Namespace: "service-ns",
							Consumers: []ServiceConsumer{{Partition: "other"}},
						},
					},
				},
			},
			consulMeta: common.ConsulMeta{
				PartitionsEnabled: true,
				Partition:         otherPartition,
			},
			expAllow:      false,
			expErrMessage: "partitionexports.consul.hashicorp.com \"local\" is invalid: name: Invalid value: \"local\": partitionexports resource name must be the same name as the partition, \"other\"",
		},
		"partitions disabled": {
			existingResources: []runtime.Object{},
			newResource: &PartitionExports{
				ObjectMeta: metav1.ObjectMeta{
					Name: otherPartition,
				},
				Spec: PartitionExportsSpec{
					Services: []ExportedService{
						{
							Name:      "service",
							Namespace: "service-ns",
							Consumers: []ServiceConsumer{{Partition: "other"}},
						},
					},
				},
			},
			consulMeta: common.ConsulMeta{
				PartitionsEnabled: false,
				Partition:         "",
			},
			expAllow:      false,
			expErrMessage: "partitionexports.consul.hashicorp.com \"other\" is forbidden: Consul Enterprise Admin Partitions must be enabled to create PartitionExports",
		},
		"no services": {
			existingResources: []runtime.Object{},
			newResource: &PartitionExports{
				ObjectMeta: metav1.ObjectMeta{
					Name: otherPartition,
				},
				Spec: PartitionExportsSpec{
					Services: []ExportedService{},
				},
			},
			consulMeta: common.ConsulMeta{
				PartitionsEnabled: true,
				Partition:         otherPartition,
			},
			expAllow:      false,
			expErrMessage: "partitionexports.consul.hashicorp.com \"other\" is invalid: spec.services: Invalid value: []v1alpha1.ExportedService(nil): at least one service must be exported",
		},
		"service with no consumers": {
			existingResources: []runtime.Object{},
			newResource: &PartitionExports{
				ObjectMeta: metav1.ObjectMeta{
					Name: otherPartition,
				},
				Spec: PartitionExportsSpec{
					Services: []ExportedService{
						{
							Name:      "service",
							Namespace: "service-ns",
							Consumers: []ServiceConsumer{},
						},
					},
				},
			},
			consulMeta: common.ConsulMeta{
				PartitionsEnabled: true,
				Partition:         otherPartition,
			},
			expAllow:      false,
			expErrMessage: "partitionexports.consul.hashicorp.com \"other\" is invalid: spec.services[0]: Invalid value: []v1alpha1.ServiceConsumer(nil): service must have at least 1 consumer.",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			marshalledRequestObject, err := json.Marshal(c.newResource)
			require.NoError(t, err)
			s := runtime.NewScheme()
			s.AddKnownTypes(GroupVersion, &PartitionExports{}, &PartitionExportsList{})
			client := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(c.existingResources...).Build()
			decoder, err := admission.NewDecoder(s)
			require.NoError(t, err)

			validator := &PartitionExportsWebhook{
				Client:       client,
				ConsulClient: nil,
				Logger:       logrtest.TestLogger{T: t},
				decoder:      decoder,
				ConsulMeta:   c.consulMeta,
			}
			response := validator.Handle(ctx, admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      c.newResource.KubernetesName(),
					Namespace: otherNS,
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: marshalledRequestObject,
					},
				},
			})

			require.Equal(t, c.expAllow, response.Allowed)
			if c.expErrMessage != "" {
				require.Equal(t, c.expErrMessage, response.AdmissionResponse.Result.Message)
			}
		})
	}
}
