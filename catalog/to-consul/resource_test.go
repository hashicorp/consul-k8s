package catalog

import (
	"testing"
	"time"

	"github.com/deckarep/golang-set"
	"github.com/hashicorp/consul-k8s/helper/controller"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

const nodeName1 = "ip-10-11-12-13.ec2.internal"
const nodeName2 = "ip-10-11-12-14.ec2.internal"

func init() {
	hclog.DefaultOptions.Level = hclog.Debug
}

func TestServiceResource_impl(t *testing.T) {
	var _ controller.Resource = &ServiceResource{}
	var _ controller.Backgrounder = &ServiceResource{}
}

// Test that deleting a service properly deletes the registration.
func TestServiceResource_createDelete(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}
	serviceResource := defaultServiceResource(client, syncer)

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(svc)
	require.NoError(err)

	// Delete
	require.NoError(client.CoreV1().Services(metav1.NamespaceDefault).Delete("foo", nil))
	time.Sleep(300 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 0)
}

// Test that we're default enabled.
func TestServiceResource_defaultEnable(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}
	serviceResource := defaultServiceResource(client, syncer)

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(svc)
	require.NoError(err)
	time.Sleep(200 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 1)
}

// Test that we can explicitly disable.
func TestServiceResource_defaultEnableDisable(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}
	serviceResource := defaultServiceResource(client, syncer)

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	svc.Annotations[annotationServiceSync] = "false"
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(svc)
	require.NoError(err)
	time.Sleep(200 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 0)
}

// Test that we can default disable
func TestServiceResource_defaultDisable(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.ExplicitEnable = true

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(svc)
	require.NoError(err)
	time.Sleep(200 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 0)
}

// Test that we can default disable but override
func TestServiceResource_defaultDisableEnable(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.ExplicitEnable = true

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	svc.Annotations[annotationServiceSync] = "t"
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(svc)
	require.NoError(err)
	time.Sleep(200 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 1)
}

// // Test that system resources are not synced by default.
// func TestServiceResource_system(t *testing.T) {
// 	t.Parallel()
// 	require := require.New(t)
// 	client := fake.NewSimpleClientset()
// 	syncer := &TestSyncer{}
// 	serviceResource := defaultServiceResource(client, syncer)

// 	// Start the controller
// 	closer := controller.TestControllerRun(&serviceResource)
// 	defer closer()

// 	// Insert an LB service
// 	svc := lbService("foo", metav1.NamespaceSystem, "1.2.3.4")
// 	_, err := client.CoreV1().Services(metav1.NamespaceSystem).Create(svc)
// 	require.NoError(err)
// 	time.Sleep(200 * time.Millisecond)

// 	// Verify what we got
// 	syncer.Lock()
// 	defer syncer.Unlock()
// 	actual := syncer.Registrations
// 	require.Len(actual, 0)
// }

// Test changing the sync tag to false deletes the service.
func TestServiceResource_changeSyncToFalse(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.ExplicitEnable = true

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service with the sync=true
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	svc.Annotations[annotationServiceSync] = "true"
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(svc)
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
	_, err = client.CoreV1().Services(metav1.NamespaceDefault).Update(svc)
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
// when AddK8SNamespaceSuffix is true
func TestServiceResource_addK8SNamespace(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.AddK8SNamespaceSuffix = true

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service with the sync=true
	svc := lbService("foo", "namespace", "1.2.3.4")
	_, err := client.CoreV1().Services("namespace").Create(svc)
	require.NoError(t, err)

	// Verify that the service name has k8s namespace appended with an '-'
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 1)
		require.Equal(t, actual[0].Service.Service, "foo-namespace")
	})
}

// Test k8s namespace suffix is appended
// when the consul prefix is provided
func TestServiceResource_addK8SNamespaceWithPrefix(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.AddK8SNamespaceSuffix = true
	serviceResource.ConsulServicePrefix = "prefix"

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service with the sync=true
	svc := lbService("foo", "namespace", "1.2.3.4")
	_, err := client.CoreV1().Services("namespace").Create(svc)
	require.NoError(t, err)

	// Verify that the service name has k8s namespace appended with an '-'
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 1)
		require.Equal(t, actual[0].Service.Service, "prefixfoo-namespace")
	})
}

