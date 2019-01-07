package catalog

import (
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/helper/controller"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
)

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

	// Start the controller
	closer := controller.TestControllerRun(&ServiceResource{
		Log:    hclog.Default(),
		Client: client,
		Syncer: syncer,
	})
	defer closer()

	// Insert an LB service
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(testService("foo"))
	require.NoError(err)
	time.Sleep(200 * time.Millisecond)

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

	// Start the controller
	closer := controller.TestControllerRun(&ServiceResource{
		Log:    hclog.Default(),
		Client: client,
		Syncer: syncer,
	})
	defer closer()

	// Insert an LB service
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(testService("foo"))
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

	// Start the controller
	closer := controller.TestControllerRun(&ServiceResource{
		Log:    hclog.Default(),
		Client: client,
		Syncer: syncer,
	})
	defer closer()

	// Insert an LB service
	svc := testService("foo")
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

	// Start the controller
	closer := controller.TestControllerRun(&ServiceResource{
		Log:            hclog.Default(),
		Client:         client,
		Syncer:         syncer,
		ExplicitEnable: true,
	})
	defer closer()

	// Insert an LB service
	svc := testService("foo")
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

	// Start the controller
	closer := controller.TestControllerRun(&ServiceResource{
		Log:            hclog.Default(),
		Client:         client,
		Syncer:         syncer,
		ExplicitEnable: true,
	})
	defer closer()

	// Insert an LB service
	svc := testService("foo")
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

// Test that system resources are not synced by default.
func TestServiceResource_system(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}

	// Start the controller
	closer := controller.TestControllerRun(&ServiceResource{
		Log:    hclog.Default(),
		Client: client,
		Syncer: syncer,
	})
	defer closer()

	// Insert an LB service
	svc := testService("foo")
	_, err := client.CoreV1().Services(metav1.NamespaceSystem).Create(svc)
	require.NoError(err)
	time.Sleep(200 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 0)
}

// Test that external IPs take priority.
func TestServiceResource_externalIP(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}

	// Start the controller
	closer := controller.TestControllerRun(&ServiceResource{
		Log:    hclog.Default(),
		Client: client,
		Syncer: syncer,
	})
	defer closer()

	// Insert an LB service
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(&apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},

		Spec: apiv1.ServiceSpec{
			Type:        apiv1.ServiceTypeLoadBalancer,
			ExternalIPs: []string{"a", "b"},
		},

		Status: apiv1.ServiceStatus{
			LoadBalancer: apiv1.LoadBalancerStatus{
				Ingress: []apiv1.LoadBalancerIngress{
					apiv1.LoadBalancerIngress{
						IP: "1.2.3.4",
					},
				},
			},
		},
	})
	require.NoError(err)

	// Wait a bit
	time.Sleep(500 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 2)
	require.Equal("foo", actual[0].Service.Service)
	require.Equal("a", actual[0].Service.Address)
	require.Equal("foo", actual[1].Service.Service)
	require.Equal("b", actual[1].Service.Address)
}

// Test that the proper registrations are generated for a LoadBalancer.
func TestServiceResource_lb(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}

	// Start the controller
	closer := controller.TestControllerRun(&ServiceResource{
		Log:    hclog.Default(),
		Client: client,
		Syncer: syncer,
	})
	defer closer()

	// Insert an LB service
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(&apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},

		Spec: apiv1.ServiceSpec{
			Type: apiv1.ServiceTypeLoadBalancer,
		},

		Status: apiv1.ServiceStatus{
			LoadBalancer: apiv1.LoadBalancerStatus{
				Ingress: []apiv1.LoadBalancerIngress{
					apiv1.LoadBalancerIngress{
						IP: "1.2.3.4",
					},
				},
			},
		},
	})
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

