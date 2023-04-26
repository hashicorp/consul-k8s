package deployer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var (
	name        = "gateway"
	namespace   = "default"
	image       = "consul-k8s-control-plane:latest"
	replicas    = int32(1)
	labels      = map[string]string{}
	annotations = map[string]string{}
	selector    = map[string]string{}

	gateway = Gateway{
		Name:        name,
		Namespace:   namespace,
		Image:       image,
		Replicas:    replicas,
		Labels:      labels,
		Annotations: annotations,
		Selector:    selector,
	}

	deployment = &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: appsv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       namespace,
			Name:            name,
			ResourceVersion: "1",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  name,
							Image: image,
						},
					},
				},
			},
			Selector: &metav1.LabelSelector{},
		},
	}

	service = &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       namespace,
			Name:            name,
			ResourceVersion: "1",
		},
		Spec: corev1.ServiceSpec{},
	}
)

func TestCreate(t *testing.T) {
	t.Parallel()

	client := fake.NewClientBuilder().Build()
	gatewayDeployer := NewGatewayDeployer(client)

	err := gatewayDeployer.Create(context.Background(), gateway)
	require.NoError(t, err)

	var d appsv1.Deployment
	err = client.Get(context.Background(), types.NamespacedName{
		Namespace: gateway.Deployment().Namespace,
		Name:      gateway.Deployment().Name,
	}, &d)
	require.NoError(t, err)
	require.Equal(t, *deployment, d)

	var s corev1.Service
	err = client.Get(context.Background(), types.NamespacedName{
		Namespace: gateway.Service().Namespace,
		Name:      gateway.Service().Name,
	}, &s)
	require.NoError(t, err)
	require.Equal(t, *service, s)
}

func TestDelete(t *testing.T) {
	t.Parallel()

	podToNotDelete := &corev1.Pod{}

	k8sObjects := []client.Object{service, deployment, podToNotDelete}

	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}

	client := fake.NewClientBuilder().WithObjects(k8sObjects...).Build()

	gatewayDeployer := NewGatewayDeployer(client)

	err := gatewayDeployer.Delete(context.Background(), namespacedName)
	require.NoError(t, err)

	// Ensure the service and deployment are deleted
	var d appsv1.Deployment
	err = client.Get(context.Background(), types.NamespacedName{
		Namespace: gateway.Deployment().Namespace,
		Name:      gateway.Deployment().Name,
	}, &d)
	require.Error(t, err)

	var s corev1.Service
	err = client.Get(context.Background(), types.NamespacedName{
		Namespace: gateway.Service().Namespace,
		Name:      gateway.Service().Name,
	}, &s)
	require.Error(t, err)

	// Ensure the pod is not deleted
	var p corev1.Pod
	err = client.Get(context.Background(), types.NamespacedName{
		Namespace: podToNotDelete.Namespace,
		Name:      podToNotDelete.Name,
	}, &p)
	require.NoError(t, err)
}
