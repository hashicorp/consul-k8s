package consul

import (
	"os"
	"strings"

	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
	"github.com/hashicorp/consul/api"
	consulAPI "github.com/hashicorp/consul/api"
)

const (
	metaKeyManagedBy       = "managed-by"
	metaValueManagedBy     = "consul-k8s-gateway-controller"
	metaKeyKubeNS          = "k8s-namespace"
	metaKeyKubeServiceName = "k8s-service-name"

	AnnotationGateway = "consul.hashicorp.com/gateway"
)

type consulIdentifier struct {
	name      string
	namespace string
	partition string
}

type NamespaceName struct {
	Namespace string
	Name      string
}

type Translator struct {
	EnableConsulNamespaces bool
	ConsulDestNamespace    string
	EnableK8sMirroring     bool
	MirroringPrefix        string
}

func (t Translator) GatewayToAPIGateway(k8sGW gwv1beta1.Gateway, certs map[NamespaceName]consulIdentifier) consulAPI.APIGatewayConfigEntry {
	listeners := make([]consulAPI.APIGatewayListener, 0, len(k8sGW.Spec.Listeners))
	consulPartition := os.Getenv("CONSUL_PARTITION")
	for _, listener := range k8sGW.Spec.Listeners {
		certificates := make([]consulAPI.ResourceReference, 0, len(listener.TLS.CertificateRefs))
		for _, certificate := range listener.TLS.CertificateRefs {
			certRef, ok := certs[NamespaceName{Name: string(certificate.Name), Namespace: string(*certificate.Namespace)}]
			if !ok {
				// drop the ref from the created gateway
				continue
			}
			c := consulAPI.ResourceReference{
				Kind:      consulAPI.InlineCertificate,
				Name:      certRef.name,
				Partition: certRef.partition,
				Namespace: certRef.namespace,
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
	gwName := k8sGW.Name

	if gwNameFromAnnotation, ok := k8sGW.Annotations[AnnotationGateway]; ok && gwNameFromAnnotation != "" && !strings.Contains(gwNameFromAnnotation, ",") {
		gwName = gwNameFromAnnotation
	}

	return consulAPI.APIGatewayConfigEntry{
		Kind: api.APIGateway,
		Name: gwName,
		Meta: map[string]string{
			metaKeyManagedBy:       metaValueManagedBy,
			metaKeyKubeNS:          k8sGW.GetObjectMeta().GetNamespace(),
			metaKeyKubeServiceName: k8sGW.GetObjectMeta().GetName(),
		},
		Listeners: listeners,
		Partition: consulPartition,
		Namespace: namespaces.ConsulNamespace(k8sGW.GetObjectMeta().GetNamespace(), t.EnableK8sMirroring, t.ConsulDestNamespace, t.EnableK8sMirroring, t.MirroringPrefix),
	}
}

func ptrTo[T any](v T) *T {
	return &v
}
