package connectinject

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
)

func TestHandlerConsulDataplaneSidecar(t *testing.T) {
	cases := map[string]struct {
		webhookSetupFunc     func(w *MeshWebhook)
		additionalExpCmdArgs string
	}{
		"default": {
			webhookSetupFunc: nil,
		},
		"with custom gRPC port": {
			webhookSetupFunc: func(w *MeshWebhook) {
				w.ConsulGRPCPort = 8602
			},
		},
		"with ACLs": {
			webhookSetupFunc: func(w *MeshWebhook) {
				w.AuthMethod = "test-auth-method"
			},
			additionalExpCmdArgs: " -static-token=$(cat /consul/connect-inject/acl-token)",
		},
		"with TLS and CA cert provided": {
			webhookSetupFunc: func(w *MeshWebhook) {
				w.TLSEnabled = true
				w.ConsulTLSServerName = "server.dc1.consul"
				w.ConsulCACert = "consul-ca-cert"
			},
			additionalExpCmdArgs: " -tls-enabled -tls-server-name=server.dc1.consul -tls-ca-certs-path=/consul/connect-inject/consul-ca.pem",
		},
		"with TLS and no CA cert provided": {
			webhookSetupFunc: func(w *MeshWebhook) {
				w.TLSEnabled = true
				w.ConsulTLSServerName = "server.dc1.consul"
			},
			additionalExpCmdArgs: " -tls-enabled -tls-server-name=server.dc1.consul",
		},
		"with single destination namespace": {
			webhookSetupFunc: func(w *MeshWebhook) {
				w.EnableNamespaces = true
				w.ConsulDestinationNamespace = "consul-namespace"
			},
			additionalExpCmdArgs: " -service-namespace=consul-namespace",
		},
		"with namespace mirroring": {
			webhookSetupFunc: func(w *MeshWebhook) {
				w.EnableNamespaces = true
				w.EnableK8SNSMirroring = true
			},
			additionalExpCmdArgs: " -service-namespace=k8snamespace",
		},
		"with namespace mirroring prefix": {
			webhookSetupFunc: func(w *MeshWebhook) {
				w.EnableNamespaces = true
				w.EnableK8SNSMirroring = true
				w.K8SNSMirroringPrefix = "foo-"
			},
			additionalExpCmdArgs: " -service-namespace=foo-k8snamespace",
		},
		"with partitions": {
			webhookSetupFunc: func(w *MeshWebhook) {
				w.ConsulPartition = "partition-1"
			},
			additionalExpCmdArgs: " -service-partition=partition-1",
		},
		"with different log level": {
			webhookSetupFunc: func(w *MeshWebhook) {
				w.LogLevel = "debug"
			},
		},
		"with different log level and log json": {
			webhookSetupFunc: func(w *MeshWebhook) {
				w.LogLevel = "debug"
				w.LogJSON = true
			},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := &MeshWebhook{
				ConsulAddress:  "1.1.1.1",
				ConsulGRPCPort: 8502,
				LogLevel:       "info",
				LogJSON:        false,
			}
			if c.webhookSetupFunc != nil {
				c.webhookSetupFunc(w)
			}
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Annotations: map[string]string{
						annotationService: "foo",
					},
				},

				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "web",
						},
					},
				},
			}

			container, err := w.consulDataplaneSidecar(testNS, pod, multiPortInfo{})
			require.NoError(t, err)
			// todo(agentless): test default concurrency
			expCmd := []string{
				"/bin/sh", "-ec",
				"consul-dataplane -addresses=1.1.1.1 -grpc-port=" + strconv.Itoa(w.ConsulGRPCPort) +
					" -proxy-service-id=$(cat /consul/connect-inject/proxyid) " +
					"-service-node-name=k8s-service-mesh -log-level=" + w.LogLevel + " -log-json=" + strconv.FormatBool(w.LogJSON) + c.additionalExpCmdArgs}
			require.Equal(t, container.Command, expCmd)

			require.Equal(t, container.VolumeMounts, []corev1.VolumeMount{
				{
					Name:      volumeName,
					MountPath: "/consul/connect-inject",
				},
			})
			expectedProbe := &corev1.Probe{
				Handler: corev1.Handler{
					TCPSocket: &corev1.TCPSocketAction{
						Port: intstr.FromInt(EnvoyInboundListenerPort),
					},
				},
				InitialDelaySeconds: 1,
			}
			require.Equal(t, expectedProbe, container.ReadinessProbe)
			require.Equal(t, expectedProbe, container.LivenessProbe)
			require.Nil(t, container.StartupProbe)
			require.Len(t, container.Env, 1)
			require.Equal(t, container.Env[0].Name, "TMPDIR")
			require.Equal(t, container.Env[0].Value, "/consul/connect-inject")
		})
	}
}

