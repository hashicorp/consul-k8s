// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package catalog

import (
	"context"
	"testing"

	mapset "github.com/deckarep/golang-set"
	"github.com/hashicorp/consul-k8s/control-plane/helper/controller"
	consulapi "github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"
)

const nodeName1 = "ip-10-11-12-13.ec2.internal"
const nodeName2 = "ip-10-11-12-14.ec2.internal"

func init() {
	hclog.DefaultOptions.Level = hclog.Debug
}

// Test that deleting a service properly deletes the registration.
func TestServiceResource_createDelete(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	createNodes(t, client)
	createEndpointSlice(t, client, svc.Name, metav1.NamespaceDefault)

	// Delete
	require.NoError(t, client.CoreV1().Services(metav1.NamespaceDefault).Delete(context.Background(), "foo", metav1.DeleteOptions{}))

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 0)
	})
}

// Test that Loadbalancer service weight is set from service annotation.
func TestServiceWeight_ingress(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	svc.Annotations[annotationServiceWeight] = "22"
	svc.Status.LoadBalancer.Ingress = append(
		svc.Status.LoadBalancer.Ingress,
		corev1.LoadBalancerIngress{IP: "3.3.3.3"},
	)

	svc.Status.LoadBalancer.Ingress = append(
		svc.Status.LoadBalancer.Ingress,
		corev1.LoadBalancerIngress{IP: "4.4.4.4"},
	)

	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 3)
		require.Equal(r, "foo", actual[1].Service.Service)
		require.Equal(r, "3.3.3.3", actual[1].Service.Address)
		require.Equal(r, 22, actual[1].Service.Weights.Passing)
		require.Equal(r, "foo", actual[2].Service.Service)
		require.Equal(r, "4.4.4.4", actual[2].Service.Address)
		require.Equal(r, 22, actual[2].Service.Weights.Passing)
		require.NotEqual(r, actual[1].Service.ID, actual[2].Service.ID)
	})
}

// Test that Loadbalancer service weight is set from service annotation.
func TestServiceWeight_externalIP(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	svc.Annotations[annotationServiceWeight] = "22"
	svc.Spec.ExternalIPs = []string{"3.3.3.3", "4.4.4.4"}

	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 2)
		require.Equal(r, "foo", actual[0].Service.Service)
		require.Equal(r, "3.3.3.3", actual[0].Service.Address)
		require.Equal(r, 22, actual[0].Service.Weights.Passing)
		require.Equal(r, "foo", actual[1].Service.Service)
		require.Equal(r, "4.4.4.4", actual[1].Service.Address)
		require.Equal(r, 22, actual[1].Service.Weights.Passing)
		require.NotEqual(r, actual[0].Service.ID, actual[1].Service.ID)
	})
}

// Test service weight.
func TestServiceWeight(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		Weight         string
		ExpectError    bool
		ExtectedWeight int
	}{
		"external-IP": {
			Weight:         "22",
			ExpectError:    false,
			ExtectedWeight: 22,
		},
		"non-int-weight": {
			Weight:         "non-int",
			ExpectError:    true,
			ExtectedWeight: 0,
		},
		"one-weight": {
			Weight:         "1",
			ExpectError:    true,
			ExtectedWeight: 0,
		},
		"zero-weight": {
			Weight:         "0",
			ExpectError:    true,
			ExtectedWeight: 0,
		},
		"negative-weight": {
			Weight:         "-2",
			ExpectError:    true,
			ExtectedWeight: 0,
		},
		"greater-than-100-is-allowed": {
			Weight:         "1000",
			ExpectError:    false,
			ExtectedWeight: 1000,
		},
	}

	for name, c := range cases {
		t.Run(name, func(tt *testing.T) {
			weightI, err := getServiceWeight(c.Weight)
			// Verify what we got
			retry.Run(tt, func(r *retry.R) {
				if c.ExpectError {
					require.Error(r, err)
				} else {
					require.Equal(r, c.ExtectedWeight, weightI)
				}
			})
		})
	}
}

// Test that we're default enabled.
func TestServiceResource_defaultEnable(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 1)
	})
}

// Test that we can explicitly disable.
func TestServiceResource_defaultEnableDisable(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	svc.Annotations[annotationServiceSync] = "false"
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 0)
	})
}

// Test that we can default disable.
func TestServiceResource_defaultDisable(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.ExplicitEnable = true

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 0)
	})
}

// Test that we can default disable but override.
func TestServiceResource_defaultDisableEnable(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.ExplicitEnable = true

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	svc.Annotations[annotationServiceSync] = "t"
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 1)
	})
}

// Test changing the sync tag to false deletes the service.
func TestServiceResource_changeSyncToFalse(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.ExplicitEnable = true

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service with the sync=true
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	svc.Annotations[annotationServiceSync] = "true"
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	// Verify the service gets registered.
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 1)
	})

	// Update the sync annotation to false.
	svc.Annotations[annotationServiceSync] = "false"
	_, err = client.CoreV1().Services(metav1.NamespaceDefault).Update(context.Background(), svc, metav1.UpdateOptions{})
	require.NoError(t, err)

	// Verify the service gets deregistered.
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 0)
	})
}

// Test that the k8s namespace is appended with a '-'
// when AddK8SNamespaceSuffix is true.
func TestServiceResource_addK8SNamespace(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.AddK8SNamespaceSuffix = true

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service with the sync=true
	svc := lbService("foo", "namespace", "1.2.3.4")
	_, err := client.CoreV1().Services("namespace").Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	// Verify that the service name has k8s namespace appended with an '-'
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 1)
		require.Equal(r, actual[0].Service.Service, "foo-namespace")
	})
}

// Test k8s namespace suffix is appended
// when the consul prefix is provided.
func TestServiceResource_addK8SNamespaceWithPrefix(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.AddK8SNamespaceSuffix = true
	serviceResource.ConsulServicePrefix = "prefix"

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service with the sync=true
	svc := lbService("foo", "namespace", "1.2.3.4")
	_, err := client.CoreV1().Services("namespace").Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	// Verify that the service name has k8s namespace appended with an '-'
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 1)
		require.Equal(r, actual[0].Service.Service, "prefixfoo-namespace")
	})
}

