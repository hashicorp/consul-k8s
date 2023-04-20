package consul

import (
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul/api"
	consulAPI "github.com/hashicorp/consul/api"
)

const (
	metaKeyManagedBy       = "managed-by"
	metaValueManagedBy     = "consul-k8s-gateway-controller"
	metaKeyKubeNS          = "k8s-namespace"
	metaKeyKubeServiceName = "k8s-service-name"
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
				Kind:      consulAPI.APIGateway,
				Name:      k8sGW.Name,
				Namespace: k8sGW.Namespace,
			},
			LastTransitionTime: &condition.LastTransitionTime.Time,
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
					Namespace:   k8sGW.Namespace,
				},
				LastTransitionTime: &condition.LastTransitionTime.Time,
			}
			conditions = append(conditions, listenerCondition)
		}
	}

	return consulAPI.APIGatewayConfigEntry{
		Kind: api.APIGateway,
		Name: k8sGW.Name,
		Meta: map[string]string{
			metaKeyManagedBy:       metaValueManagedBy,
			metaKeyKubeNS:          k8sGW.GetObjectMeta().GetNamespace(),
			metaKeyKubeServiceName: k8sGW.GetObjectMeta().GetName(),
		},
		Listeners: listeners,
		Status: consulAPI.ConfigEntryStatus{
			Conditions: conditions,
		},
		Namespace: k8sGW.GetObjectMeta().GetNamespace(),
	}
}

func ptrTo[T any](v T) *T {
	return &v
}
