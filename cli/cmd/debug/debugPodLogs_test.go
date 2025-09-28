package debug

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

// The check creates multiple resources(consul components) in multiple namespaces
// and assert the log collection based on given namespace.
func TestCapturePodLogs(t *testing.T) {
	cases := map[string]struct {
		namespace            string
		duration             time.Duration
		since                time.Duration
		expectedOutputBuffer []string
		fetchLogsFunc        func(ns string, podName string, opts *corev1.PodLogOptions) (io.ReadCloser, error)
		errorExpected        bool
	}{
		"test consul namespace": {
			namespace: "consul",
			duration:  10 * time.Second,
			fetchLogsFunc: func(ns string, podName string, opts *corev1.PodLogOptions) (io.ReadCloser, error) {
				logContent := fmt.Sprintf("Logs for pod %s in namespace %s\n", podName, ns)
				return io.NopCloser(bytes.NewReader([]byte(logContent))), nil
			},
			expectedOutputBuffer: []string{"Capturing pods logs.....", "Pods Logs captured"},
			errorExpected:        false,
		},
		"test another namespace": {
			namespace: "another",
			duration:  10 * time.Second,
			fetchLogsFunc: func(ns string, podName string, opts *corev1.PodLogOptions) (io.ReadCloser, error) {
				logContent := fmt.Sprintf("Logs for pod %s in namespace %s\n", podName, ns)
				return io.NopCloser(bytes.NewReader([]byte(logContent))), nil
			},
			expectedOutputBuffer: []string{"Capturing pods logs.....", "Pods Logs captured"},
			errorExpected:        false,
		},
		"test log collection with since": {
			namespace: "consul",
			since:     10 * time.Second,
			fetchLogsFunc: func(ns string, podName string, opts *corev1.PodLogOptions) (io.ReadCloser, error) {
				logContent := fmt.Sprintf("Logs for pod %s in namespace %s\n", podName, ns)
				return io.NopCloser(bytes.NewReader([]byte(logContent))), nil
			},
			expectedOutputBuffer: []string{"Capturing pods logs.....", "Pods Logs captured"},
			errorExpected:        false,
		},
		"log capture failure": {
			namespace: "consul",
			duration:  10 * time.Second,
			fetchLogsFunc: func(ns string, podName string, opts *corev1.PodLogOptions) (io.ReadCloser, error) {
				return nil, fmt.Errorf("testing log fetch error")
			},
			expectedOutputBuffer: []string{oneOrMoreErrorOccured.Error()},
			errorExpected:        true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			buf := new(bytes.Buffer)
			c := initializeDebugCommands(buf)
			l := &LogCapture{
				BaseCommand: c.BaseCommand,
				kubernetes:  fake.NewSimpleClientset(),
				output:      t.TempDir(),
				duration:    tc.duration,
				since:       tc.since,
				namespace:   tc.namespace,
				ctx:         c.Ctx,
			}
			err := createConsulResources(l.ctx, l.kubernetes)
			require.NoError(t, err, "failed to create consul resources")

			// Mock the log fetching function.
			l.fetchLogsFunc = tc.fetchLogsFunc

			err = l.captureLogs()

			if tc.errorExpected {
				require.Error(t, err, "expected error capturing pod logs")
				require.Contains(t, err.Error(), tc.expectedOutputBuffer[0], "expected err but mismatch")
				expectedErrorFile := filepath.Join(l.output, "logs", "logCaptureErrors.txt")
				_, statErr := os.Stat(expectedErrorFile)
				require.NoError(t, statErr, "expected error file to be created: %s", expectedErrorFile)
				errorContent, readErr := os.ReadFile(expectedErrorFile)
				require.NoError(t, readErr, "failed to read error file: %s", expectedErrorFile)
				require.Contains(t, string(errorContent), "testing log fetch error", "expected error messages in error file")
				return
			}

			require.NoError(t, err, "did not expect error capturing pod logs")
			actual := buf.String()
			require.Contains(t, actual, "Capturing pods logs.....")
			require.Contains(t, actual, "Pods Logs captured")

			// verify log files
			expectedContainers := []string{"init-container", "nginx-container"}
			baseLogPath := filepath.Join(l.output, "logs")

			for _, config := range getResourceConfigs() {
				// config.Component are: StatefulSet, DaemonSet, Deployment, Sidecar
				// kind are: statefulsets, daemonsets, deployments, sidecars
				kind := strings.ToLower(config.Component) + "s"
				for i := 0; i < int(config.Replicas); i++ {
					podName := fmt.Sprintf("%s-pod-%d", config.Name, i)
					for _, cont := range expectedContainers {
						// namespace aware assertion..
						// because file saved after log capture are not namespace aware
						if config.Namespace == tc.namespace || config.Component == "Sidecar" {
							expectedFilePaths := filepath.Join(baseLogPath, kind, config.Name, podName, cont+".log")
							_, err := os.Stat(expectedFilePaths)
							require.NoError(t, err, "expected log file to exist: %s", expectedFilePaths)
							expectedLog := fmt.Sprintf("Logs for pod %s in namespace %s\n", podName, config.Namespace)
							actualLog, err := os.ReadFile(expectedFilePaths)
							require.NoError(t, err, "failed to read log file: %s", expectedFilePaths)
							require.Equal(t, expectedLog, string(actualLog), "log content mismatch for file: %s", expectedFilePaths)
						}
					}
				}
			}
			t.Run("check audit log file", func(t *testing.T) {
				expectedAuditLogPath := filepath.Join(l.output, "logs", "logCaptureAudit.txt")
				_, err := os.Stat(expectedAuditLogPath)
				require.NoError(t, err, "expected audit log file to exist: %s", expectedAuditLogPath)
				auditLogContent, err := os.ReadFile(expectedAuditLogPath)
				require.NoError(t, err, "failed to read audit log file: %s", expectedAuditLogPath)
				auditLogStr := string(auditLogContent)

				require.NotContains(t, auditLogStr, "Failed", "did not expect failures in audit log")
			})
		})
	}
}