// Test that when consul node name is set to a non-default value,
// services are synced to that node.
func TestServiceResource_ConsulNodeName(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.ConsulNodeName = "test-node"

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service with the sync=true
	svc := lbService("foo", "namespace", "1.2.3.4")
	_, err := client.CoreV1().Services("namespace").Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	// Verify that the service name has k8s namespace appended with an '-'
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 1)
		require.Equal(r, actual[0].Node, "test-node")
	})
}

// Test k8s namespace suffix is not appended
// when the service name annotation is provided.
func TestServiceResource_addK8SNamespaceWithNameAnnotation(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.AddK8SNamespaceSuffix = true

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service with the sync=true
	svc := lbService("foo", "bar", "1.2.3.4")
	svc.Annotations[annotationServiceName] = "different-service-name"
	_, err := client.CoreV1().Services("bar").Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	// Verify that the service name annotation is preferred
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 1)
		require.Equal(r, actual[0].Service.Service, "different-service-name")
	})
}

// Test that external IPs take priority.
func TestServiceResource_externalIP(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	svc.Spec.ExternalIPs = []string{"3.3.3.3", "4.4.4.4"}
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 2)
		require.Equal(r, "foo", actual[0].Service.Service)
		require.Equal(r, "3.3.3.3", actual[0].Service.Address)
		require.Equal(r, "foo", actual[1].Service.Service)
		require.Equal(r, "4.4.4.4", actual[1].Service.Address)
	})
}

// Test externalIP with Prefix.
func TestServiceResource_externalIPPrefix(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.ConsulServicePrefix = "prefix"

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	svc.Spec.ExternalIPs = []string{"3.3.3.3", "4.4.4.4"}
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 2)
		require.Equal(r, "prefixfoo", actual[0].Service.Service)
		require.Equal(r, "3.3.3.3", actual[0].Service.Address)
		require.Equal(r, "prefixfoo", actual[1].Service.Service)
		require.Equal(r, "4.4.4.4", actual[1].Service.Address)
	})
}

// Test that the proper registrations are generated for a LoadBalancer.
func TestServiceResource_lb(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 1)
		require.Equal(r, "foo", actual[0].Service.Service)
		require.Equal(r, "1.2.3.4", actual[0].Service.Address)
	})
}

// Test that the proper registrations are generated for a LoadBalancer with a prefix.
func TestServiceResource_lbPrefix(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.ConsulServicePrefix = "prefix"

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 1)
		require.Equal(r, "prefixfoo", actual[0].Service.Service)
		require.Equal(r, "1.2.3.4", actual[0].Service.Address)
	})
}

// Test that the proper registrations are generated for a LoadBalancer
// with multiple endpoints.
func TestServiceResource_lbMultiEndpoint(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	svc.Status.LoadBalancer.Ingress = append(
		svc.Status.LoadBalancer.Ingress,
		corev1.LoadBalancerIngress{IP: "2.3.4.5"},
	)
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 2)
		require.Equal(r, "foo", actual[0].Service.Service)
		require.Equal(r, "1.2.3.4", actual[0].Service.Address)
		require.Equal(r, "foo", actual[1].Service.Service)
		require.Equal(r, "2.3.4.5", actual[1].Service.Address)
		require.NotEqual(r, actual[1].Service.ID, actual[0].Service.ID)
	})
}

// Test explicit name annotation.
func TestServiceResource_lbAnnotatedName(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	svc.Annotations[annotationServiceName] = "bar"
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 1)
		require.Equal(r, "bar", actual[0].Service.Service)
	})
}

// Test default port and additional ports in the meta.
func TestServiceResource_lbPort(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	svc.Spec.Ports = []corev1.ServicePort{
		{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080)},
		{Name: "rpc", Port: 8500, TargetPort: intstr.FromInt(2000)},
	}
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 1)
		require.Equal(r, 80, actual[0].Service.Port)
		require.Equal(r, "80", actual[0].Service.Meta["port-http"])
		require.Equal(r, "8500", actual[0].Service.Meta["port-rpc"])
	})
}

// Test default port works with override annotation.
func TestServiceResource_lbAnnotatedPort(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	svc.Annotations[annotationServicePort] = "rpc"
	svc.Spec.Ports = []corev1.ServicePort{
		{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080)},
		{Name: "rpc", Port: 8500, TargetPort: intstr.FromInt(2000)},
	}
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 1)
		require.Equal(r, 8500, actual[0].Service.Port)
		require.Equal(r, "80", actual[0].Service.Meta["port-http"])
		require.Equal(r, "8500", actual[0].Service.Meta["port-rpc"])
	})
}

// Test annotated tags.
func TestServiceResource_lbAnnotatedTags(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.ConsulK8STag = TestConsulK8STag

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	svc.Annotations[annotationServiceTags] = `one, leadingwhitespace,trailingwhitespace ,\,leadingcomma,trailingcomma\,,middle\,comma,,`
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 1)
		require.Equal(r, []string{"k8s", "one", "leadingwhitespace", "trailingwhitespace", ",leadingcomma", "trailingcomma,", "middle,comma"}, actual[0].Service.Tags)
	})
}

// Test annotated service meta.
func TestServiceResource_lbAnnotatedMeta(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	svc.Annotations[annotationServiceMetaPrefix+"foo"] = "bar"
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 1)
		require.Equal(r, "bar", actual[0].Service.Meta["foo"])
	})
}