// Test k8s namespace suffix is not appended
// when the service name annotation is provided
func TestServiceResource_addK8SNamespaceWithNameAnnotation(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.AddK8SNamespaceSuffix = true

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service with the sync=true
	svc := lbService("foo", "bar", "1.2.3.4")
	svc.Annotations[annotationServiceName] = "different-service-name"
	_, err := client.CoreV1().Services("bar").Create(svc)
	require.NoError(t, err)

	// Verify that the service name annotation is preferred
	retry.Run(t, func(r *retry.R) {
		syncer.Lock()
		defer syncer.Unlock()
		actual := syncer.Registrations
		require.Len(r, actual, 1)
		require.Equal(t, actual[0].Service.Service, "different-service-name")
	})
}

// Test that external IPs take priority.
func TestServiceResource_externalIP(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}
	serviceResource := defaultServiceResource(client, syncer)

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	svc.Spec.ExternalIPs = []string{"3.3.3.3", "4.4.4.4"}
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(svc)
	require.NoError(err)

	// Wait a bit
	time.Sleep(500 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 2)
	require.Equal("foo", actual[0].Service.Service)
	require.Equal("3.3.3.3", actual[0].Service.Address)
	require.Equal("foo", actual[1].Service.Service)
	require.Equal("4.4.4.4", actual[1].Service.Address)
}

// Test externalIP with Prefix
func TestServiceResource_externalIPPrefix(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.ConsulServicePrefix = "prefix"

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	svc.Spec.ExternalIPs = []string{"3.3.3.3", "4.4.4.4"}
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(svc)
	require.NoError(err)

	// Wait a bit
	time.Sleep(500 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 2)
	require.Equal("prefixfoo", actual[0].Service.Service)
	require.Equal("3.3.3.3", actual[0].Service.Address)
	require.Equal("prefixfoo", actual[1].Service.Service)
	require.Equal("4.4.4.4", actual[1].Service.Address)
}

// Test that the proper registrations are generated for a LoadBalancer.
func TestServiceResource_lb(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}
	serviceResource := defaultServiceResource(client, syncer)

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(svc)
	require.NoError(err)

	// Wait a bit
	time.Sleep(500 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 1)
	require.Equal("foo", actual[0].Service.Service)
	require.Equal("1.2.3.4", actual[0].Service.Address)
}

// Test that the proper registrations are generated for a LoadBalancer with a prefix
func TestServiceResource_lbPrefix(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.ConsulServicePrefix = "prefix"

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(svc)
	require.NoError(err)

	// Wait a bit
	time.Sleep(500 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 1)
	require.Equal("prefixfoo", actual[0].Service.Service)
	require.Equal("1.2.3.4", actual[0].Service.Address)
}

// Test that the proper registrations are generated for a LoadBalancer
// with multiple endpoints.
func TestServiceResource_lbMultiEndpoint(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}
	serviceResource := defaultServiceResource(client, syncer)

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	svc.Status.LoadBalancer.Ingress = append(
		svc.Status.LoadBalancer.Ingress,
		apiv1.LoadBalancerIngress{IP: "2.3.4.5"},
	)
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(svc)
	require.NoError(err)

	// Wait a bit
	time.Sleep(500 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 2)
	require.Equal("foo", actual[0].Service.Service)
	require.Equal("1.2.3.4", actual[0].Service.Address)
	require.Equal("foo", actual[1].Service.Service)
	require.Equal("2.3.4.5", actual[1].Service.Address)
	require.NotEqual(actual[1].Service.ID, actual[0].Service.ID)
}

// Test explicit name annotation
func TestServiceResource_lbAnnotatedName(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}
	serviceResource := defaultServiceResource(client, syncer)

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	svc.Annotations[annotationServiceName] = "bar"
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(svc)
	require.NoError(err)
	time.Sleep(300 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 1)
	require.Equal("bar", actual[0].Service.Service)
}

// Test default port and additional ports in the meta
func TestServiceResource_lbPort(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}
	serviceResource := defaultServiceResource(client, syncer)

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	svc.Spec.Ports = []apiv1.ServicePort{
		{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080)},
		{Name: "rpc", Port: 8500, TargetPort: intstr.FromInt(2000)},
	}
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(svc)
	require.NoError(err)
	time.Sleep(300 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 1)
	require.Equal(80, actual[0].Service.Port)
	require.Equal("80", actual[0].Service.Meta["port-http"])
	require.Equal("8500", actual[0].Service.Meta["port-rpc"])
}