func TestHandlerConsulDataplaneSidecar_Concurrency(t *testing.T) {
	// todo(agentless): re-enable once we support passing extra flags to Envoy.
	t.Skipf("skip until we support extra flag to Envoy")
	cases := map[string]struct {
		annotations map[string]string
		expCommand  []string
		expErr      string
	}{
		"default settings, no annotations": {
			annotations: map[string]string{
				annotationService: "foo",
			},
			expCommand: []string{
				"envoy",
				"--config-path", "/consul/connect-inject/envoy-bootstrap.yaml",
				"--concurrency", "0",
			},
		},
		"default settings, annotation override": {
			annotations: map[string]string{
				annotationService:               "foo",
				annotationEnvoyProxyConcurrency: "42",
			},
			expCommand: []string{
				"envoy",
				"--config-path", "/consul/connect-inject/envoy-bootstrap.yaml",
				"--concurrency", "42",
			},
		},
		"default settings, invalid concurrency annotation negative number": {
			annotations: map[string]string{
				annotationService:               "foo",
				annotationEnvoyProxyConcurrency: "-42",
			},
			expErr: "invalid envoy concurrency, must be >= 0: -42",
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h := MeshWebhook{}
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: c.annotations,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "web",
						},
					},
				},
			}
			container, err := h.consulDataplaneSidecar(testNS, pod, multiPortInfo{})
			if c.expErr != "" {
				require.EqualError(t, err, c.expErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, c.expCommand, container.Command)
			}
		})
	}
}

func TestHandlerConsulDataplaneSidecar_Multiport(t *testing.T) {
	for _, aclsEnabled := range []bool{false, true} {
		name := fmt.Sprintf("acls enabled: %t", aclsEnabled)
		t.Run(name, func(t *testing.T) {
			w := MeshWebhook{
				ConsulAddress:  "1.1.1.1",
				ConsulGRPCPort: 8502,
				LogLevel:       "info",
			}
			if aclsEnabled {
				w.AuthMethod = "test-auth-method"
			}
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotationService: "web,web-admin",
					},
				},

				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "web",
						},
						{
							Name: "web-admin",
						},
					},
				},
			}
			multiPortInfos := []multiPortInfo{
				{
					serviceIndex: 0,
					serviceName:  "web",
				},
				{
					serviceIndex: 1,
					serviceName:  "web-admin",
				},
			}
			expCommand := [][]string{
				{"/bin/sh", "-ec", "consul-dataplane -addresses=1.1.1.1 -grpc-port=8502 -proxy-service-id=$(cat /consul/connect-inject/proxyid-web) -service-node-name=k8s-service-mesh -log-level=info -log-json=false -envoy-admin-bind-port=19000"},
				{"/bin/sh", "-ec", "consul-dataplane -addresses=1.1.1.1 -grpc-port=8502 -proxy-service-id=$(cat /consul/connect-inject/proxyid-web-admin) -service-node-name=k8s-service-mesh -log-level=info -log-json=false -envoy-admin-bind-port=19001"},
			}
			if aclsEnabled {
				expCommand = [][]string{
					{"/bin/sh", "-ec", "consul-dataplane -addresses=1.1.1.1 -grpc-port=8502 -proxy-service-id=$(cat /consul/connect-inject/proxyid-web) -service-node-name=k8s-service-mesh -log-level=info -log-json=false -static-token=$(cat /consul/connect-inject/acl-token-web) -envoy-admin-bind-port=19000"},
					{"/bin/sh", "-ec", "consul-dataplane -addresses=1.1.1.1 -grpc-port=8502 -proxy-service-id=$(cat /consul/connect-inject/proxyid-web-admin) -service-node-name=k8s-service-mesh -log-level=info -log-json=false -static-token=$(cat /consul/connect-inject/acl-token-web-admin) -envoy-admin-bind-port=19001"},
				}
			}

			for i, expCmd := range expCommand {
				container, err := w.consulDataplaneSidecar(testNS, pod, multiPortInfos[i])
				require.NoError(t, err)
				require.Equal(t, expCmd, container.Command)

				require.Equal(t, container.VolumeMounts, []corev1.VolumeMount{
					{
						Name:      volumeName,
						MountPath: "/consul/connect-inject",
					},
				})

				port := EnvoyInboundListenerPort + i
				expectedProbe := &corev1.Probe{
					Handler: corev1.Handler{
						TCPSocket: &corev1.TCPSocketAction{
							Port: intstr.FromInt(port),
						},
					},
					InitialDelaySeconds: 1,
				}
				require.Equal(t, expectedProbe, container.ReadinessProbe)
				require.Equal(t, expectedProbe, container.LivenessProbe)
				require.Nil(t, container.StartupProbe)
			}
		})
	}
}