// Test that the proper registrations are generated for a LoadBalancer
// with multiple endpoints.
func TestServiceResource_lbMultiEndpoint(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}

	// Start the controller
	closer := controller.TestControllerRun(&ServiceResource{
		Log:    hclog.Default(),
		Client: client,
		Syncer: syncer,
	})
	defer closer()

	// Insert an LB service
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(&apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},

		Spec: apiv1.ServiceSpec{
			Type: apiv1.ServiceTypeLoadBalancer,
		},

		Status: apiv1.ServiceStatus{
			LoadBalancer: apiv1.LoadBalancerStatus{
				Ingress: []apiv1.LoadBalancerIngress{
					apiv1.LoadBalancerIngress{
						IP: "1.2.3.4",
					},
					apiv1.LoadBalancerIngress{
						IP: "2.3.4.5",
					},
				},
			},
		},
	})
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

	// Start the controller
	closer := controller.TestControllerRun(&ServiceResource{
		Log:    hclog.Default(),
		Client: client,
		Syncer: syncer,
	})
	defer closer()

	// Insert an LB service
	svc := testService("foo")
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

	// Start the controller
	closer := controller.TestControllerRun(&ServiceResource{
		Log:    hclog.Default(),
		Client: client,
		Syncer: syncer,
	})
	defer closer()

	// Insert an LB service
	svc := testService("foo")
	svc.Spec.Ports = []apiv1.ServicePort{
		apiv1.ServicePort{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080)},
		apiv1.ServicePort{Name: "rpc", Port: 8500, TargetPort: intstr.FromInt(2000)},
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

	// Start the controller
	closer := controller.TestControllerRun(&ServiceResource{
		Log:    hclog.Default(),
		Client: client,
		Syncer: syncer,
	})
	defer closer()

	// Insert an LB service
	svc := testService("foo")
	svc.Annotations[annotationServicePort] = "rpc"
	svc.Spec.Ports = []apiv1.ServicePort{
		apiv1.ServicePort{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080)},
		apiv1.ServicePort{Name: "rpc", Port: 8500, TargetPort: intstr.FromInt(2000)},
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

	// Start the controller
	closer := controller.TestControllerRun(&ServiceResource{
		Log:    hclog.Default(),
		Client: client,
		Syncer: syncer,
	})
	defer closer()

	// Insert an LB service
	svc := testService("foo")
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

	// Start the controller
	closer := controller.TestControllerRun(&ServiceResource{
		Log:    hclog.Default(),
		Client: client,
		Syncer: syncer,
	})
	defer closer()

	// Insert an LB service
	svc := testService("foo")
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
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}

	// Start the controller
	closer := controller.TestControllerRun(&ServiceResource{
		Log:                hclog.Default(),
		Client:             client,
		Syncer:             syncer,
		NodeExternalIPSync: true,
	})
	defer closer()

	node1 := "ip-10-11-12-13.ec2.internal"
	node2 := "ip-10-11-12-14.ec2.internal"
	// Insert the nodes
	_, err := client.CoreV1().Nodes().Create(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: node1,
		},

		Status: apiv1.NodeStatus{
			Addresses: []apiv1.NodeAddress{
				apiv1.NodeAddress{Type: apiv1.NodeExternalIP, Address: "1.2.3.4"},
				apiv1.NodeAddress{Type: apiv1.NodeInternalIP, Address: "4.5.6.7"},
			},
		},
	})
	require.NoError(err)

	_, err = client.CoreV1().Nodes().Create(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: node2,
		},

		Status: apiv1.NodeStatus{
			Addresses: []apiv1.NodeAddress{
				apiv1.NodeAddress{Type: apiv1.NodeExternalIP, Address: "2.3.4.5"},
				apiv1.NodeAddress{Type: apiv1.NodeInternalIP, Address: "3.4.5.6"},
			},
		},
	})
	require.NoError(err)
	time.Sleep(200 * time.Millisecond)

	// Insert the endpoints
	_, err = client.CoreV1().Endpoints(metav1.NamespaceDefault).Create(&apiv1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},

		Subsets: []apiv1.EndpointSubset{
			apiv1.EndpointSubset{
				Addresses: []apiv1.EndpointAddress{
					apiv1.EndpointAddress{NodeName: &node1, IP: "1.2.3.4"},
				},
				Ports: []apiv1.EndpointPort{
					apiv1.EndpointPort{Name: "http", Port: 8080},
					apiv1.EndpointPort{Name: "rpc", Port: 2000},
				},
			},

			apiv1.EndpointSubset{
				Addresses: []apiv1.EndpointAddress{
					apiv1.EndpointAddress{NodeName: &node2, IP: "2.3.4.5"},
				},
				Ports: []apiv1.EndpointPort{
					apiv1.EndpointPort{Name: "http", Port: 8080},
					apiv1.EndpointPort{Name: "rpc", Port: 2000},
				},
			},
		},
	})
	require.NoError(err)

	// Wait a bit
	time.Sleep(300 * time.Millisecond)

	// Insert the service
	_, err = client.CoreV1().Services(metav1.NamespaceDefault).Create(&apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},

		Spec: apiv1.ServiceSpec{
			Type: apiv1.ServiceTypeNodePort,
			Ports: []apiv1.ServicePort{
				apiv1.ServicePort{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080), NodePort: 30000},
				apiv1.ServicePort{Name: "rpc", Port: 8500, TargetPort: intstr.FromInt(2000), NodePort: 30001},
			},
		},
	})
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
	require.Equal(node1, actual[0].Node)
	require.Equal("foo", actual[1].Service.Service)
	require.Equal("2.3.4.5", actual[1].Service.Address)
	require.Equal(30000, actual[1].Service.Port)
	require.Equal(node2, actual[1].Node)
	require.NotEqual(actual[0].Service.ID, actual[1].Service.ID)
}