// Test default port works with override annotation
func TestServiceResource_lbAnnotatedPort(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}
	serviceResource := defaultServiceResource(client, syncer)

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	svc.Annotations[annotationServicePort] = "rpc"
	svc.Spec.Ports = []apiv1.ServicePort{
		{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080)},
		{Name: "rpc", Port: 8500, TargetPort: intstr.FromInt(2000)},
	}
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(svc)
	require.NoError(err)
	time.Sleep(300 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 1)
	require.Equal(8500, actual[0].Service.Port)
	require.Equal("80", actual[0].Service.Meta["port-http"])
	require.Equal("8500", actual[0].Service.Meta["port-rpc"])
}

// Test annotated tags
func TestServiceResource_lbAnnotatedTags(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.ConsulK8STag = TestConsulK8STag

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	svc.Annotations[annotationServiceTags] = "one, two,three"
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(svc)
	require.NoError(err)
	time.Sleep(300 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 1)
	require.Equal([]string{"k8s", "one", "two", "three"}, actual[0].Service.Tags)
}

// Test annotated service meta
func TestServiceResource_lbAnnotatedMeta(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}
	serviceResource := defaultServiceResource(client, syncer)

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert an LB service
	svc := lbService("foo", metav1.NamespaceDefault, "1.2.3.4")
	svc.Annotations[annotationServiceMetaPrefix+"foo"] = "bar"
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(svc)
	require.NoError(err)
	time.Sleep(300 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 1)
	require.Equal("bar", actual[0].Service.Meta["foo"])
}

// Test that the proper registrations are generated for a NodePort type.
func TestServiceResource_nodePort(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	syncer := &TestSyncer{}
	client := fake.NewSimpleClientset()
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.NodePortSync = ExternalOnly

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	createNodes(t, client)

	createEndpoints(t, client, "foo", metav1.NamespaceDefault)

	// Insert the service
	svc := nodePortService("foo", metav1.NamespaceDefault)
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(svc)
	require.NoError(err)

	time.Sleep(200 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 2)
	require.Equal("foo", actual[0].Service.Service)
	require.Equal("1.2.3.4", actual[0].Service.Address)
	require.Equal(30000, actual[0].Service.Port)
	require.Equal("k8s-sync", actual[0].Node)
	require.Equal("foo", actual[1].Service.Service)
	require.Equal("2.3.4.5", actual[1].Service.Address)
	require.Equal(30000, actual[1].Service.Port)
	require.Equal("k8s-sync", actual[1].Node)
	require.NotEqual(actual[0].Service.ID, actual[1].Service.ID)
}

// Test node port works with prefix
func TestServiceResource_nodePortPrefix(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.NodePortSync = ExternalOnly
	serviceResource.ConsulServicePrefix = "prefix"

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	createNodes(t, client)

	createEndpoints(t, client, "foo", metav1.NamespaceDefault)

	// Insert the service
	svc := nodePortService("foo", metav1.NamespaceDefault)
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(svc)
	require.NoError(err)

	// Wait a bit
	time.Sleep(200 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 2)
	require.Equal("prefixfoo", actual[0].Service.Service)
	require.Equal("1.2.3.4", actual[0].Service.Address)
	require.Equal(30000, actual[0].Service.Port)
	require.Equal("k8s-sync", actual[0].Node)
	require.Equal("prefixfoo", actual[1].Service.Service)
	require.Equal("2.3.4.5", actual[1].Service.Address)
	require.Equal(30000, actual[1].Service.Port)
	require.Equal("k8s-sync", actual[1].Node)
	require.NotEqual(actual[0].Service.ID, actual[1].Service.ID)
}