func TestHandlerConsulDataplaneSidecar_withSecurityContext(t *testing.T) {
	cases := map[string]struct {
		tproxyEnabled      bool
		openShiftEnabled   bool
		expSecurityContext *corev1.SecurityContext
	}{
		"tproxy disabled; openshift disabled": {
			tproxyEnabled:    false,
			openShiftEnabled: false,
			expSecurityContext: &corev1.SecurityContext{
				RunAsUser:              pointer.Int64(sidecarUserAndGroupID),
				RunAsGroup:             pointer.Int64(sidecarUserAndGroupID),
				RunAsNonRoot:           pointer.Bool(true),
				ReadOnlyRootFilesystem: pointer.Bool(true),
			},
		},
		"tproxy enabled; openshift disabled": {
			tproxyEnabled:    true,
			openShiftEnabled: false,
			expSecurityContext: &corev1.SecurityContext{
				RunAsUser:              pointer.Int64(sidecarUserAndGroupID),
				RunAsGroup:             pointer.Int64(sidecarUserAndGroupID),
				RunAsNonRoot:           pointer.Bool(true),
				ReadOnlyRootFilesystem: pointer.Bool(true),
			},
		},
		"tproxy disabled; openshift enabled": {
			tproxyEnabled:      false,
			openShiftEnabled:   true,
			expSecurityContext: nil,
		},
		"tproxy enabled; openshift enabled": {
			tproxyEnabled:    true,
			openShiftEnabled: true,
			expSecurityContext: &corev1.SecurityContext{
				RunAsUser:              pointer.Int64(sidecarUserAndGroupID),
				RunAsGroup:             pointer.Int64(sidecarUserAndGroupID),
				RunAsNonRoot:           pointer.Bool(true),
				ReadOnlyRootFilesystem: pointer.Bool(true),
			},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := MeshWebhook{
				EnableTransparentProxy: c.tproxyEnabled,
				EnableOpenShift:        c.openShiftEnabled,
			}
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotationService: "foo",
					},
				},

				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "web",
						},
					},
				},
			}
			ec, err := w.consulDataplaneSidecar(testNS, pod, multiPortInfo{})
			require.NoError(t, err)
			require.Equal(t, c.expSecurityContext, ec.SecurityContext)
		})
	}
}

// Test that if the user specifies a pod security context with the same uid as `sidecarUserAndGroupID` that we return
// an error to the meshWebhook.
func TestHandlerConsulDataplaneSidecar_FailsWithDuplicatePodSecurityContextUID(t *testing.T) {
	require := require.New(t)
	w := MeshWebhook{}
	pod := corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "web",
				},
			},
			SecurityContext: &corev1.PodSecurityContext{
				RunAsUser: pointer.Int64(sidecarUserAndGroupID),
			},
		},
	}
	_, err := w.consulDataplaneSidecar(testNS, pod, multiPortInfo{})
	require.EqualError(err, fmt.Sprintf("pod's security context cannot have the same UID as consul-dataplane: %v", sidecarUserAndGroupID))
}

