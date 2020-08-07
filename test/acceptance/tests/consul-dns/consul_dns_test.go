package consuldns

import (
	"fmt"
	"testing"
	"time"

	"github.com/hashicorp/consul-helm/test/acceptance/framework"
	"github.com/hashicorp/consul-helm/test/acceptance/helpers"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const podName = "dns-pod"

func TestConsulDNS(t *testing.T) {
	env := suite.Environment()
	context := env.DefaultContext(t)
	releaseName := helpers.RandomName()

	cluster := framework.NewHelmCluster(t, nil, context, suite.Config(), releaseName)
	cluster.Create(t)

	k8sClient := context.KubernetesClient(t)
	contextNamespace := context.KubectlOptions().Namespace

	dnsService, err := k8sClient.CoreV1().Services(contextNamespace).Get(fmt.Sprintf("%s-%s", releaseName, "consul-dns"), metav1.GetOptions{})
	require.NoError(t, err)

	dnsIP := dnsService.Spec.ClusterIP

	createdDNSPod(t, releaseName, context)

	testDNS(t, context, dnsIP)
}

func createdDNSPod(t *testing.T, releaseName string, context framework.TestContext) {
	dnsPod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: podName,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "dns",
					Image:   "anubhavmishra/tiny-tools",
					Command: []string{"dig", fmt.Sprintf("@%s-consul-dns", releaseName), "consul.service.consul"},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}
	_, err := context.KubernetesClient(t).CoreV1().Pods(context.KubectlOptions().Namespace).Create(&dnsPod)
	require.NoError(t, err)

	helpers.Cleanup(t, suite.Config().NoCleanupOnFailure, func() {
		// Note: this delete command won't wait for pods to be fully terminated.
		// This shouldn't cause any test pollution because the underlying
		// objects are deployments, and so when other tests create these
		// they should have different pod names.
		helpers.RunKubectl(t, context.KubectlOptions(), "delete", "pod", "dns-pod")
	})
}

func testDNS(t *testing.T, context framework.TestContext, dnsIP string) {
	retrier := &retry.Timer{
		Timeout: 20 * time.Second,
		Wait:    500 * time.Millisecond,
	}
	retry.RunWith(retrier, t, func(r *retry.R) {
		logs, err := helpers.RunKubectlAndGetOutputE(t, context.KubectlOptions(), "logs", podName)
		require.NoError(r, err)

		require.Contains(r, logs, fmt.Sprintf("SERVER: %s", dnsIP))
		require.Contains(r, logs, "ANSWER SECTION:")
	})
}