// Test that the registrations for a NodePort type
// are generated only for the nodes where pods are running.
func TestServiceResource_nodePort_singleEndpoint(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.NodePortSync = ExternalOnly

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	node1, _ := createNodes(t, client)

	// Insert the endpoints
	_, err := client.CoreV1().Endpoints(metav1.NamespaceDefault).Create(&apiv1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},

		Subsets: []apiv1.EndpointSubset{
			{
				Addresses: []apiv1.EndpointAddress{
					{NodeName: &node1.Name, IP: "1.2.3.4"},
				},
				Ports: []apiv1.EndpointPort{
					{Name: "http", Port: 8080},
					{Name: "rpc", Port: 2000},
				},
			},
		},
	})
	require.NoError(err)

	// Insert the service
	svc := nodePortService("foo", metav1.NamespaceDefault)
	_, err = client.CoreV1().Services(metav1.NamespaceDefault).Create(svc)
	require.NoError(err)

	// Wait a bit
	time.Sleep(300 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 1)
	require.Equal("foo", actual[0].Service.Service)
	require.Equal("1.2.3.4", actual[0].Service.Address)
	require.Equal(30000, actual[0].Service.Port)
	require.Equal("k8s-sync", actual[0].Node)
}

// Test that the proper registrations are generated for a NodePort with annotated port.
func TestServiceResource_nodePortAnnotatedPort(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.NodePortSync = ExternalOnly

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	createNodes(t, client)

	createEndpoints(t, client, "foo", metav1.NamespaceDefault)

	// Insert the service
	svc := nodePortService("foo", metav1.NamespaceDefault)
	svc.Annotations = map[string]string{annotationServicePort: "rpc"}
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(svc)
	require.NoError(err)

	// Wait a bit
	time.Sleep(300 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 2)
	require.Equal("foo", actual[0].Service.Service)
	require.Equal("1.2.3.4", actual[0].Service.Address)
	require.Equal(30001, actual[0].Service.Port)
	require.Equal("k8s-sync", actual[0].Node)
	require.Equal("foo", actual[1].Service.Service)
	require.Equal("2.3.4.5", actual[1].Service.Address)
	require.Equal(30001, actual[1].Service.Port)
	require.Equal("k8s-sync", actual[1].Node)
	require.NotEqual(actual[0].Service.ID, actual[1].Service.ID)
}

// Test that the proper registrations are generated for a NodePort with an unnamed port.
func TestServiceResource_nodePortUnnamedPort(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.NodePortSync = ExternalOnly

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	createNodes(t, client)

	createEndpoints(t, client, "foo", metav1.NamespaceDefault)

	// Insert the service
	svc := nodePortService("foo", metav1.NamespaceDefault)
	// Override service ports
	svc.Spec.Ports = []apiv1.ServicePort{
		{Port: 80, TargetPort: intstr.FromInt(8080), NodePort: 30000},
		{Port: 8500, TargetPort: intstr.FromInt(2000), NodePort: 30001},
	}
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(svc)
	require.NoError(err)

	// Wait a bit
	time.Sleep(300 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 2)
	require.Equal("foo", actual[0].Service.Service)
	require.Equal("1.2.3.4", actual[0].Service.Address)
	require.Equal(30000, actual[0].Service.Port)
	require.Equal("k8s-sync", actual[0].Node)
	require.Equal("foo", actual[1].Service.Service)
	require.Equal("2.3.4.5", actual[1].Service.Address)
	require.Equal(30000, actual[1].Service.Port)
	require.Equal("k8s-sync", actual[1].Node)
	require.NotEqual(actual[0].Service.ID, actual[1].Service.ID)
}

// Test that the proper registrations are generated for a NodePort type
// when syncing internal Node IPs only.
func TestServiceResource_nodePort_internalOnlySync(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.NodePortSync = InternalOnly

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	createNodes(t, client)

	createEndpoints(t, client, "foo", metav1.NamespaceDefault)

	// Insert the service
	svc := nodePortService("foo", metav1.NamespaceDefault)
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(svc)
	require.NoError(err)

	// Wait a bit
	time.Sleep(200 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 2)
	require.Equal("foo", actual[0].Service.Service)
	require.Equal("4.5.6.7", actual[0].Service.Address)
	require.Equal(30000, actual[0].Service.Port)
	require.Equal("k8s-sync", actual[0].Node)
	require.Equal("foo", actual[1].Service.Service)
	require.Equal("3.4.5.6", actual[1].Service.Address)
	require.Equal(30000, actual[1].Service.Port)
	require.Equal("k8s-sync", actual[1].Node)
	require.NotEqual(actual[0].Service.ID, actual[1].Service.ID)
}