// Test that the proper registrations are generated for a NodePort type.
func TestServiceResource_nodePort_singleEndpoint(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}

	// Start the controller
	closer := controller.TestControllerRun(&ServiceResource{
		Log:                hclog.Default(),
		Client:             client,
		Syncer:             syncer,
		NodeExternalIPSync: true,
	})
	defer closer()

	node1 := "ip-10-11-12-13.ec2.internal"
	node2 := "ip-10-11-12-14.ec2.internal"
	// Insert the nodes
	_, err := client.CoreV1().Nodes().Create(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: node1,
		},

		Status: apiv1.NodeStatus{
			Addresses: []apiv1.NodeAddress{
				apiv1.NodeAddress{Type: apiv1.NodeExternalIP, Address: "1.2.3.4"},
			},
		},
	})
	require.NoError(err)

	_, err = client.CoreV1().Nodes().Create(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: node2,
		},

		Status: apiv1.NodeStatus{
			Addresses: []apiv1.NodeAddress{
				apiv1.NodeAddress{Type: apiv1.NodeExternalIP, Address: "2.3.4.5"},
			},
		},
	})
	require.NoError(err)
	time.Sleep(200 * time.Millisecond)

	// Insert the endpoints
	_, err = client.CoreV1().Endpoints(metav1.NamespaceDefault).Create(&apiv1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},

		Subsets: []apiv1.EndpointSubset{
			apiv1.EndpointSubset{
				Addresses: []apiv1.EndpointAddress{
					apiv1.EndpointAddress{NodeName: &node1, IP: "1.2.3.4"},
				},
				Ports: []apiv1.EndpointPort{
					apiv1.EndpointPort{Name: "http", Port: 8080},
					apiv1.EndpointPort{Name: "rpc", Port: 2000},
				},
			},
		},
	})
	require.NoError(err)

	// Wait a bit
	time.Sleep(300 * time.Millisecond)

	// Insert the service
	_, err = client.CoreV1().Services(metav1.NamespaceDefault).Create(&apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},

		Spec: apiv1.ServiceSpec{
			Type: apiv1.ServiceTypeNodePort,
			Ports: []apiv1.ServicePort{
				apiv1.ServicePort{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080), NodePort: 30000},
				apiv1.ServicePort{Name: "rpc", Port: 8500, TargetPort: intstr.FromInt(2000), NodePort: 30001},
			},
		},
	})
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
	require.Equal(node1, actual[0].Node)
}