// Test that with LoadBalancerEndpointsSync set to true we track the IP of the endpoints not the LB IP/name.
func TestServiceResource_lbRegisterEndpoints(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.LoadBalancerEndpointsSync = true

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	node1, _ := createNodes(t, client)

	// Insert the endpoint slice
	_, err := client.DiscoveryV1().EndpointSlices(metav1.NamespaceDefault).Create(
		context.Background(),
		&discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "foo-",
				Labels:       map[string]string{discoveryv1.LabelServiceName: "foo"},
			},
			AddressType: discoveryv1.AddressTypeIPv4,
			Endpoints: []discoveryv1.Endpoint{
				{
					Addresses: []string{"8.8.8.8"},
					Conditions: discoveryv1.EndpointConditions{
						Ready:       ptr.To(true),
						Serving:     ptr.To(true),
						Terminating: ptr.To(false),
					},
					TargetRef: &corev1.ObjectReference{Kind: "pod", Name: "foo", Namespace: metav1.NamespaceDefault},
					NodeName:  &node1.Name,
					Zone:      ptr.To("us-west-2a"),
				},
			},
			Ports: []discoveryv1.EndpointPort{
				{
					Name: ptr.To("http"),
					Port: ptr.To(int32(8080)),
				},
				{
					Name: ptr.To("rpc"),
					Port: ptr.To(int32(2000)),
				},
			},
		},
		metav1.CreateOptions{})
	require.NoError(t, err)

	// Insert an LB service
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	_, err = client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 1)
		require.Equal(r, "foo", actual[0].Service.Service)
		require.Equal(r, "8.8.8.8", actual[0].Service.Address)
		require.Equal(r, 8080, actual[0].Service.Port)
		require.Equal(r, "k8s-sync", actual[0].Node)
	})
}

// Test that the proper registrations are generated for a NodePort type.
func TestServiceResource_nodePort(t *testing.T) {
	t.Parallel()
	syncer := newTestSyncer()
	client := fake.NewSimpleClientset()
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.NodePortSync = ExternalOnly

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	createNodes(t, client)

	createEndpointSlice(t, client, "foo", metav1.NamespaceDefault)

	// Insert the service
	svc := nodePortService("foo", metav1.NamespaceDefault)
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 2)
		require.Equal(r, "foo", actual[0].Service.Service)
		require.Equal(r, "1.2.3.4", actual[0].Service.Address)
		require.Equal(r, 30000, actual[0].Service.Port)
		require.Equal(r, "k8s-sync", actual[0].Node)
		require.Equal(r, "foo", actual[1].Service.Service)
		require.Equal(r, "2.3.4.5", actual[1].Service.Address)
		require.Equal(r, 30000, actual[1].Service.Port)
		require.Equal(r, "k8s-sync", actual[1].Node)
		require.NotEqual(r, actual[0].Service.ID, actual[1].Service.ID)
	})
}

// Test node port works with prefix.
func TestServiceResource_nodePortPrefix(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.NodePortSync = ExternalOnly
	serviceResource.ConsulServicePrefix = "prefix"

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	createNodes(t, client)

	createEndpointSlice(t, client, "foo", metav1.NamespaceDefault)

	// Insert the service
	svc := nodePortService("foo", metav1.NamespaceDefault)
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 2)
		require.Equal(r, "prefixfoo", actual[0].Service.Service)
		require.Equal(r, "1.2.3.4", actual[0].Service.Address)
		require.Equal(r, 30000, actual[0].Service.Port)
		require.Equal(r, "k8s-sync", actual[0].Node)
		require.Equal(r, "prefixfoo", actual[1].Service.Service)
		require.Equal(r, "2.3.4.5", actual[1].Service.Address)
		require.Equal(r, 30000, actual[1].Service.Port)
		require.Equal(r, "k8s-sync", actual[1].Node)
		require.NotEqual(r, actual[0].Service.ID, actual[1].Service.ID)
	})
}

// Test that the registrations for a NodePort type
// are generated only for the nodes where pods are running.
func TestServiceResource_nodePort_singleEndpoint(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.NodePortSync = ExternalOnly

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	node1, _ := createNodes(t, client)

	// Insert the endpoint slice
	_, err := client.DiscoveryV1().EndpointSlices(metav1.NamespaceDefault).Create(
		context.Background(),
		&discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "foo-",
				Labels:       map[string]string{discoveryv1.LabelServiceName: "foo"},
			},
			AddressType: discoveryv1.AddressTypeIPv4,
			Endpoints: []discoveryv1.Endpoint{
				{
					Addresses: []string{"1.2.3.4"},
					Conditions: discoveryv1.EndpointConditions{
						Ready:       ptr.To(true),
						Serving:     ptr.To(true),
						Terminating: ptr.To(false),
					},
					TargetRef: &corev1.ObjectReference{Kind: "pod", Name: "foo", Namespace: metav1.NamespaceDefault},
					NodeName:  &node1.Name,
					Zone:      ptr.To("us-west-2a"),
				},
			},
			Ports: []discoveryv1.EndpointPort{
				{
					Name: ptr.To("http"),
					Port: ptr.To(int32(8080)),
				},
				{
					Name: ptr.To("rpc"),
					Port: ptr.To(int32(2000)),
				},
			},
		},
		metav1.CreateOptions{})
	require.NoError(t, err)

	// Insert the service
	svc := nodePortService("foo", metav1.NamespaceDefault)
	_, err = client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 1)
		require.Equal(r, "foo", actual[0].Service.Service)
		require.Equal(r, "1.2.3.4", actual[0].Service.Address)
		require.Equal(r, 30000, actual[0].Service.Port)
		require.Equal(r, "k8s-sync", actual[0].Node)
	})
}

// Test that the proper registrations are generated for a NodePort with annotated port.
func TestServiceResource_nodePortAnnotatedPort(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.NodePortSync = ExternalOnly

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	createNodes(t, client)

	// Insert the service
	svc := nodePortService("foo", metav1.NamespaceDefault)
	svc.Annotations = map[string]string{annotationServicePort: "rpc"}
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	createEndpointSlice(t, client, svc.Name, metav1.NamespaceDefault)

	require.NoError(t, err)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 2)
		require.Equal(r, "foo", actual[0].Service.Service)
		require.Equal(r, "1.2.3.4", actual[0].Service.Address)
		require.Equal(r, 30001, actual[0].Service.Port)
		require.Equal(r, "k8s-sync", actual[0].Node)
		require.Equal(r, "foo", actual[1].Service.Service)
		require.Equal(r, "2.3.4.5", actual[1].Service.Address)
		require.Equal(r, 30001, actual[1].Service.Port)
		require.Equal(r, "k8s-sync", actual[1].Node)
		require.NotEqual(r, actual[0].Service.ID, actual[1].Service.ID)
	})
}