// Test that the proper registrations are generated for a NodePort type
// when preferring to sync external Node IPs over internal IPs
func TestServiceResource_nodePort_externalFirstSync(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.NodePortSync = ExternalFirst

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	node1, _ := createNodes(t, client)

	node1.Status = apiv1.NodeStatus{
		Addresses: []apiv1.NodeAddress{
			{Type: apiv1.NodeInternalIP, Address: "4.5.6.7"},
		},
	}
	_, err := client.CoreV1().Nodes().UpdateStatus(node1)
	require.NoError(err)

	createEndpoints(t, client, "foo", metav1.NamespaceDefault)

	// Insert the service
	svc := nodePortService("foo", metav1.NamespaceDefault)
	_, err = client.CoreV1().Services(metav1.NamespaceDefault).Create(svc)
	require.NoError(err)

	// Wait a bit
	time.Sleep(300 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 2)
	require.Equal("foo", actual[0].Service.Service)
	require.Equal("4.5.6.7", actual[0].Service.Address)
	require.Equal(30000, actual[0].Service.Port)
	require.Equal("k8s-sync", actual[0].Node)
	require.Equal("foo", actual[1].Service.Service)
	require.Equal("2.3.4.5", actual[1].Service.Address)
	require.Equal(30000, actual[1].Service.Port)
	require.Equal("k8s-sync", actual[1].Node)
	require.NotEqual(actual[0].Service.ID, actual[1].Service.ID)
}

// Test that the proper registrations are generated for a ClusterIP type.
func TestServiceResource_clusterIP(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.ClusterIPSync = true

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert the service
	svc := clusterIPService("foo", metav1.NamespaceDefault)
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(svc)
	require.NoError(err)

	// Insert the endpoints
	createEndpoints(t, client, "foo", metav1.NamespaceDefault)

	// Wait a bit
	time.Sleep(300 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 2)
	require.Equal("foo", actual[0].Service.Service)
	require.Equal("1.1.1.1", actual[0].Service.Address)
	require.Equal(8080, actual[0].Service.Port)
	require.Equal("foo", actual[1].Service.Service)
	require.Equal("2.2.2.2", actual[1].Service.Address)
	require.Equal(8080, actual[1].Service.Port)
	require.NotEqual(actual[0].Service.ID, actual[1].Service.ID)
}

// Test clusterIP with prefix
func TestServiceResource_clusterIPPrefix(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.ClusterIPSync = true
	serviceResource.ConsulServicePrefix = "prefix"

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert the service
	svc := clusterIPService("foo", metav1.NamespaceDefault)
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(svc)
	require.NoError(err)

	// Insert the endpoints
	createEndpoints(t, client, "foo", metav1.NamespaceDefault)

	// Wait a bit
	time.Sleep(300 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 2)
	require.Equal("prefixfoo", actual[0].Service.Service)
	require.Equal("1.1.1.1", actual[0].Service.Address)
	require.Equal(8080, actual[0].Service.Port)
	require.Equal("prefixfoo", actual[1].Service.Service)
	require.Equal("2.2.2.2", actual[1].Service.Address)
	require.Equal(8080, actual[1].Service.Port)
	require.NotEqual(actual[0].Service.ID, actual[1].Service.ID)
}

// Test that the proper registrations are generated for a ClusterIP type with
// annotated port name override.
func TestServiceResource_clusterIPAnnotatedPortName(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.ClusterIPSync = true

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert the service
	svc := clusterIPService("foo", metav1.NamespaceDefault)
	svc.Annotations[annotationServicePort] = "rpc"
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(svc)
	require.NoError(err)

	// Insert the endpoints
	createEndpoints(t, client, "foo", metav1.NamespaceDefault)

	// Wait a bit
	time.Sleep(300 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 2)
	require.Equal("foo", actual[0].Service.Service)
	require.Equal("1.1.1.1", actual[0].Service.Address)
	require.Equal(2000, actual[0].Service.Port)
	require.Equal("foo", actual[1].Service.Service)
	require.Equal("2.2.2.2", actual[1].Service.Address)
	require.Equal(2000, actual[1].Service.Port)
	require.NotEqual(actual[0].Service.ID, actual[1].Service.ID)
}