// Test that a NodePort created earlier works (doesn't require an Endpoints
// update event).
func TestServiceResource_nodePortInitial(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}

	// Start the controller
	closer := controller.TestControllerRun(&ServiceResource{
		Log:                hclog.Default(),
		Client:             client,
		Syncer:             syncer,
		NodeExternalIPSync: true,
	})
	defer closer()
	time.Sleep(100 * time.Millisecond)

	node1 := "ip-10-11-12-13.ec2.internal"
	node2 := "ip-10-11-12-14.ec2.internal"
	// Insert the nodes
	_, err := client.CoreV1().Nodes().Create(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: node1,
		},

		Status: apiv1.NodeStatus{
			Addresses: []apiv1.NodeAddress{
				apiv1.NodeAddress{Type: apiv1.NodeExternalIP, Address: "1.2.3.4"},
			},
		},
	})
	require.NoError(err)

	_, err = client.CoreV1().Nodes().Create(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: node2,
		},

		Status: apiv1.NodeStatus{
			Addresses: []apiv1.NodeAddress{
				apiv1.NodeAddress{Type: apiv1.NodeExternalIP, Address: "2.3.4.5"},
			},
		},
	})
	require.NoError(err)
	time.Sleep(200 * time.Millisecond)

	// Insert the endpoints
	_, err = client.CoreV1().Endpoints(metav1.NamespaceDefault).Create(&apiv1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},

		Subsets: []apiv1.EndpointSubset{
			apiv1.EndpointSubset{
				Addresses: []apiv1.EndpointAddress{
					apiv1.EndpointAddress{NodeName: &node1, IP: "1.2.3.4"},
				},
				Ports: []apiv1.EndpointPort{
					apiv1.EndpointPort{Name: "http", Port: 8080},
					apiv1.EndpointPort{Name: "rpc", Port: 2000},
				},
			},

			apiv1.EndpointSubset{
				Addresses: []apiv1.EndpointAddress{
					apiv1.EndpointAddress{NodeName: &node2, IP: "2.3.4.5"},
				},
				Ports: []apiv1.EndpointPort{
					apiv1.EndpointPort{Name: "http", Port: 8080},
					apiv1.EndpointPort{Name: "rpc", Port: 2000},
				},
			},
		},
	})
	require.NoError(err)

	// Wait a bit
	time.Sleep(300 * time.Millisecond)

	// Insert the service
	_, err = client.CoreV1().Services(metav1.NamespaceDefault).Create(&apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},

		Spec: apiv1.ServiceSpec{
			Type: apiv1.ServiceTypeNodePort,
			Ports: []apiv1.ServicePort{
				apiv1.ServicePort{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080), NodePort: 30000},
				apiv1.ServicePort{Name: "rpc", Port: 8500, TargetPort: intstr.FromInt(2000), NodePort: 30001},
			},
		},
	})
	require.NoError(err)

	// Wait a bit
	time.Sleep(400 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 2)
	require.Equal("foo", actual[0].Service.Service)
	require.Equal("1.2.3.4", actual[0].Service.Address)
	require.Equal(30000, actual[0].Service.Port)
	require.Equal(node1, actual[0].Node)
	require.Equal("foo", actual[1].Service.Service)
	require.Equal("2.3.4.5", actual[1].Service.Address)
	require.Equal(30000, actual[1].Service.Port)
	require.Equal(node2, actual[1].Node)
}

// Test that the proper registrations are generated for a NodePort with annotated port.
func TestServiceResource_nodePortAnnotatedPort(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}

	// Start the controller
	closer := controller.TestControllerRun(&ServiceResource{
		Log:                hclog.Default(),
		Client:             client,
		Syncer:             syncer,
		NodeExternalIPSync: true,
	})
	defer closer()

	node1 := "ip-10-11-12-13.ec2.internal"
	node2 := "ip-10-11-12-14.ec2.internal"
	// Insert the nodes
	_, err := client.CoreV1().Nodes().Create(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: node1,
		},

		Status: apiv1.NodeStatus{
			Addresses: []apiv1.NodeAddress{
				apiv1.NodeAddress{Type: apiv1.NodeExternalIP, Address: "1.2.3.4"},
			},
		},
	})
	require.NoError(err)

	_, err = client.CoreV1().Nodes().Create(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: node2,
		},

		Status: apiv1.NodeStatus{
			Addresses: []apiv1.NodeAddress{
				apiv1.NodeAddress{Type: apiv1.NodeExternalIP, Address: "2.3.4.5"},
			},
		},
	})
	require.NoError(err)

	// Wait a bit
	time.Sleep(300 * time.Millisecond)

	// Insert the endpoints
	_, err = client.CoreV1().Endpoints(metav1.NamespaceDefault).Create(&apiv1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},

		Subsets: []apiv1.EndpointSubset{
			apiv1.EndpointSubset{
				Addresses: []apiv1.EndpointAddress{
					apiv1.EndpointAddress{NodeName: &node1, IP: "1.2.3.4"},
				},
				Ports: []apiv1.EndpointPort{
					apiv1.EndpointPort{Name: "http", Port: 8080},
					apiv1.EndpointPort{Name: "rpc", Port: 2000},
				},
			},

			apiv1.EndpointSubset{
				Addresses: []apiv1.EndpointAddress{
					apiv1.EndpointAddress{NodeName: &node2, IP: "2.3.4.5"},
				},
				Ports: []apiv1.EndpointPort{
					apiv1.EndpointPort{Name: "http", Port: 8080},
					apiv1.EndpointPort{Name: "rpc", Port: 2000},
				},
			},
		},
	})
	require.NoError(err)

	// Wait a bit
	time.Sleep(300 * time.Millisecond)

	// Insert the service
	_, err = client.CoreV1().Services(metav1.NamespaceDefault).Create(&apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "foo",
			Annotations: map[string]string{annotationServicePort: "rpc"},
		},

		Spec: apiv1.ServiceSpec{
			Type: apiv1.ServiceTypeNodePort,
			Ports: []apiv1.ServicePort{
				apiv1.ServicePort{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080), NodePort: 30000},
				apiv1.ServicePort{Name: "rpc", Port: 8500, TargetPort: intstr.FromInt(2000), NodePort: 30001},
			},
		},
	})
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
	require.Equal(node1, actual[0].Node)
	require.Equal("foo", actual[1].Service.Service)
	require.Equal("2.3.4.5", actual[1].Service.Address)
	require.Equal(30001, actual[1].Service.Port)
	require.Equal(node2, actual[1].Node)
	require.NotEqual(actual[0].Service.ID, actual[1].Service.ID)
}

