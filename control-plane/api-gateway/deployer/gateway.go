package deployer

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Gateway struct {
	Name        string
	Namespace   string
	Image       string
	Replicas    int32
	Labels      map[string]string
	Annotations map[string]string
	Selector    map[string]string
}

func (d Gateway) Deployment() *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        d.Name,
			Namespace:   d.Namespace,
			Labels:      d.Labels,
			Annotations: d.Annotations, // Do I need to filter annotations?
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &d.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: d.Selector,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: d.Selector,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  d.Name,
							Image: d.Image,
						},
					},
				},
			},
		},
	}
}

func (d Gateway) Service() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        d.Name,
			Namespace:   d.Namespace,
			Labels:      d.Labels,
			Annotations: d.Annotations, // Do I need to filter annotations?
		},
		Spec: corev1.ServiceSpec{
			Selector: d.Selector,
		},
	}
}