// Test that the proper registrations are generated for a ClusterIP type with
// annotated port number override.
func TestServiceResource_clusterIPAnnotatedPortNumber(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.ClusterIPSync = true

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert the service
	svc := clusterIPService("foo", metav1.NamespaceDefault)
	svc.Annotations[annotationServicePort] = "4141"
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(svc)
	require.NoError(err)

	// Insert the endpoints
	createEndpoints(t, client, "foo", metav1.NamespaceDefault)

	// Wait a bit
	time.Sleep(300 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 2)
	require.Equal("foo", actual[0].Service.Service)
	require.Equal("1.1.1.1", actual[0].Service.Address)
	require.Equal(4141, actual[0].Service.Port)
	require.Equal("foo", actual[1].Service.Service)
	require.Equal("2.2.2.2", actual[1].Service.Address)
	require.Equal(4141, actual[1].Service.Port)
	require.NotEqual(actual[0].Service.ID, actual[1].Service.ID)
}

// Test that the proper registrations are generated for a ClusterIP type with unnamed ports.
func TestServiceResource_clusterIPUnnamedPorts(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.ClusterIPSync = true

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert the service
	svc := clusterIPService("foo", metav1.NamespaceDefault)
	svc.Spec.Ports = []apiv1.ServicePort{
		{Port: 80, TargetPort: intstr.FromInt(8080)},
		{Port: 8500, TargetPort: intstr.FromInt(2000)},
	}
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(svc)
	require.NoError(err)

	// Insert the endpoints
	createEndpoints(t, client, "foo", metav1.NamespaceDefault)

	// Wait a bit
	time.Sleep(300 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 2)
	require.Equal("foo", actual[0].Service.Service)
	require.Equal("1.1.1.1", actual[0].Service.Address)
	require.Equal(8080, actual[0].Service.Port)
	require.Equal("foo", actual[1].Service.Service)
	require.Equal("2.2.2.2", actual[1].Service.Address)
	require.Equal(8080, actual[1].Service.Port)
	require.NotEqual(actual[0].Service.ID, actual[1].Service.ID)
}

// Test that the ClusterIP services aren't synced when ClusterIPSync
// is disabled.
func TestServiceResource_clusterIPSyncDisabled(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.ClusterIPSync = false

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert the service
	svc := clusterIPService("foo", metav1.NamespaceDefault)
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(svc)
	require.NoError(err)

	// Insert the endpoints
	createEndpoints(t, client, "foo", metav1.NamespaceDefault)

	// Wait a bit
	time.Sleep(300 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 0)
}

// Test that the ClusterIP services are synced when watching all namespaces
func TestServiceResource_clusterIPAllNamespaces(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}
	testNamespace := "test_namespace"
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.ClusterIPSync = true

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert the service
	svc := clusterIPService("foo", testNamespace)
	_, err := client.CoreV1().Services(testNamespace).Create(svc)
	require.NoError(err)

	// Insert the endpoints
	createEndpoints(t, client, "foo", testNamespace)

	// Wait a bit
	time.Sleep(300 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 2)
	require.Equal("foo", actual[0].Service.Service)
	require.Equal("1.1.1.1", actual[0].Service.Address)
	require.Equal(8080, actual[0].Service.Port)
	require.Equal("foo", actual[1].Service.Service)
	require.Equal("2.2.2.2", actual[1].Service.Address)
	require.Equal(8080, actual[1].Service.Port)
	require.NotEqual(actual[0].Service.ID, actual[1].Service.ID)
}

// Test using a port name annotation when the targetPort is a named port.
func TestServiceResource_clusterIPTargetPortNamed(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}
	serviceResource := defaultServiceResource(client, syncer)
	serviceResource.ClusterIPSync = true

	// Start the controller
	closer := controller.TestControllerRun(&serviceResource)
	defer closer()

	// Insert the service
	svc := clusterIPService("foo", metav1.NamespaceDefault)
	svc.Annotations[annotationServicePort] = "rpc"
	svc.Spec.Ports = []apiv1.ServicePort{
		{Port: 80, TargetPort: intstr.FromString("httpPort"), Name: "http"},
		{Port: 8500, TargetPort: intstr.FromString("rpcPort"), Name: "rpc"},
	}
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(svc)
	require.NoError(err)

	// Insert the endpoints
	createEndpoints(t, client, "foo", metav1.NamespaceDefault)

	// Wait a bit
	time.Sleep(300 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 2)
	require.Equal("foo", actual[0].Service.Service)
	require.Equal("1.1.1.1", actual[0].Service.Address)
	require.Equal(2000, actual[0].Service.Port)
	require.Equal("foo", actual[1].Service.Service)
	require.Equal("2.2.2.2", actual[1].Service.Address)
	require.Equal(2000, actual[1].Service.Port)
	require.NotEqual(actual[0].Service.ID, actual[1].Service.ID)
}