// Test that the proper registrations are generated for a NodePort with annotated port.
func TestServiceResource_nodePortUnnamedPort(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}

	// Start the controller
	closer := controller.TestControllerRun(&ServiceResource{
		Log:                hclog.Default(),
		Client:             client,
		Syncer:             syncer,
		NodeExternalIPSync: true,
	})
	defer closer()

	node1 := "ip-10-11-12-13.ec2.internal"
	node2 := "ip-10-11-12-14.ec2.internal"
	// Insert the nodes
	_, err := client.CoreV1().Nodes().Create(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: node1,
		},

		Status: apiv1.NodeStatus{
			Addresses: []apiv1.NodeAddress{
				apiv1.NodeAddress{Type: apiv1.NodeExternalIP, Address: "1.2.3.4"},
			},
		},
	})
	require.NoError(err)

	_, err = client.CoreV1().Nodes().Create(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: node2,
		},

		Status: apiv1.NodeStatus{
			Addresses: []apiv1.NodeAddress{
				apiv1.NodeAddress{Type: apiv1.NodeExternalIP, Address: "2.3.4.5"},
			},
		},
	})
	require.NoError(err)

	// Wait a bit
	time.Sleep(300 * time.Millisecond)

	// Insert the endpoints
	_, err = client.CoreV1().Endpoints(metav1.NamespaceDefault).Create(&apiv1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},

		Subsets: []apiv1.EndpointSubset{
			apiv1.EndpointSubset{
				Addresses: []apiv1.EndpointAddress{
					apiv1.EndpointAddress{NodeName: &node1, IP: "1.2.3.4"},
				},
				Ports: []apiv1.EndpointPort{
					apiv1.EndpointPort{Port: 8080},
					apiv1.EndpointPort{Port: 2000},
				},
			},

			apiv1.EndpointSubset{
				Addresses: []apiv1.EndpointAddress{
					apiv1.EndpointAddress{NodeName: &node2, IP: "2.3.4.5"},
				},
				Ports: []apiv1.EndpointPort{
					apiv1.EndpointPort{Port: 8080},
					apiv1.EndpointPort{Port: 2000},
				},
			},
		},
	})
	require.NoError(err)

	// Wait a bit
	time.Sleep(300 * time.Millisecond)

	// Insert the service
	_, err = client.CoreV1().Services(metav1.NamespaceDefault).Create(&apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},

		Spec: apiv1.ServiceSpec{
			Type: apiv1.ServiceTypeNodePort,
			Ports: []apiv1.ServicePort{
				apiv1.ServicePort{Port: 80, TargetPort: intstr.FromInt(8080), NodePort: 30000},
				apiv1.ServicePort{Port: 8500, TargetPort: intstr.FromInt(2000), NodePort: 30001},
			},
		},
	})
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
	require.Equal(node1, actual[0].Node)
	require.Equal("foo", actual[1].Service.Service)
	require.Equal("2.3.4.5", actual[1].Service.Address)
	require.Equal(30000, actual[1].Service.Port)
	require.Equal(node2, actual[1].Node)
	require.NotEqual(actual[0].Service.ID, actual[1].Service.ID)
}