// Test that if the user specifies a container with security context with the same uid as `sidecarUserAndGroupID` that we
// return an error to the meshWebhook. If a container using the consul-dataplane image has the same uid, we don't return an error
// because in multiport pod there can be multiple consul-dataplane sidecars.
func TestHandlerConsulDataplaneSidecar_FailsWithDuplicateContainerSecurityContextUID(t *testing.T) {
	cases := []struct {
		name          string
		pod           corev1.Pod
		webhook       MeshWebhook
		expErr        bool
		expErrMessage string
	}{
		{
			name: "fails with non consul-dataplane image",
			pod: corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "web",
							// Setting RunAsUser: 1 should succeed.
							SecurityContext: &corev1.SecurityContext{
								RunAsUser: pointer.Int64(1),
							},
						},
						{
							Name: "app",
							// Setting RunAsUser: 5995 should fail.
							SecurityContext: &corev1.SecurityContext{
								RunAsUser: pointer.Int64(sidecarUserAndGroupID),
							},
							Image: "not-consul-dataplane",
						},
					},
				},
			},
			webhook:       MeshWebhook{},
			expErr:        true,
			expErrMessage: fmt.Sprintf("container \"app\" has runAsUser set to the same UID \"%d\" as consul-dataplane which is not allowed", sidecarUserAndGroupID),
		},
		{
			name: "doesn't fail with envoy image",
			pod: corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "web",
							// Setting RunAsUser: 1 should succeed.
							SecurityContext: &corev1.SecurityContext{
								RunAsUser: pointer.Int64(1),
							},
						},
						{
							Name: "sidecar",
							// Setting RunAsUser: 5995 should succeed if the image matches h.ImageConsulDataplane.
							SecurityContext: &corev1.SecurityContext{
								RunAsUser: pointer.Int64(sidecarUserAndGroupID),
							},
							Image: "envoy",
						},
					},
				},
			},
			webhook: MeshWebhook{
				ImageConsulDataplane: "envoy",
			},
			expErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.webhook.consulDataplaneSidecar(testNS, tc.pod, multiPortInfo{})
			if tc.expErr {
				require.EqualError(t, err, tc.expErrMessage)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// Test that we can pass extra args to envoy via the extraEnvoyArgs flag
// or via pod annotations. When arguments are passed in both ways, the
// arguments set via pod annotations are used.
func TestHandlerConsulDataplaneSidecar_EnvoyExtraArgs(t *testing.T) {
	// todo(agentless): enable when we support passing extra args to Envoy.
	t.Skipf("skip until we support passing extra args to Envoy")
	cases := []struct {
		name                     string
		envoyExtraArgs           string
		pod                      *corev1.Pod
		expectedContainerCommand []string
	}{
		{
			name:           "no extra options provided",
			envoyExtraArgs: "",
			pod:            &corev1.Pod{},
			expectedContainerCommand: []string{
				"envoy",
				"--config-path", "/consul/connect-inject/envoy-bootstrap.yaml",
				"--concurrency", "0",
			},
		},
		{
			name:           "via flag: extra log-level option",
			envoyExtraArgs: "--log-level debug",
			pod:            &corev1.Pod{},
			expectedContainerCommand: []string{
				"envoy",
				"--config-path", "/consul/connect-inject/envoy-bootstrap.yaml",
				"--concurrency", "0",
				"--log-level", "debug",
			},
		},
		{
			name:           "via flag: multiple arguments with quotes",
			envoyExtraArgs: "--log-level debug --admin-address-path \"/tmp/consul/foo bar\"",
			pod:            &corev1.Pod{},
			expectedContainerCommand: []string{
				"envoy",
				"--config-path", "/consul/connect-inject/envoy-bootstrap.yaml",
				"--concurrency", "0",
				"--log-level", "debug",
				"--admin-address-path", "\"/tmp/consul/foo bar\"",
			},
		},
		{
			name:           "via annotation: multiple arguments with quotes",
			envoyExtraArgs: "",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotationEnvoyExtraArgs: "--log-level debug --admin-address-path \"/tmp/consul/foo bar\"",
					},
				},
			},
			expectedContainerCommand: []string{
				"envoy",
				"--config-path", "/consul/connect-inject/envoy-bootstrap.yaml",
				"--concurrency", "0",
				"--log-level", "debug",
				"--admin-address-path", "\"/tmp/consul/foo bar\"",
			},
		},
		{
			name:           "via flag and annotation: should prefer setting via the annotation",
			envoyExtraArgs: "this should be overwritten",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotationEnvoyExtraArgs: "--log-level debug --admin-address-path \"/tmp/consul/foo bar\"",
					},
				},
			},
			expectedContainerCommand: []string{
				"envoy",
				"--config-path", "/consul/connect-inject/envoy-bootstrap.yaml",
				"--concurrency", "0",
				"--log-level", "debug",
				"--admin-address-path", "\"/tmp/consul/foo bar\"",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := MeshWebhook{
				ImageConsul:          "hashicorp/consul:latest",
				ImageConsulDataplane: "hashicorp/consul-k8s:latest",
				EnvoyExtraArgs:       tc.envoyExtraArgs,
			}

			c, err := h.consulDataplaneSidecar(testNS, *tc.pod, multiPortInfo{})
			require.NoError(t, err)
			require.Equal(t, tc.expectedContainerCommand, c.Command)
		})
	}
}

