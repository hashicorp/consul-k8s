package connectinject

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestContainerEnvVars(t *testing.T) {
	serviceHost := "static-server"
	servicePort := "7890"
	upstreamAnnotation := fmt.Sprintf("%s:%s:dc2", serviceHost, servicePort)

	minimal := func() *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					annotationService: "foo",
				},
			},
		}
	}

	cases := []struct {
		Name        string
		Pod         func(*corev1.Pod) *corev1.Pod
		ServiceHost string // Service host name used to craft env var names
		ServicePort string // Service port that should be set to env var
	}{
		// The test checks that upstream annotated pods with a datacenter segment
		// can be properly parsed into serviceHost and port environment variables.
		{
			"Upstream with datacenter",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationUpstreams] = upstreamAnnotation
				return pod
			},
			serviceHost,
			servicePort,
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)

			var h Handler
			envVars := h.containerEnvVars(tt.Pod(minimal()))

			hostEntryFound := false
			portEntryFound := false
			portCorrectlySet := false
			// TODO: Consider extracting functions for the format calls to ensure no
			// implementation drift between tests and function under test.
			formattedServiceHost := strings.ToUpper(strings.Replace(tt.ServiceHost, "-", "_", -1))

			for i := range envVars {
				if envVars[i].Name == fmt.Sprintf("%s_CONNECT_SERVICE_HOST", formattedServiceHost) {
					hostEntryFound = true
				}
				if envVars[i].Name == fmt.Sprintf("%s_CONNECT_SERVICE_PORT", formattedServiceHost) {
					portEntryFound = true
					if envVars[i].Value == tt.ServicePort {
						portCorrectlySet = true
					}
				}
			}

			require.True(hostEntryFound)
			require.True(portEntryFound)
			require.True(portCorrectlySet)
		})
	}
}