// ResourceConfig holds the parameters for creating a specific type of resource.
type ResourceConfig struct {
	Replicas  int32
	Labels    map[string]string
	Component string
	Name      string
	Namespace string
}

// getResourceConfigs returns a slice of ResourceConfig for creating fake k8 resources for testing.
func getResourceConfigs() []ResourceConfig {
	resourceConfigs := []ResourceConfig{
		{
			Replicas:  1,
			Labels:    map[string]string{"app": "consul", "chart": "consul-helm", "component": "server"},
			Component: "StatefulSet",
			Name:      "consul-server",
			Namespace: "consul",
		},
		{
			Replicas:  1,
			Labels:    map[string]string{"app": "consul", "chart": "consul-helm", "component": "server"},
			Component: "StatefulSet",
			Name:      "consul-server",
			Namespace: "another",
		},
		{
			Replicas:  2,
			Labels:    map[string]string{"app": "consul", "chart": "consul-helm", "component": "client"},
			Component: "DaemonSet",
			Name:      "consul-client",
			Namespace: "consul",
		},
		{
			Replicas:  1,
			Labels:    map[string]string{"app": "consul", "chart": "consul-helm", "component": "consul-deployment-1"},
			Component: "Deployment",
			Name:      "consul-deployment-1",
			Namespace: "consul",
		},
		{
			Replicas:  1,
			Labels:    map[string]string{"app": "consul", "chart": "consul-helm", "component": "consul-deployment-2"},
			Component: "Deployment",
			Name:      "consul-deployment-2",
			Namespace: "another",
		},
		{
			Replicas:  1,
			Labels:    map[string]string{"consul.hashicorp.com/connect-inject-status": "injected"},
			Component: "Sidecar",
			Name:      "sidecar",
			Namespace: "another",
		},
	}
	return resourceConfigs
}

// CreateConsulResources creates fake Kubernetes resources based on the provided configs.
func createConsulResources(ctx context.Context, k8sClient kubernetes.Interface) error {
	configs := getResourceConfigs()
	for _, config := range configs {
		switch config.Component {
		case "StatefulSet":
			ss := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: config.Name, Namespace: config.Namespace, Labels: config.Labels},
				Spec:       appsv1.StatefulSetSpec{Replicas: &config.Replicas, Selector: &metav1.LabelSelector{MatchLabels: config.Labels}},
			}
			if _, err := k8sClient.AppsV1().StatefulSets(config.Namespace).Create(ctx, ss, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("failed to create StatefulSet %s: %w", config.Name, err)
			}
		case "DaemonSet":
			ds := &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{Name: config.Name, Namespace: config.Namespace, Labels: config.Labels},
				Spec:       appsv1.DaemonSetSpec{Selector: &metav1.LabelSelector{MatchLabels: config.Labels}},
			}
			if _, err := k8sClient.AppsV1().DaemonSets(config.Namespace).Create(ctx, ds, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("failed to create DaemonSet %s: %w", config.Name, err)
			}
		case "Deployment":
			dep := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: config.Name, Namespace: config.Namespace, Labels: config.Labels},
				Spec:       appsv1.DeploymentSpec{Replicas: &config.Replicas, Selector: &metav1.LabelSelector{MatchLabels: config.Labels}},
			}
			if _, err := k8sClient.AppsV1().Deployments(config.Namespace).Create(ctx, dep, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("failed to create Deployment %s: %w", config.Name, err)
			}
		}

		// Create the associated pods for the resource.
		if err := createPodsForResource(ctx, k8sClient, config.Namespace, config.Name, config.Replicas, config.Labels); err != nil {
			return err
		}
	}
	return nil
}

// createPodsForResource is a helper to reduce duplication.
func createPodsForResource(ctx context.Context, k8sClient kubernetes.Interface, namespace, resourceName string, replicas int32, labels map[string]string) error {
	for i := 0; i < int(replicas); i++ {
		podName := fmt.Sprintf("%s-pod-%d", resourceName, i)
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: namespace, Labels: labels},
			Spec: corev1.PodSpec{
				InitContainers: []corev1.Container{{Name: "init-container", Image: "busybox:1.28"}},
				Containers:     []corev1.Container{{Name: "nginx-container", Image: "nginx:1.21.6"}},
			},
		}
		if _, err := k8sClient.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("failed to create Pod %s: %w", podName, err)
		}
	}
	return nil
}