func TestHandlerConsulDataplaneSidecar_UserVolumeMounts(t *testing.T) {
	cases := []struct {
		name                          string
		pod                           corev1.Pod
		expectedContainerVolumeMounts []corev1.VolumeMount
		expErr                        string
	}{
		{
			name: "able to set a sidecar container volume mount via annotation",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotationEnvoyExtraArgs:               "--log-level debug --admin-address-path \"/tmp/consul/foo bar\"",
						annotationConsulSidecarUserVolumeMount: "[{\"name\": \"tls-cert\", \"mountPath\": \"/custom/path\"}, {\"name\": \"tls-ca\", \"mountPath\": \"/custom/path2\"}]",
					},
				},
			},
			expectedContainerVolumeMounts: []corev1.VolumeMount{
				{
					Name:      "consul-connect-inject-data",
					MountPath: "/consul/connect-inject",
				},
				{
					Name:      "tls-cert",
					MountPath: "/custom/path",
				},
				{
					Name:      "tls-ca",
					MountPath: "/custom/path2",
				},
			},
		},
		{
			name: "invalid annotation results in error",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotationEnvoyExtraArgs:               "--log-level debug --admin-address-path \"/tmp/consul/foo bar\"",
						annotationConsulSidecarUserVolumeMount: "[abcdefg]",
					},
				},
			},
			expErr: "invalid character 'a' looking ",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := MeshWebhook{
				ImageConsul:          "hashicorp/consul:latest",
				ImageConsulDataplane: "hashicorp/consul-k8s:latest",
			}
			c, err := h.consulDataplaneSidecar(testNS, tc.pod, multiPortInfo{})
			if tc.expErr == "" {
				require.NoError(t, err)
				require.Equal(t, tc.expectedContainerVolumeMounts, c.VolumeMounts)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expErr)
			}
		})
	}
}