// Test that the proper registrations are generated for a NodePort with an unnamed port.
func TestServiceResource_nodePortUnnamedPort(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.NodePortSync = ExternalOnly

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	createNodes(t, client)

	createEndpointSlice(t, client, "foo", metav1.NamespaceDefault)

	// Insert the service
	svc := nodePortService("foo", metav1.NamespaceDefault)
	// Override service ports
	svc.Spec.Ports = []corev1.ServicePort{
		{Port: 80, TargetPort: intstr.FromInt(8080), NodePort: 30000},
		{Port: 8500, TargetPort: intstr.FromInt(2000), NodePort: 30001},
	}
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 2)
		require.Equal(r, "foo", actual[0].Service.Service)
		require.Equal(r, "1.2.3.4", actual[0].Service.Address)
		require.Equal(r, 30000, actual[0].Service.Port)
		require.Equal(r, "k8s-sync", actual[0].Node)
		require.Equal(r, "foo", actual[1].Service.Service)
		require.Equal(r, "2.3.4.5", actual[1].Service.Address)
		require.Equal(r, 30000, actual[1].Service.Port)
		require.Equal(r, "k8s-sync", actual[1].Node)
		require.NotEqual(r, actual[0].Service.ID, actual[1].Service.ID)
	})
}

// Test that the proper registrations are generated for a NodePort type
// when syncing internal Node IPs only.
func TestServiceResource_nodePort_internalOnlySync(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.NodePortSync = InternalOnly

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	createNodes(t, client)

	createEndpointSlice(t, client, "foo", metav1.NamespaceDefault)

	// Insert the service
	svc := nodePortService("foo", metav1.NamespaceDefault)
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 2)
		require.Equal(r, "foo", actual[0].Service.Service)
		require.Equal(r, "4.5.6.7", actual[0].Service.Address)
		require.Equal(r, 30000, actual[0].Service.Port)
		require.Equal(r, "k8s-sync", actual[0].Node)
		require.Equal(r, "foo", actual[1].Service.Service)
		require.Equal(r, "3.4.5.6", actual[1].Service.Address)
		require.Equal(r, 30000, actual[1].Service.Port)
		require.Equal(r, "k8s-sync", actual[1].Node)
		require.NotEqual(r, actual[0].Service.ID, actual[1].Service.ID)
	})
}

// Test that the proper registrations are generated for a NodePort type
// when preferring to sync external Node IPs over internal IPs.
func TestServiceResource_nodePort_externalFirstSync(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.NodePortSync = ExternalFirst

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	node1, _ := createNodes(t, client)

	node1.Status = corev1.NodeStatus{
		Addresses: []corev1.NodeAddress{
			{Type: corev1.NodeInternalIP, Address: "4.5.6.7"},
		},
	}
	_, err := client.CoreV1().Nodes().UpdateStatus(context.Background(), node1, metav1.UpdateOptions{})
	require.NoError(t, err)

	createEndpointSlice(t, client, "foo", metav1.NamespaceDefault)

	// Insert the service
	svc := nodePortService("foo", metav1.NamespaceDefault)
	_, err = client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 2)
		require.Equal(r, "foo", actual[0].Service.Service)
		require.Equal(r, "4.5.6.7", actual[0].Service.Address)
		require.Equal(r, 30000, actual[0].Service.Port)
		require.Equal(r, "k8s-sync", actual[0].Node)
		require.Equal(r, "foo", actual[1].Service.Service)
		require.Equal(r, "2.3.4.5", actual[1].Service.Address)
		require.Equal(r, 30000, actual[1].Service.Port)
		require.Equal(r, "k8s-sync", actual[1].Node)
		require.NotEqual(r, actual[0].Service.ID, actual[1].Service.ID)
	})
}

// Test that the proper registrations are generated for a ClusterIP type.
func TestServiceResource_clusterIP(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.ClusterIPSync = true

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert the service
	svc := clusterIPService("foo", metav1.NamespaceDefault)
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	createNodes(t, client)

	// Insert the endpoint slice
	createEndpointSlice(t, client, "foo", metav1.NamespaceDefault)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 2)
		require.Equal(r, "foo", actual[0].Service.Service)
		require.Equal(r, "1.1.1.1", actual[0].Service.Address)
		require.Equal(r, 8080, actual[0].Service.Port)
		require.Equal(r, "foo", actual[1].Service.Service)
		require.Equal(r, "2.2.2.2", actual[1].Service.Address)
		require.Equal(r, 8080, actual[1].Service.Port)
		require.Equal(r, "us-west-2a", actual[0].Service.Meta["external-k8s-topology-zone"])
		require.Equal(r, "us-west-2b", actual[1].Service.Meta["external-k8s-topology-zone"])
		require.NotEqual(r, actual[0].Service.ID, actual[1].Service.ID)
	})
}

// Test that the proper registrations with health checks are generated for a ClusterIP type.
func TestServiceResource_clusterIP_healthCheck(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.ClusterIPSync = true

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert the service
	svc := clusterIPService("foo", metav1.NamespaceDefault)
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	createNodes(t, client)

	// Insert the endpoint slice
	createEndpointSlice(t, client, "foo", metav1.NamespaceDefault)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 2)
		require.Equal(r, consulKubernetesCheckName, actual[0].Check.Name)
		require.Equal(r, consulapi.HealthPassing, actual[0].Check.Status)
		require.Equal(r, kubernetesSuccessReasonMsg, actual[0].Check.Output)
		require.Equal(r, consulKubernetesCheckType, actual[0].Check.Type)
		require.Equal(r, consulKubernetesCheckName, actual[1].Check.Name)
		require.Equal(r, consulapi.HealthPassing, actual[1].Check.Status)
		require.Equal(r, kubernetesSuccessReasonMsg, actual[1].Check.Output)
		require.Equal(r, consulKubernetesCheckType, actual[1].Check.Type)
	})
}

