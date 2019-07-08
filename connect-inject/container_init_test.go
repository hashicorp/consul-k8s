package connectinject

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestHandlerContainerInit(t *testing.T) {
	minimal := func() *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					annotationService: "foo",
				},
			},

			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					corev1.Container{
						Name: "web",
					},

					corev1.Container{
						Name: "web-side",
					},
				},
			},
		}
	}

	cases := []struct {
		Name   string
		Pod    func(*corev1.Pod) *corev1.Pod
		Cmd    string // Strings.Contains test
		CmdNot string // Not contains
	}{
		{
			"Only service",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				return pod
			},
			`alias_service = "web"`,
			`upstreams`,
		},

		{
			"Service port specified",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				pod.Annotations[annotationPort] = "1234"
				return pod
			},
			"local_service_port = 1234",
			"",
		},

		{
			"Upstream",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				pod.Annotations[annotationUpstreams] = "db:1234"
				return pod
			},
			`destination_name = "db"`,
			"",
		},

		{
			"Upstream datacenter specified",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				pod.Annotations[annotationUpstreams] = "db:1234:dc1"
				return pod
			},
			`datacenter = "dc1"`,
			"",
		},

		{
			"No Upstream datacenter specified",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				pod.Annotations[annotationUpstreams] = "db:1234"
				return pod
			},
			"",
			`datacenter`,
		},
		{
			"Check Destination Type Query Annotation",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				pod.Annotations[annotationUpstreams] = "prepared_query:handle:1234"
				return pod
			},
			`destination_type = "prepared_query"`,
			`destination_type = "service"`,
		},

		{
			"Check Destination Name Query Annotation",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				pod.Annotations[annotationUpstreams] = "prepared_query:handle:1234"
				return pod
			},
			`destination_name = "handle"`,
			"",
		},

		{
			"Service ID set to POD_NAME env var",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				pod.Annotations[annotationUpstreams] = "db:1234"
				return pod
			},
			`id   = "${POD_NAME}-web-sidecar-proxy"`,
			"",
		},

		{
			"Proxy ID set to POD_NAME env var",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				pod.Annotations[annotationUpstreams] = "db:1234"
				return pod
			},
			`-proxy-id="${POD_NAME}-web-sidecar-proxy"`,
			"",
		},

		{
			"Tags specified",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				pod.Annotations[annotationUpstreams] = "db:1234:dc1"
				pod.Annotations[annotationTags] = "abc,123"
				return pod
			},
			`tags = ["abc","123"]`,
			"",
		},

		{
			"No Tags specified",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				return pod
			},
			"",
			`tags`,
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)

			var h Handler
			container, err := h.containerInit(tt.Pod(minimal()))
			require.NoError(err)
			actual := strings.Join(container.Command, " ")
			require.Contains(actual, tt.Cmd)
			if tt.CmdNot != "" {
				require.NotContains(actual, tt.CmdNot)
			}
		})
	}
}
