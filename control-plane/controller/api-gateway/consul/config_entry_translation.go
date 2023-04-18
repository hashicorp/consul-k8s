package consul

import (
	"time"

	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	consulAPI "github.com/hashicorp/consul/api"
)

func GatewayToAPIGateway(k8sGW gwv1beta1.Gateway) consulAPI.APIGatewayConfigEntry {
	listeners := make([]consulAPI.APIGatewayListener, 0, len(k8sGW.Spec.Listeners))
	conditions := make([]consulAPI.Condition, 0, len(k8sGW.Status.Conditions))
	for _, listener := range k8sGW.Spec.Listeners {
		certificates := make([]consulAPI.ResourceReference, 0, len(listener.TLS.CertificateRefs))
		for _, certificate := range listener.TLS.CertificateRefs {
			c := consulAPI.ResourceReference{
				Kind:        consulAPI.InlineCertificate,
				Name:        string(certificate.Name),
				SectionName: "",
				Partition:   "",
				Namespace:   string(*certificate.Namespace),
			}
			certificates = append(certificates, c)
		}
		l := consulAPI.APIGatewayListener{
			Name:     string(listener.Name),
			Hostname: string(*listener.Hostname),
			Port:     int(listener.Port),
			Protocol: string(listener.Protocol),
			TLS: consulAPI.APIGatewayTLSConfiguration{
				Certificates: certificates,
				MaxVersion:   "",
				MinVersion:   "",
				CipherSuites: []string{},
			},
		}

		listeners = append(listeners, l)
	}

	for _, condition := range k8sGW.Status.Conditions {
		// TODO: John Maguire - convert status/reason/type to use a mapping of values defined
		// in consul to convert from the k8s status/type/reason into consul status/types/reason
		c := consulAPI.Condition{
			Type:    condition.Type,
			Status:  string(condition.Status),
			Reason:  condition.Reason,
			Message: condition.Message,
			Resource: &consulAPI.ResourceReference{
				Kind:        k8sGW.Kind,
				Name:        k8sGW.Name,
				SectionName: "",
				Partition:   "",
				Namespace:   k8sGW.Namespace,
			},
			LastTransitionTime: &time.Time{},
		}
		conditions = append(conditions, c)
	}

	for _, listener := range k8sGW.Status.Listeners {
		for _, condition := range listener.Conditions {
			listenerCondition := consulAPI.Condition{
				Type:    condition.Type,
				Status:  string(condition.Status),
				Reason:  condition.Reason,
				Message: condition.Message,
				Resource: &consulAPI.ResourceReference{
					Kind:        consulAPI.APIGateway,
					Name:        k8sGW.Name,
					SectionName: string(listener.Name),
					Partition:   "",
					Namespace:   k8sGW.Namespace,
				},
				LastTransitionTime: ptrTo(time.Now().UTC()),
			}
			conditions = append(conditions, listenerCondition)
		}
	}

	return consulAPI.APIGatewayConfigEntry{
		Kind:      k8sGW.Kind,
		Name:      k8sGW.Name,
		Meta:      map[string]string{}, // TODO: what should go in here?
		Listeners: listeners,
		Status: consulAPI.ConfigEntryStatus{
			Conditions: conditions,
		},
		CreateIndex: 0,
		ModifyIndex: 0,
		Partition:   "",
		Namespace:   "",
	}

	// _ = gwv1beta1.Gateway{
	// 	TypeMeta: metav1.TypeMeta{
	// 		Kind:       "",
	// 		APIVersion: "",
	// 	},
	// 	ObjectMeta: metav1.ObjectMeta{
	// 		Name:                       "",
	// 		GenerateName:               "",
	// 		Namespace:                  "",
	// 		SelfLink:                   "",
	// 		UID:                        "",
	// 		ResourceVersion:            "",
	// 		Generation:                 0,
	// 		CreationTimestamp:          metav1.Time{},
	// 		DeletionTimestamp:          &metav1.Time{},
	// 		DeletionGracePeriodSeconds: new(int64),
	// 		Labels:                     map[string]string{},
	// 		Annotations:                map[string]string{},
	// 		OwnerReferences: []metav1.OwnerReference{
	// 			{
	// 				APIVersion:         "",
	// 				Kind:               "",
	// 				Name:               "",
	// 				UID:                "",
	// 				Controller:         new(bool),
	// 				BlockOwnerDeletion: new(bool),
	// 			},
	// 		},
	// 		Finalizers: []string{},
	// 		ManagedFields: []metav1.ManagedFieldsEntry{
	// 			{
	// 				Manager:     "",
	// 				Operation:   "",
	// 				APIVersion:  "",
	// 				Time:        &metav1.Time{},
	// 				FieldsType:  "",
	// 				FieldsV1:    &metav1.FieldsV1{},
	// 				Subresource: "",
	// 			},
	// 		},
	// 	},
	// 	Spec: gwv1beta1.GatewaySpec{
	// 		GatewayClassName: "",
	// 		Listeners: []gwv1beta1.Listener{
	// 			{
	// 				Name:     "",
	// 				Hostname: new(gwv1beta1.Hostname),
	// 				Port:     0,
	// 				Protocol: "",
	// 				TLS: &gwv1beta1.GatewayTLSConfig{
	// 					Mode: new(gwv1beta1.TLSModeType),
	// 					CertificateRefs: []gwv1beta1.SecretObjectReference{
	// 						{
	// 							Group:     &"",
	// 							Kind:      &"",
	// 							Name:      "",
	// 							Namespace: &"",
	// 						},
	// 					},
	// 					Options: map[gwv1beta1.AnnotationKey]gwv1beta1.AnnotationValue{},
	// 				},
	// 				AllowedRoutes: &gwv1beta1.AllowedRoutes{},
	// 			},
	// 		},
	// 		Addresses: []gwv1beta1.GatewayAddress{},
	// 	},
	// 	Status: gwv1beta1.GatewayStatus{
	// 		Addresses: []gwv1beta1.GatewayAddress{
	// 			{
	// 				Type:  &"",
	// 				Value: "",
	// 			},
	// 		},
	// 		Conditions: []metav1.Condition{
	// 			{
	// 				Type:               "",
	// 				Status:             "",
	// 				ObservedGeneration: 0,
	// 				LastTransitionTime: metav1.Time{},
	// 				Reason:             "",
	// 				Message:            "",
	// 			},
	// 		},
	// 		Listeners: []gwv1beta1.ListenerStatus{
	// 			{
	// 				Name: "",
	// 				SupportedKinds: []gwv1beta1.RouteGroupKind{
	// 					{
	// 						Group: &"",
	// 						Kind:  "",
	// 					},
	// 				},
	// 				AttachedRoutes: 0,
	// 				Conditions: []metav1.Condition{
	// 					{
	// 						Type:               "",
	// 						Status:             "",
	// 						ObservedGeneration: 0,
	// 						LastTransitionTime: metav1.Time{},
	// 						Reason:             "",
	// 						Message:            "",
	// 					},
	// 				},
	// 			},
	// 		},
	// 	},
	// }
}

func ptrTo[T any](v T) *T {
	return &v
}