// Test clusterIP with prefix.
func TestServiceResource_clusterIPPrefix(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.ClusterIPSync = true
	serviceResource.ConsulServicePrefix = "prefix"

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert the service
	svc := clusterIPService("foo", metav1.NamespaceDefault)
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	createNodes(t, client)

	// Insert the endpoint slice
	createEndpointSlice(t, client, "foo", metav1.NamespaceDefault)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 2)
		require.Equal(r, "prefixfoo", actual[0].Service.Service)
		require.Equal(r, "1.1.1.1", actual[0].Service.Address)
		require.Equal(r, 8080, actual[0].Service.Port)
		require.Equal(r, "prefixfoo", actual[1].Service.Service)
		require.Equal(r, "2.2.2.2", actual[1].Service.Address)
		require.Equal(r, 8080, actual[1].Service.Port)
		require.NotEqual(r, actual[0].Service.ID, actual[1].Service.ID)
	})
}

// Test that the proper registrations are generated for a ClusterIP type with
// annotated port name override.
func TestServiceResource_clusterIPAnnotatedPortName(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.ClusterIPSync = true

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert the service
	svc := clusterIPService("foo", metav1.NamespaceDefault)
	svc.Annotations[annotationServicePort] = "rpc"
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	createNodes(t, client)

	// Insert the endpoint slice
	createEndpointSlice(t, client, "foo", metav1.NamespaceDefault)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 2)
		require.Equal(r, "foo", actual[0].Service.Service)
		require.Equal(r, "1.1.1.1", actual[0].Service.Address)
		require.Equal(r, 2000, actual[0].Service.Port)
		require.Equal(r, "foo", actual[1].Service.Service)
		require.Equal(r, "2.2.2.2", actual[1].Service.Address)
		require.Equal(r, 2000, actual[1].Service.Port)
		require.NotEqual(r, actual[0].Service.ID, actual[1].Service.ID)
	})
}

// Test that the proper registrations are generated for a ClusterIP type with
// annotated port number override.
func TestServiceResource_clusterIPAnnotatedPortNumber(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.ClusterIPSync = true

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert the service
	svc := clusterIPService("foo", metav1.NamespaceDefault)
	svc.Annotations[annotationServicePort] = "4141"
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	createNodes(t, client)

	// Insert the endpoint slice
	createEndpointSlice(t, client, "foo", metav1.NamespaceDefault)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 2)
		require.Equal(r, "foo", actual[0].Service.Service)
		require.Equal(r, "1.1.1.1", actual[0].Service.Address)
		require.Equal(r, 4141, actual[0].Service.Port)
		require.Equal(r, "foo", actual[1].Service.Service)
		require.Equal(r, "2.2.2.2", actual[1].Service.Address)
		require.Equal(r, 4141, actual[1].Service.Port)
		require.NotEqual(r, actual[0].Service.ID, actual[1].Service.ID)
	})
}

// Test that the proper registrations are generated for a ClusterIP type with unnamed ports.
func TestServiceResource_clusterIPUnnamedPorts(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.ClusterIPSync = true

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert the service
	svc := clusterIPService("foo", metav1.NamespaceDefault)
	svc.Spec.Ports = []corev1.ServicePort{
		{Port: 80, TargetPort: intstr.FromInt(8080)},
		{Port: 8500, TargetPort: intstr.FromInt(2000)},
	}
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	createNodes(t, client)

	// Insert the endpoint slice
	createEndpointSlice(t, client, "foo", metav1.NamespaceDefault)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 2)
		require.Equal(r, "foo", actual[0].Service.Service)
		require.Equal(r, "1.1.1.1", actual[0].Service.Address)
		require.Equal(r, 8080, actual[0].Service.Port)
		require.Equal(r, "foo", actual[1].Service.Service)
		require.Equal(r, "2.2.2.2", actual[1].Service.Address)
		require.Equal(r, 8080, actual[1].Service.Port)
		require.NotEqual(r, actual[0].Service.ID, actual[1].Service.ID)
	})
}

// Test that the ClusterIP services aren't synced when ClusterIPSync
// is disabled.
func TestServiceResource_clusterIPSyncDisabled(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.ClusterIPSync = false

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert the service
	svc := clusterIPService("foo", metav1.NamespaceDefault)
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	createNodes(t, client)

	// Insert the endpoint slice
	createEndpointSlice(t, client, "foo", metav1.NamespaceDefault)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 0)
	})
}

// Test that the ClusterIP services are synced when watching all namespaces.
func TestServiceResource_clusterIPAllNamespaces(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	testNamespace := "test_namespace"
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.ClusterIPSync = true

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert the service
	svc := clusterIPService("foo", testNamespace)
	_, err := client.CoreV1().Services(testNamespace).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	createNodes(t, client)

	// Insert the endpoint slice
	createEndpointSlice(t, client, "foo", testNamespace)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 2)
		require.Equal(r, "foo", actual[0].Service.Service)
		require.Equal(r, "1.1.1.1", actual[0].Service.Address)
		require.Equal(r, 8080, actual[0].Service.Port)
		require.Equal(r, "foo", actual[1].Service.Service)
		require.Equal(r, "2.2.2.2", actual[1].Service.Address)
		require.Equal(r, 8080, actual[1].Service.Port)
		require.NotEqual(r, actual[0].Service.ID, actual[1].Service.ID)
	})
}