// Test that the proper registrations are generated for a NodePort type.
func TestServiceResource_nodePort_internalIP(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}

	// Start the controller
	closer := controller.TestControllerRun(&ServiceResource{
		Log:                hclog.Default(),
		Client:             client,
		Syncer:             syncer,
		NodeExternalIPSync: false,
	})
	defer closer()

	node1 := "ip-10-11-12-13.ec2.internal"
	node2 := "ip-10-11-12-14.ec2.internal"
	// Insert the nodes
	_, err := client.CoreV1().Nodes().Create(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: node1,
		},

		Status: apiv1.NodeStatus{
			Addresses: []apiv1.NodeAddress{
				apiv1.NodeAddress{Type: apiv1.NodeExternalIP, Address: "1.2.3.4"},
				apiv1.NodeAddress{Type: apiv1.NodeInternalIP, Address: "4.5.6.7"},
			},
		},
	})
	require.NoError(err)

	_, err = client.CoreV1().Nodes().Create(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: node2,
		},

		Status: apiv1.NodeStatus{
			Addresses: []apiv1.NodeAddress{
				apiv1.NodeAddress{Type: apiv1.NodeExternalIP, Address: "2.3.4.5"},
				apiv1.NodeAddress{Type: apiv1.NodeInternalIP, Address: "3.4.5.6"},
			},
		},
	})
	require.NoError(err)
	time.Sleep(200 * time.Millisecond)

	// Insert the endpoints
	_, err = client.CoreV1().Endpoints(metav1.NamespaceDefault).Create(&apiv1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},

		Subsets: []apiv1.EndpointSubset{
			apiv1.EndpointSubset{
				Addresses: []apiv1.EndpointAddress{
					apiv1.EndpointAddress{NodeName: &node1, IP: "1.2.3.4"},
				},
				Ports: []apiv1.EndpointPort{
					apiv1.EndpointPort{Name: "http", Port: 8080},
					apiv1.EndpointPort{Name: "rpc", Port: 2000},
				},
			},

			apiv1.EndpointSubset{
				Addresses: []apiv1.EndpointAddress{
					apiv1.EndpointAddress{NodeName: &node2, IP: "2.3.4.5"},
				},
				Ports: []apiv1.EndpointPort{
					apiv1.EndpointPort{Name: "http", Port: 8080},
					apiv1.EndpointPort{Name: "rpc", Port: 2000},
				},
			},
		},
	})
	require.NoError(err)

	// Wait a bit
	time.Sleep(300 * time.Millisecond)

	// Insert the service
	_, err = client.CoreV1().Services(metav1.NamespaceDefault).Create(&apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},

		Spec: apiv1.ServiceSpec{
			Type: apiv1.ServiceTypeNodePort,
			Ports: []apiv1.ServicePort{
				apiv1.ServicePort{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080), NodePort: 30000},
				apiv1.ServicePort{Name: "rpc", Port: 8500, TargetPort: intstr.FromInt(2000), NodePort: 30001},
			},
		},
	})
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
	require.Equal(node1, actual[0].Node)
	require.Equal("foo", actual[1].Service.Service)
	require.Equal("3.4.5.6", actual[1].Service.Address)
	require.Equal(30000, actual[1].Service.Port)
	require.Equal(node2, actual[1].Node)
	require.NotEqual(actual[0].Service.ID, actual[1].Service.ID)
}