func TestHandlerConsulDataplaneSidecar_Resources(t *testing.T) {
	mem1 := resource.MustParse("100Mi")
	mem2 := resource.MustParse("200Mi")
	cpu1 := resource.MustParse("100m")
	cpu2 := resource.MustParse("200m")
	zero := resource.MustParse("0")

	cases := map[string]struct {
		webhook      MeshWebhook
		annotations  map[string]string
		expResources corev1.ResourceRequirements
		expErr       string
	}{
		"no defaults, no annotations": {
			webhook:     MeshWebhook{},
			annotations: nil,
			expResources: corev1.ResourceRequirements{
				Limits:   corev1.ResourceList{},
				Requests: corev1.ResourceList{},
			},
		},
		"all defaults, no annotations": {
			webhook: MeshWebhook{
				DefaultProxyCPURequest:    cpu1,
				DefaultProxyCPULimit:      cpu2,
				DefaultProxyMemoryRequest: mem1,
				DefaultProxyMemoryLimit:   mem2,
			},
			annotations: nil,
			expResources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    cpu2,
					corev1.ResourceMemory: mem2,
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    cpu1,
					corev1.ResourceMemory: mem1,
				},
			},
		},
		"no defaults, all annotations": {
			webhook: MeshWebhook{},
			annotations: map[string]string{
				annotationSidecarProxyCPURequest:    "100m",
				annotationSidecarProxyMemoryRequest: "100Mi",
				annotationSidecarProxyCPULimit:      "200m",
				annotationSidecarProxyMemoryLimit:   "200Mi",
			},
			expResources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    cpu2,
					corev1.ResourceMemory: mem2,
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    cpu1,
					corev1.ResourceMemory: mem1,
				},
			},
		},
		"annotations override defaults": {
			webhook: MeshWebhook{
				DefaultProxyCPURequest:    zero,
				DefaultProxyCPULimit:      zero,
				DefaultProxyMemoryRequest: zero,
				DefaultProxyMemoryLimit:   zero,
			},
			annotations: map[string]string{
				annotationSidecarProxyCPURequest:    "100m",
				annotationSidecarProxyMemoryRequest: "100Mi",
				annotationSidecarProxyCPULimit:      "200m",
				annotationSidecarProxyMemoryLimit:   "200Mi",
			},
			expResources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    cpu2,
					corev1.ResourceMemory: mem2,
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    cpu1,
					corev1.ResourceMemory: mem1,
				},
			},
		},
		"defaults set to zero, no annotations": {
			webhook: MeshWebhook{
				DefaultProxyCPURequest:    zero,
				DefaultProxyCPULimit:      zero,
				DefaultProxyMemoryRequest: zero,
				DefaultProxyMemoryLimit:   zero,
			},
			annotations: nil,
			expResources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    zero,
					corev1.ResourceMemory: zero,
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    zero,
					corev1.ResourceMemory: zero,
				},
			},
		},
		"annotations set to 0": {
			webhook: MeshWebhook{},
			annotations: map[string]string{
				annotationSidecarProxyCPURequest:    "0",
				annotationSidecarProxyMemoryRequest: "0",
				annotationSidecarProxyCPULimit:      "0",
				annotationSidecarProxyMemoryLimit:   "0",
			},
			expResources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    zero,
					corev1.ResourceMemory: zero,
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    zero,
					corev1.ResourceMemory: zero,
				},
			},
		},
		"invalid cpu request": {
			webhook: MeshWebhook{},
			annotations: map[string]string{
				annotationSidecarProxyCPURequest: "invalid",
			},
			expErr: "parsing annotation consul.hashicorp.com/sidecar-proxy-cpu-request:\"invalid\": quantities must match the regular expression",
		},
		"invalid cpu limit": {
			webhook: MeshWebhook{},
			annotations: map[string]string{
				annotationSidecarProxyCPULimit: "invalid",
			},
			expErr: "parsing annotation consul.hashicorp.com/sidecar-proxy-cpu-limit:\"invalid\": quantities must match the regular expression",
		},
		"invalid memory request": {
			webhook: MeshWebhook{},
			annotations: map[string]string{
				annotationSidecarProxyMemoryRequest: "invalid",
			},
			expErr: "parsing annotation consul.hashicorp.com/sidecar-proxy-memory-request:\"invalid\": quantities must match the regular expression",
		},
		"invalid memory limit": {
			webhook: MeshWebhook{},
			annotations: map[string]string{
				annotationSidecarProxyMemoryLimit: "invalid",
			},
			expErr: "parsing annotation consul.hashicorp.com/sidecar-proxy-memory-limit:\"invalid\": quantities must match the regular expression",
		},
	}

	for name, c := range cases {
		t.Run(name, func(tt *testing.T) {
			require := require.New(tt)
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: c.annotations,
				},

				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "web",
						},
					},
				},
			}
			container, err := c.webhook.consulDataplaneSidecar(testNS, pod, multiPortInfo{})
			if c.expErr != "" {
				require.NotNil(err)
				require.Contains(err.Error(), c.expErr)
			} else {
				require.NoError(err)
				require.Equal(c.expResources, container.Resources)
			}
		})
	}
}
