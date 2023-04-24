package consul

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/google/go-cmp/cmp"

	"github.com/hashicorp/consul/api"
)

func TestGatewayToAPIGateway(t *testing.T) {
	k8sObjectName := "my-k8s-gw"
	k8sNamespace := "my-k8s-namespace"

	// gw status
	gwLastTransmissionTime := time.Now()

	// listener one configuration
	listenerOneName := "listener-one"
	listenerOneHostname := "*.consul.io"
	listenerOnePort := 3366
	listenerOneProtocol := "https"

	// listener one tls config
	listenerOneCertName := "one-cert"
	listenerOneCertK8sNamespace := "one-cert-k8s-ns"
	listenerOneCertConsulNamespace := "one-cert-consul-ns"

	// listener one status
	listenerOneLastTransmissionTime := time.Now()

	// listener two configuration
	listenerTwoName := "listener-two"
	listenerTwoHostname := "*.consul.io"
	listenerTwoPort := 5432
	listenerTwoProtocol := "https"

	// listener one tls config
	listenerTwoCertName := "two-cert"
	listenerTwoCertK8sNamespace := "two-cert-k8s-ns"
	listenerTwoCertConsulNamespace := "two-cert-consul-ns"

	// listener two status
	listenerTwoLastTransmissionTime := time.Now()

	testCases := map[string]struct {
		annotations            map[string]string
		expectedGWName         string
		listenerOneK8sCertRefs []gwv1beta1.SecretObjectReference
	}{
		"when gw name is not overriden by annotations": {
			annotations:    make(map[string]string),
			expectedGWName: k8sObjectName,
			listenerOneK8sCertRefs: []gwv1beta1.SecretObjectReference{
				{
					Name:      gwv1beta1.ObjectName(listenerOneCertName),
					Namespace: ptrTo(gwv1beta1.Namespace(listenerOneCertK8sNamespace)),
				},
			},
		},
		"when gw name is overriden by annotations": {
			annotations:    map[string]string{AnnotationGateway: "my-new-gw-name"},
			expectedGWName: "my-new-gw-name",
			listenerOneK8sCertRefs: []gwv1beta1.SecretObjectReference{
				{
					Name:      gwv1beta1.ObjectName(listenerOneCertName),
					Namespace: ptrTo(gwv1beta1.Namespace(listenerOneCertK8sNamespace)),
				},
			},
		},
		"when k8s has certs that are not referenced in consul": {
			annotations:    make(map[string]string),
			expectedGWName: k8sObjectName,
			listenerOneK8sCertRefs: []gwv1beta1.SecretObjectReference{
				{
					Name:      gwv1beta1.ObjectName(listenerOneCertName),
					Namespace: ptrTo(gwv1beta1.Namespace(listenerOneCertK8sNamespace)),
				},
				{
					Name:      gwv1beta1.ObjectName("cert that won't exist in the translated type"),
					Namespace: ptrTo(gwv1beta1.Namespace(listenerOneCertK8sNamespace)),
				},
			},
		},
	}

	for name, tc := range testCases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			input := gwv1beta1.Gateway{
				TypeMeta: metav1.TypeMeta{
					Kind: "Gateway",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:        k8sObjectName,
					Namespace:   k8sNamespace,
					Annotations: tc.annotations,
				},
				Spec: gwv1beta1.GatewaySpec{
					Listeners: []gwv1beta1.Listener{
						{
							Name:     gwv1beta1.SectionName(listenerOneName),
							Hostname: ptrTo(gwv1beta1.Hostname(listenerOneHostname)),
							Port:     gwv1beta1.PortNumber(listenerOnePort),
							Protocol: gwv1beta1.ProtocolType(listenerOneProtocol),
							TLS: &gwv1beta1.GatewayTLSConfig{
								CertificateRefs: tc.listenerOneK8sCertRefs,
							},
						},
						{
							Name:     gwv1beta1.SectionName(listenerTwoName),
							Hostname: ptrTo(gwv1beta1.Hostname(listenerTwoHostname)),
							Port:     gwv1beta1.PortNumber(listenerTwoPort),
							Protocol: gwv1beta1.ProtocolType(listenerTwoProtocol),
							TLS: &gwv1beta1.GatewayTLSConfig{
								CertificateRefs: []gwv1beta1.SecretObjectReference{
									{
										Name:      gwv1beta1.ObjectName(listenerTwoCertName),
										Namespace: ptrTo(gwv1beta1.Namespace(listenerTwoCertK8sNamespace)),
									},
								},
							},
						},
					},
				},
				Status: gwv1beta1.GatewayStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(gwv1beta1.GatewayConditionAccepted),
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Time{Time: gwLastTransmissionTime},
							Reason:             string(gwv1beta1.GatewayReasonAccepted),
							Message:            "I'm accepted",
						},
					},
					Listeners: []gwv1beta1.ListenerStatus{
						{
							Name:           gwv1beta1.SectionName(listenerOneName),
							AttachedRoutes: 5,
							Conditions: []metav1.Condition{
								{
									Type:               string(gwv1beta1.GatewayConditionReady),
									Status:             metav1.ConditionTrue,
									LastTransitionTime: metav1.Time{Time: listenerOneLastTransmissionTime},
									Reason:             string(gwv1beta1.GatewayConditionReady),
									Message:            "I'm ready",
								},
							},
						},

						{
							Name:           gwv1beta1.SectionName(listenerTwoName),
							AttachedRoutes: 3,
							Conditions: []metav1.Condition{
								{
									Type:               string(gwv1beta1.GatewayConditionReady),
									Status:             metav1.ConditionTrue,
									LastTransitionTime: metav1.Time{Time: listenerTwoLastTransmissionTime},
									Reason:             string(gwv1beta1.GatewayConditionReady),
									Message:            "I'm also ready",
								},
							},
						},
					},
				},
			}

			expectedConfigEntry := api.APIGatewayConfigEntry{
				Kind: api.APIGateway,
				Name: tc.expectedGWName,
				Meta: map[string]string{
					metaKeyManagedBy:       metaValueManagedBy,
					metaKeyKubeNS:          k8sNamespace,
					metaKeyKubeServiceName: k8sObjectName,
				},
				Listeners: []api.APIGatewayListener{
					{
						Name:     listenerOneName,
						Hostname: listenerOneHostname,
						Port:     listenerOnePort,
						Protocol: listenerOneProtocol,
						TLS: api.APIGatewayTLSConfiguration{
							Certificates: []api.ResourceReference{
								{
									Kind:      api.InlineCertificate,
									Name:      listenerOneCertName,
									Namespace: listenerOneCertConsulNamespace,
								},
							},
						},
					},
					{
						Name:     listenerTwoName,
						Hostname: listenerTwoHostname,
						Port:     listenerTwoPort,
						Protocol: listenerTwoProtocol,
						TLS: api.APIGatewayTLSConfiguration{
							Certificates: []api.ResourceReference{
								{
									Kind:      api.InlineCertificate,
									Name:      listenerTwoCertName,
									Namespace: listenerTwoCertConsulNamespace,
								},
							},
						},
					},
				},
				Status:    api.ConfigEntryStatus{},
				Namespace: k8sNamespace,
			}
			translator := Translator{
				EnableConsulNamespaces: true,
				ConsulDestNamespace:    "",
				EnableK8sMirroring:     true,
				MirroringPrefix:        "",
			}

			certs := map[types.NamespacedName]consulIdentifier{
				{Name: listenerOneCertName, Namespace: listenerOneCertK8sNamespace}: {
					name:      listenerOneCertName,
					namespace: listenerOneCertConsulNamespace,
				},
				{Name: listenerTwoCertName, Namespace: listenerTwoCertK8sNamespace}: {
					name:      listenerTwoCertName,
					namespace: listenerTwoCertConsulNamespace,
				},
			}

			actualConfigEntry := translator.GatewayToAPIGateway(input, certs)

			if diff := cmp.Diff(expectedConfigEntry, actualConfigEntry); diff != "" {
				t.Errorf("GatewayToAPIGateway() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