// Test that the proper registrations are generated for a ClusterIP type.
func TestServiceResource_clusterIP(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}

	// Start the controller
	closer := controller.TestControllerRun(&ServiceResource{
		Log:           hclog.Default(),
		Client:        client,
		Syncer:        syncer,
		ClusterIPSync: true,
	})
	defer closer()

	// Insert the service
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(&apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},

		Spec: apiv1.ServiceSpec{
			Type: apiv1.ServiceTypeClusterIP,
			Ports: []apiv1.ServicePort{
				apiv1.ServicePort{Port: 80, TargetPort: intstr.FromInt(8080)},
			},
		},
	})
	require.NoError(err)

	// Wait a bit
	time.Sleep(300 * time.Millisecond)

	// Insert the endpoints
	_, err = client.CoreV1().Endpoints(metav1.NamespaceDefault).Create(&apiv1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},

		Subsets: []apiv1.EndpointSubset{
			apiv1.EndpointSubset{
				Addresses: []apiv1.EndpointAddress{
					apiv1.EndpointAddress{IP: "1.2.3.4"},
				},
			},

			apiv1.EndpointSubset{
				Addresses: []apiv1.EndpointAddress{
					apiv1.EndpointAddress{IP: "2.3.4.5"},
				},
			},
		},
	})
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
	require.Equal(80, actual[0].Service.Port)
	require.Equal("foo", actual[1].Service.Service)
	require.Equal("2.3.4.5", actual[1].Service.Address)
	require.Equal(80, actual[1].Service.Port)
	require.NotEqual(actual[0].Service.ID, actual[1].Service.ID)
}

// Test that the proper registrations are generated for a ClusterIP type with multiple ports.
func TestServiceResource_clusterIPMultiEndpoint(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}

	// Start the controller
	closer := controller.TestControllerRun(&ServiceResource{
		Log:           hclog.Default(),
		Client:        client,
		Syncer:        syncer,
		ClusterIPSync: true,
	})
	defer closer()

	// Insert the service
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(&apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},

		Spec: apiv1.ServiceSpec{
			Type: apiv1.ServiceTypeClusterIP,
			Ports: []apiv1.ServicePort{
				apiv1.ServicePort{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080)},
				apiv1.ServicePort{Name: "rpc", Port: 8500, TargetPort: intstr.FromInt(2000)},
			},
		},
	})
	require.NoError(err)

	// Wait a bit
	time.Sleep(300 * time.Millisecond)

	// Insert the endpoints
	_, err = client.CoreV1().Endpoints(metav1.NamespaceDefault).Create(&apiv1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},

		Subsets: []apiv1.EndpointSubset{
			apiv1.EndpointSubset{
				Addresses: []apiv1.EndpointAddress{
					apiv1.EndpointAddress{IP: "1.2.3.4"},
				},
			},

			apiv1.EndpointSubset{
				Addresses: []apiv1.EndpointAddress{
					apiv1.EndpointAddress{IP: "2.3.4.5"},
				},
			},
		},
	})
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
	require.Equal(80, actual[0].Service.Port)
	require.Equal("foo", actual[1].Service.Service)
	require.Equal("2.3.4.5", actual[1].Service.Address)
	require.Equal(80, actual[1].Service.Port)
	require.NotEqual(actual[0].Service.ID, actual[1].Service.ID)
}

// Test that the proper registrations are generated for a ClusterIP type with annotated override.
func TestServiceResource_clusterIPAnnotatedPort(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}

	// Start the controller
	closer := controller.TestControllerRun(&ServiceResource{
		Log:           hclog.Default(),
		Client:        client,
		Syncer:        syncer,
		ClusterIPSync: true,
	})
	defer closer()

	// Insert the service
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(&apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "foo",
			Annotations: map[string]string{annotationServicePort: "rpc"},
		},

		Spec: apiv1.ServiceSpec{
			Type: apiv1.ServiceTypeClusterIP,
			Ports: []apiv1.ServicePort{
				apiv1.ServicePort{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080)},
				apiv1.ServicePort{Name: "rpc", Port: 8500, TargetPort: intstr.FromInt(2000)},
			},
		},
	})
	require.NoError(err)

	// Wait a bit
	time.Sleep(300 * time.Millisecond)

	// Insert the endpoints
	_, err = client.CoreV1().Endpoints(metav1.NamespaceDefault).Create(&apiv1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},

		Subsets: []apiv1.EndpointSubset{
			apiv1.EndpointSubset{
				Addresses: []apiv1.EndpointAddress{
					apiv1.EndpointAddress{IP: "1.2.3.4"},
				},
			},

			apiv1.EndpointSubset{
				Addresses: []apiv1.EndpointAddress{
					apiv1.EndpointAddress{IP: "2.3.4.5"},
				},
			},
		},
	})
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
	require.Equal(8500, actual[0].Service.Port)
	require.Equal("foo", actual[1].Service.Service)
	require.Equal("2.3.4.5", actual[1].Service.Address)
	require.Equal(8500, actual[1].Service.Port)
	require.NotEqual(actual[0].Service.ID, actual[1].Service.ID)
}