// Test using a port name annotation when the targetPort is a named port.
func TestServiceResource_clusterIPTargetPortNamed(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.ClusterIPSync = true

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert the service
	svc := clusterIPService("foo", metav1.NamespaceDefault)
	svc.Annotations[annotationServicePort] = "rpc"
	svc.Spec.Ports = []corev1.ServicePort{
		{Port: 80, TargetPort: intstr.FromString("httpPort"), Name: "http"},
		{Port: 8500, TargetPort: intstr.FromString("rpcPort"), Name: "rpc"},
	}
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	createNodes(t, client)

	// Insert the endpoint slice
	createEndpointSlice(t, client, "foo", metav1.NamespaceDefault)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 2)
		require.Equal(r, "foo", actual[0].Service.Service)
		require.Equal(r, "1.1.1.1", actual[0].Service.Address)
		require.Equal(r, 2000, actual[0].Service.Port)
		require.Equal(r, "foo", actual[1].Service.Service)
		require.Equal(r, "2.2.2.2", actual[1].Service.Address)
		require.Equal(r, 2000, actual[1].Service.Port)
		require.NotEqual(r, actual[0].Service.ID, actual[1].Service.ID)
	})
}

// Test target information are set in service meta.
func TestServiceResource_targetRefInMeta(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.ClusterIPSync = true

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert the service
	svc := clusterIPService("foo", metav1.NamespaceDefault)
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)

	createNodes(t, client)

	// Insert the endpoint slice
	createEndpointSlice(t, client, "foo", metav1.NamespaceDefault)

	// Verify what we got
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 2)
		require.Equal(r, "foobar", actual[0].Service.Meta[ConsulK8SRefValue])
		require.Equal(r, "pod", actual[0].Service.Meta[ConsulK8SRefKind])
		require.Equal(r, nodeName1, actual[0].Service.Meta[ConsulK8SNodeName])

		// For second service, there is a node but no target refs
		require.Equal(r, nodeName2, actual[1].Service.Meta[ConsulK8SNodeName])
		require.NotContains(r, actual[1].Service.Meta, ConsulK8SRefValue)
		require.NotContains(r, actual[1].Service.Meta, ConsulK8SRefKind)
	})
}

// Test allow/deny namespace lists.
func TestServiceResource_AllowDenyNamespaces(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		AllowList     mapset.Set
		DenyList      mapset.Set
		ExpNamespaces []string
	}{
		"empty lists": {
			AllowList:     mapset.NewSet(),
			DenyList:      mapset.NewSet(),
			ExpNamespaces: nil,
		},
		"only from allow list": {
			AllowList:     mapset.NewSet("foo"),
			DenyList:      mapset.NewSet(),
			ExpNamespaces: []string{"foo"},
		},
		"both in allow and deny": {
			AllowList:     mapset.NewSet("foo"),
			DenyList:      mapset.NewSet("foo"),
			ExpNamespaces: nil,
		},
		"deny removes one from allow": {
			AllowList:     mapset.NewSet("foo", "bar"),
			DenyList:      mapset.NewSet("foo"),
			ExpNamespaces: []string{"bar"},
		},
		"* in allow": {
			AllowList:     mapset.NewSet("*"),
			DenyList:      mapset.NewSet(),
			ExpNamespaces: []string{"foo", "bar"},
		},
		"* in allow with one denied": {
			AllowList:     mapset.NewSet("*"),
			DenyList:      mapset.NewSet("bar"),
			ExpNamespaces: []string{"foo"},
		},
	}

	for name, c := range cases {
		t.Run(name, func(tt *testing.T) {
			client := fake.NewSimpleClientset()
			syncer := newTestSyncer()
			serviceResource := defaultServiceResource(client, syncer)
			serviceResource.AllowK8sNamespacesSet = c.AllowList
			serviceResource.DenyK8sNamespacesSet = c.DenyList

			// Start the controller
			closer := controller.TestControllerRun(&serviceResource)
			defer closer()

			// We always have two services in two namespaces: foo and bar.
			// Each service has the same name as its origin k8s namespace which
			// we use for asserting that the right namespace got synced.
			for _, ns := range []string{"foo", "bar"} {
				_, err := client.CoreV1().Services(ns).Create(context.Background(), lbService(ns, ns, "1.2.3.4"), metav1.CreateOptions{})
				require.NoError(tt, err)
			}

			// Test we got registrations from the expected namespaces.
			retry.Run(tt, func(r *retry.R) {
				syncer.Lock()
				defer syncer.Unlock()
				actual := syncer.Registrations
				require.Len(r, actual, len(c.ExpNamespaces))
			})

			syncer.Lock()
			defer syncer.Unlock()
			for _, expNS := range c.ExpNamespaces {
				found := false
				for _, reg := range syncer.Registrations {
					// The service names are the same as their k8s destination
					// namespaces so we can that to ensure the services were
					// synced from the expected namespaces.
					if reg.Service.Service == expNS {
						found = true
					}
				}
				require.True(tt, found, "did not find service from ns %s", expNS)
			}
		})
	}
}

// Test that services are synced to the correct destination ns
// when a single destination namespace is set.
func TestServiceResource_singleDestNamespace(t *testing.T) {
	t.Parallel()
	consulDestNamespaces := []string{"default", "dest"}
	for _, consulDestNamespace := range consulDestNamespaces {
		t.Run(consulDestNamespace, func(tt *testing.T) {
			client := fake.NewSimpleClientset()
			syncer := newTestSyncer()
			serviceResource := defaultServiceResource(client, syncer)
			serviceResource.ConsulDestinationNamespace = consulDestNamespace
			serviceResource.EnableNamespaces = true
			closer := controller.TestControllerRun(&serviceResource)
			defer closer()
			_, err := client.CoreV1().Services(metav1.NamespaceDefault).
				Create(context.Background(), lbService("foo", metav1.NamespaceDefault, "1.2.3.4"), metav1.CreateOptions{})
			require.NoError(tt, err)

			retry.Run(tt, func(r *retry.R) {
				syncer.Lock()
				defer syncer.Unlock()
				actual := syncer.Registrations
				require.Len(r, actual, 1)
				require.Equal(r, consulDestNamespace, actual[0].Service.Namespace)
			})
		})
	}
}