// lbService returns a Kubernetes service of type LoadBalancer.
func lbService(name, namespace, lbIP string) *apiv1.Service {
	return &apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: map[string]string{},
		},

		Spec: apiv1.ServiceSpec{
			Type: apiv1.ServiceTypeLoadBalancer,
		},

		Status: apiv1.ServiceStatus{
			LoadBalancer: apiv1.LoadBalancerStatus{
				Ingress: []apiv1.LoadBalancerIngress{
					{
						IP: lbIP,
					},
				},
			},
		},
	}
}

// nodePortService returns a Kubernetes service of type NodePort.
func nodePortService(name, namespace string) *apiv1.Service {
	return &apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},

		Spec: apiv1.ServiceSpec{
			Type: apiv1.ServiceTypeNodePort,
			Ports: []apiv1.ServicePort{
				{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080), NodePort: 30000},
				{Name: "rpc", Port: 8500, TargetPort: intstr.FromInt(2000), NodePort: 30001},
			},
		},
	}
}

// clusterIPService returns a Kubernetes service of type ClusterIP.
func clusterIPService(name, namespace string) *apiv1.Service {
	return &apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: map[string]string{},
		},

		Spec: apiv1.ServiceSpec{
			Type: apiv1.ServiceTypeClusterIP,
			Ports: []apiv1.ServicePort{
				{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080)},
				{Name: "rpc", Port: 8500, TargetPort: intstr.FromInt(2000)},
			},
		},
	}
}

// createNodes calls the fake k8s client to create two Kubernetes nodes and returns them.
func createNodes(t *testing.T, client *fake.Clientset) (*apiv1.Node, *apiv1.Node) {
	// Insert the nodes
	node1 := &apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName1,
		},

		Status: apiv1.NodeStatus{
			Addresses: []apiv1.NodeAddress{
				{Type: apiv1.NodeExternalIP, Address: "1.2.3.4"},
				{Type: apiv1.NodeInternalIP, Address: "4.5.6.7"},
			},
		},
	}
	_, err := client.CoreV1().Nodes().Create(node1)
	require.NoError(t, err)

	node2 := &apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName2,
		},

		Status: apiv1.NodeStatus{
			Addresses: []apiv1.NodeAddress{
				{Type: apiv1.NodeExternalIP, Address: "2.3.4.5"},
				{Type: apiv1.NodeInternalIP, Address: "3.4.5.6"},
			},
		},
	}
	_, err = client.CoreV1().Nodes().Create(node2)
	require.NoError(t, err)

	return node1, node2
}

// createEndpoints calls the fake k8s client to create two endpoints on two nodes.
func createEndpoints(t *testing.T, client *fake.Clientset, serviceName string, namespace string) {
	node1 := nodeName1
	node2 := nodeName2
	_, err := client.CoreV1().Endpoints(namespace).Create(&apiv1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: namespace,
		},

		Subsets: []apiv1.EndpointSubset{
			{
				Addresses: []apiv1.EndpointAddress{
					{NodeName: &node1, IP: "1.1.1.1"},
				},
				Ports: []apiv1.EndpointPort{
					{Name: "http", Port: 8080},
					{Name: "rpc", Port: 2000},
				},
			},

			{
				Addresses: []apiv1.EndpointAddress{
					{NodeName: &node2, IP: "2.2.2.2"},
				},
				Ports: []apiv1.EndpointPort{
					{Name: "http", Port: 8080},
					{Name: "rpc", Port: 2000},
				},
			},
		},
	})

	require.NoError(t, err)
}

func defaultServiceResource(client kubernetes.Interface, syncer Syncer) ServiceResource {
	// Set up required allow and deny sets
	allowSet := mapset.NewSet("*")
	denySet := mapset.NewSet()

	return ServiceResource{
		Log:                   hclog.Default(),
		Client:                client,
		Syncer:                syncer,
		AllowK8sNamespacesSet: allowSet,
		DenyK8sNamespacesSet:  denySet,
	}
}