// Test that the proper registrations are generated for a ClusterIP type with unnamed ports.
func TestServiceResource_clusterIPUnnamedPorts(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}

	// Start the controller
	closer := controller.TestControllerRun(&ServiceResource{
		Log:           hclog.Default(),
		Client:        client,
		Syncer:        syncer,
		ClusterIPSync: true,
	})
	defer closer()

	// Insert the service
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(&apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},

		Spec: apiv1.ServiceSpec{
			Type: apiv1.ServiceTypeClusterIP,
			Ports: []apiv1.ServicePort{
				apiv1.ServicePort{Port: 80, TargetPort: intstr.FromInt(8080)},
				apiv1.ServicePort{Port: 8500, TargetPort: intstr.FromInt(2000)},
			},
		},
	})
	require.NoError(err)

	// Wait a bit
	time.Sleep(300 * time.Millisecond)

	// Insert the endpoints
	_, err = client.CoreV1().Endpoints(metav1.NamespaceDefault).Create(&apiv1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},

		Subsets: []apiv1.EndpointSubset{
			apiv1.EndpointSubset{
				Addresses: []apiv1.EndpointAddress{
					apiv1.EndpointAddress{IP: "1.2.3.4"},
				},
			},

			apiv1.EndpointSubset{
				Addresses: []apiv1.EndpointAddress{
					apiv1.EndpointAddress{IP: "2.3.4.5"},
				},
			},
		},
	})
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
	require.Equal(80, actual[0].Service.Port)
	require.Equal("foo", actual[1].Service.Service)
	require.Equal("2.3.4.5", actual[1].Service.Address)
	require.Equal(80, actual[1].Service.Port)
	require.NotEqual(actual[0].Service.ID, actual[1].Service.ID)
}

// Test that the ClusterIP services aren't synced when ClusterIPSync
// is disabled.
func TestServiceResource_clusterIPSyncDisabled(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()
	syncer := &TestSyncer{}

	// Start the controller
	closer := controller.TestControllerRun(&ServiceResource{
		Log:           hclog.Default(),
		Client:        client,
		Syncer:        syncer,
		ClusterIPSync: false,
	})
	defer closer()

	// Insert the service
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(&apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},

		Spec: apiv1.ServiceSpec{
			Type: apiv1.ServiceTypeClusterIP,
			Ports: []apiv1.ServicePort{
				apiv1.ServicePort{Port: 80, TargetPort: intstr.FromInt(8080)},
			},
		},
	})
	require.NoError(err)

	// Wait a bit
	time.Sleep(300 * time.Millisecond)

	// Insert the endpoints
	_, err = client.CoreV1().Endpoints(metav1.NamespaceDefault).Create(&apiv1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},

		Subsets: []apiv1.EndpointSubset{
			apiv1.EndpointSubset{
				Addresses: []apiv1.EndpointAddress{
					apiv1.EndpointAddress{IP: "1.2.3.4"},
				},
			},

			apiv1.EndpointSubset{
				Addresses: []apiv1.EndpointAddress{
					apiv1.EndpointAddress{IP: "2.3.4.5"},
				},
			},
		},
	})
	require.NoError(err)

	// Wait a bit
	time.Sleep(300 * time.Millisecond)

	// Verify what we got
	syncer.Lock()
	defer syncer.Unlock()
	actual := syncer.Registrations
	require.Len(actual, 0)
}

// testService returns a service that will result in a registration.
func testService(name string) *apiv1.Service {
	return &apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: map[string]string{},
		},

		Spec: apiv1.ServiceSpec{
			Type: apiv1.ServiceTypeLoadBalancer,
		},

		Status: apiv1.ServiceStatus{
			LoadBalancer: apiv1.LoadBalancerStatus{
				Ingress: []apiv1.LoadBalancerIngress{
					apiv1.LoadBalancerIngress{
						IP: "1.2.3.4",
					},
				},
			},
		},
	}
}