// Test that services are created in a mirrored namespace.
func TestServiceResource_MirroredNamespace(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.EnableK8SNSMirroring = true
	serviceResource.EnableNamespaces = true
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	k8sNamespaces := []string{"foo", "bar", "default"}
	for _, ns := range k8sNamespaces {
		_, err := client.CoreV1().Services(ns).
			Create(context.Background(), lbService(ns, ns, "1.2.3.4"), metav1.CreateOptions{})
		require.NoError(t, err)
	}

	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 3)
		for _, expNS := range k8sNamespaces {
			found := false
			for _, reg := range actual {
				if reg.Service.Namespace == expNS {
					found = true
				}
			}
			require.True(r, found, "did not find registration from ns %s", expNS)
		}
	})
}

// Test that services are created in a mirrored namespace with prefix.
func TestServiceResource_MirroredPrefixNamespace(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := newTestSyncer()
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.EnableK8SNSMirroring = true
	serviceResource.EnableNamespaces = true
	serviceResource.K8SNSMirroringPrefix = "prefix-"
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	k8sNamespaces := []string{"foo", "bar", "default"}
	for _, ns := range k8sNamespaces {
		_, err := client.CoreV1().Services(ns).
			Create(context.Background(), lbService(ns, ns, "1.2.3.4"), metav1.CreateOptions{})
		require.NoError(t, err)
	}

	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 3)
		for _, expNS := range k8sNamespaces {
			found := false
			for _, reg := range actual {
				if reg.Service.Namespace == "prefix-"+expNS {
					found = true
				}
			}
			require.True(r, found, "did not find registration from ns %s", expNS)
		}
	})
}

// Test k8s namespace suffix is not appended
// when the service name annotation is provided.
func TestServiceResource_addIngress(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		enableIngress     bool
		syncIngressIP     bool
		ingress           *networkingv1.Ingress
		expectIngressSync bool
		expectedAddress   string
		expectedPort      int
	}{
		"enable ingress on port 80": {
			enableIngress: true,
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ingress",
				},
				Spec: networkingv1.IngressSpec{
					TLS: []networkingv1.IngressTLS{
						{
							Hosts:      []string{"test.other.consul"},
							SecretName: "test-other-tls-secret",
						},
					},
					Rules: []networkingv1.IngressRule{
						{
							Host: "test.host.consul",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path: "/",
											Backend: networkingv1.IngressBackend{
												Service: &networkingv1.IngressServiceBackend{
													Name: "test-service",
													Port: networkingv1.ServiceBackendPort{
														Number: 8080,
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expectIngressSync: true,
			expectedAddress:   "test.host.consul",
			expectedPort:      80,
		},
		"enable ingress on port 443": {
			enableIngress: true,
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ingress",
				},
				Spec: networkingv1.IngressSpec{
					TLS: []networkingv1.IngressTLS{
						{
							Hosts:      []string{"test.host.consul"},
							SecretName: "test-tls-secret",
						},
					},
					Rules: []networkingv1.IngressRule{
						{
							Host: "test.host.consul",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path: "/",
											Backend: networkingv1.IngressBackend{
												Service: &networkingv1.IngressServiceBackend{
													Name: "test-service",
													Port: networkingv1.ServiceBackendPort{
														Number: 8080,
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expectIngressSync: true,
			expectedAddress:   "test.host.consul",
			expectedPort:      443,
		},
		"enable ingress on port 80 with loadbalancer IP": {
			enableIngress: true,
			syncIngressIP: true,
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ingress",
				},
				Spec: networkingv1.IngressSpec{
					TLS: []networkingv1.IngressTLS{
						{
							Hosts:      []string{"test.other.consul"},
							SecretName: "test-other-tls-secret",
						},
					},
					Rules: []networkingv1.IngressRule{
						{
							Host: "test.host.consul",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path: "/",
											Backend: networkingv1.IngressBackend{
												Service: &networkingv1.IngressServiceBackend{
													Name: "test-service",
													Port: networkingv1.ServiceBackendPort{
														Number: 8080,
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				Status: networkingv1.IngressStatus{
					LoadBalancer: networkingv1.IngressLoadBalancerStatus{
						Ingress: []networkingv1.IngressLoadBalancerIngress{{IP: "1.2.3.4"}},
					},
				},
			},
			expectIngressSync: true,
			expectedAddress:   "1.2.3.4",
			expectedPort:      80,
		},
		"enable ingress on port 443 with loadbalancer IP": {
			enableIngress: true,
			syncIngressIP: true,
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ingress",
				},
				Spec: networkingv1.IngressSpec{
					TLS: []networkingv1.IngressTLS{
						{
							Hosts:      []string{"test.host.consul"},
							SecretName: "test-tls-secret",
						},
					},
					Rules: []networkingv1.IngressRule{
						{
							Host: "test.host.consul",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path: "/",
											Backend: networkingv1.IngressBackend{
												Service: &networkingv1.IngressServiceBackend{
													Name: "test-service",
													Port: networkingv1.ServiceBackendPort{
														Number: 8080,
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				Status: networkingv1.IngressStatus{
					LoadBalancer: networkingv1.IngressLoadBalancerStatus{
						Ingress: []networkingv1.IngressLoadBalancerIngress{{IP: "1.2.3.4"}},
					},
				},
			},
			expectIngressSync: true,
			expectedAddress:   "1.2.3.4",
			expectedPort:      443,
		},
		"ingress disabled": {
			enableIngress: false,
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ingress",
				},
				Spec: networkingv1.IngressSpec{
					Rules: []networkingv1.IngressRule{
						{
							Host: "test.host.consul",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path: "/",
											Backend: networkingv1.IngressBackend{
												Service: &networkingv1.IngressServiceBackend{
													Name: "test-service",
													Port: networkingv1.ServiceBackendPort{
														Number: 8080,
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expectIngressSync: false,
			expectedAddress:   "1.1.1.1",
			expectedPort:      8080,
		},
		"ignores ingress if host != /": {
			enableIngress: true,
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ingress",
				},
				Spec: networkingv1.IngressSpec{
					Rules: []networkingv1.IngressRule{
						{
							Host: "test.host.consul",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path: "/foo",
											Backend: networkingv1.IngressBackend{
												Service: &networkingv1.IngressServiceBackend{
													Name: "test-service",
													Port: networkingv1.ServiceBackendPort{
														Number: 8080,
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expectIngressSync: false,
			expectedAddress:   "1.1.1.1",
			expectedPort:      8080,
		},
	}

	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			syncer := newTestSyncer()
			serviceResource := defaultServiceResource(client, syncer)
			serviceResource.ClusterIPSync = true
			serviceResource.EnableIngress = test.enableIngress
			serviceResource.SyncLoadBalancerIPs = test.syncIngressIP

			// Start the controller
			closer := controller.TestControllerRun(&serviceResource)
			defer closer()

			// Create the service
			_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), clusterIPService("test-service", metav1.NamespaceDefault), metav1.CreateOptions{})
			require.NoError(t, err)
			// Create the ingress
			_, err = client.NetworkingV1().Ingresses(metav1.NamespaceDefault).Create(context.Background(), test.ingress, metav1.CreateOptions{})
			require.NoError(t, err)

			createNodes(t, client)
			createEndpointSlice(t, client, "test-service", metav1.NamespaceDefault)

			// Verify that the service name annotation is preferred
			retry.Run(t, func(r *retry.R) {
				syncer.Lock()
				defer syncer.Unlock()
				actual := syncer.Registrations
				if test.expectIngressSync {
					require.Len(r, actual, 1)
					require.Equal(r, test.expectedAddress, actual[0].Service.Address)
					require.Equal(r, test.expectedPort, actual[0].Service.Port)
				} else {
					require.Len(r, actual, 2)
					require.Equal(r, test.expectedAddress, actual[0].Service.Address)
					require.Equal(r, test.expectedPort, actual[0].Service.Port)
				}

			})
		})
	}
}

// lbService returns a Kubernetes service of type LoadBalancer.
func lbService(name, namespace, lbIP string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: map[string]string{},
		},

		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
		},

		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{
					{
						IP: lbIP,
					},
				},
			},
		},
	}
}

// nodePortService returns a Kubernetes service of type NodePort.
func nodePortService(name, namespace string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},

		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeNodePort,
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080), NodePort: 30000},
				{Name: "rpc", Port: 8500, TargetPort: intstr.FromInt(2000), NodePort: 30001},
			},
		},
	}
}

// clusterIPService returns a Kubernetes service of type ClusterIP.
func clusterIPService(name, namespace string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: map[string]string{},
		},

		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080)},
				{Name: "rpc", Port: 8500, TargetPort: intstr.FromInt(2000)},
			},
		},
	}
}

// createNodes calls the fake k8s client to create two Kubernetes nodes and returns them.
func createNodes(t *testing.T, client *fake.Clientset) (*corev1.Node, *corev1.Node) {
	// Insert the nodes
	node1 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName1,
		},

		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeExternalIP, Address: "1.2.3.4"},
				{Type: corev1.NodeInternalIP, Address: "4.5.6.7"},
				{Type: corev1.NodeInternalIP, Address: "7.8.9.10"},
			},
		},
	}
	_, err := client.CoreV1().Nodes().Create(context.Background(), node1, metav1.CreateOptions{})
	require.NoError(t, err)

	node2 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName2,
		},

		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeExternalIP, Address: "2.3.4.5"},
				{Type: corev1.NodeInternalIP, Address: "3.4.5.6"},
				{Type: corev1.NodeInternalIP, Address: "6.7.8.9"},
			},
		},
	}
	_, err = client.CoreV1().Nodes().Create(context.Background(), node2, metav1.CreateOptions{})
	require.NoError(t, err)

	return node1, node2
}

// createEndpointSlices calls the fake k8s client to create an endpoint slices with two endpoints on different nodes.
func createEndpointSlice(t *testing.T, client *fake.Clientset, serviceName string, namespace string) {
	node1 := nodeName1
	node2 := nodeName2
	targetRef := corev1.ObjectReference{Kind: "pod", Name: "foobar"}

	_, err := client.DiscoveryV1().EndpointSlices(namespace).Create(
		context.Background(),
		&discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{discoveryv1.LabelServiceName: serviceName},
				Name:   serviceName + "-" + rand.String(5),
			},
			AddressType: discoveryv1.AddressTypeIPv4,
			Endpoints: []discoveryv1.Endpoint{
				{
					Addresses: []string{"1.1.1.1"},
					Conditions: discoveryv1.EndpointConditions{
						Ready:       ptr.To(true),
						Serving:     ptr.To(true),
						Terminating: ptr.To(false),
					},
					TargetRef: &targetRef,
					NodeName:  &node1,
					Zone:      ptr.To("us-west-2a"),
				},
				{
					Addresses: []string{"2.2.2.2"},
					Conditions: discoveryv1.EndpointConditions{
						Ready:       ptr.To(true),
						Serving:     ptr.To(true),
						Terminating: ptr.To(false),
					},
					NodeName: &node2,
					Zone:     ptr.To("us-west-2b"),
				},
			},
			Ports: []discoveryv1.EndpointPort{
				{
					Name: ptr.To("http"),
					Port: ptr.To(int32(8080)),
				},
				{
					Name: ptr.To("rpc"),
					Port: ptr.To(int32(2000)),
				},
			},
		},
		metav1.CreateOptions{})
	require.NoError(t, err)
}

func defaultServiceResource(client kubernetes.Interface, syncer Syncer) ServiceResource {
	return ServiceResource{
		Log:                   hclog.Default(),
		Client:                client,
		Syncer:                syncer,
		Ctx:                   context.Background(),
		AllowK8sNamespacesSet: mapset.NewSet("*"),
		DenyK8sNamespacesSet:  mapset.NewSet(),
		ConsulNodeName:        ConsulSyncNodeName,
	}
}
