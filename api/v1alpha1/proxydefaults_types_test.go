package v1alpha1

import (
	"encoding/json"
	"testing"

	capi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// Test MatchesConsul for cases that should return true.
func TestProxyDefaults_MatchesConsulTrue(t *testing.T) {
	cases := map[string]struct {
		Ours   ProxyDefaults
		Theirs *capi.ProxyConfigEntry
	}{
		"empty fields": {
			Ours: ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: ProxyDefaultsSpec{},
			},
			Theirs: &capi.ProxyConfigEntry{
				Name: "global",
				Kind: capi.ProxyDefaults,
			},
		},
		"all fields set": {
			Ours: ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: ProxyDefaultsSpec{
					Config: json.RawMessage(`{"envoy_tracing_json": "{\"http\":{\"name\":\"envoy.zipkin\",\"config\":{\"collector_cluster\":\"zipkin\",\"collector_endpoint\":\"/api/v1/spans\",\"shared_span_context\":false}}}"}`),
					MeshGateway: MeshGatewayConfig{
						Mode: "local",
					},
					Expose: ExposeConfig{
						Checks: true,
						Paths: []ExposePath{
							{
								ListenerPort:  80,
								Path:          "/test/path",
								LocalPathPort: 42,
								Protocol:      "tcp",
							},
							{
								ListenerPort:  8080,
								Path:          "/root/test/path",
								LocalPathPort: 4201,
								Protocol:      "https",
							},
						},
					},
				},
			},
			Theirs: &capi.ProxyConfigEntry{
				Kind: capi.ProxyDefaults,
				Name: "global",
				Config: map[string]interface{}{
					"envoy_tracing_json": "{\"http\":{\"name\":\"envoy.zipkin\",\"config\":{\"collector_cluster\":\"zipkin\",\"collector_endpoint\":\"/api/v1/spans\",\"shared_span_context\":false}}}",
				},
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeLocal,
				},
				Expose: capi.ExposeConfig{
					Checks: true,
					Paths: []capi.ExposePath{
						{
							ListenerPort:  80,
							Path:          "/test/path",
							LocalPathPort: 42,
							Protocol:      "tcp",
						},
						{
							ListenerPort:  8080,
							Path:          "/root/test/path",
							LocalPathPort: 4201,
							Protocol:      "https",
						},
					},
				},
			},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			require.True(t, c.Ours.MatchesConsul(c.Theirs))
		})
	}
}

// Test MatchesConsul for cases that should return false.
func TestProxyDefaults_MatchesConsulFalse(t *testing.T) {
	cases := map[string]struct {
		Ours   ProxyDefaults
		Theirs capi.ConfigEntry
	}{
		"different type": {
			Ours: ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ProxyDefaultsSpec{},
			},
			Theirs: &capi.ServiceConfigEntry{
				Name: "name",
				Kind: capi.ProxyDefaults,
			},
		},
		"different name": {
			Ours: ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ProxyDefaultsSpec{},
			},
			Theirs: &capi.ProxyConfigEntry{
				Name: "other_name",
				Kind: capi.ProxyDefaults,
			},
		},
		"different mesh gateway mode": {
			Ours: ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ProxyDefaultsSpec{
					MeshGateway: MeshGatewayConfig{
						Mode: "local",
					},
				},
			},
			Theirs: &capi.ProxyConfigEntry{
				Kind: capi.ProxyDefaults,
				Name: "name",
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeRemote,
				},
			},
		},
		"our mesh gateway mode nil": {
			Ours: ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ProxyDefaultsSpec{},
			},
			Theirs: &capi.ProxyConfigEntry{
				Name: "name",
				Kind: capi.ProxyDefaults,
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeRemote,
				},
			},
		},
		"their mesh gateway mode nil": {
			Ours: ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ProxyDefaultsSpec{
					MeshGateway: MeshGatewayConfig{
						Mode: "remote",
					},
				},
			},
			Theirs: &capi.ProxyConfigEntry{
				Name: "name",
				Kind: capi.ProxyDefaults,
			},
		},
		"different expose config": {
			Ours: ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ProxyDefaultsSpec{
					Expose: ExposeConfig{
						Checks: true,
						Paths: []ExposePath{
							{
								ListenerPort:  80,
								Path:          "/path",
								LocalPathPort: 8081,
								Protocol:      "tcp",
							},
						},
					},
				},
			},
			Theirs: &capi.ProxyConfigEntry{
				Name: "name",
				Kind: capi.ProxyDefaults,
				Expose: capi.ExposeConfig{
					Checks: false,
					Paths: []capi.ExposePath{
						{
							ListenerPort:  8080,
							Path:          "/different-path",
							LocalPathPort: 9000,
							Protocol:      "http",
						},
					},
				},
			},
		},
		"their expose config nil": {
			Ours: ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ProxyDefaultsSpec{
					Expose: ExposeConfig{
						Checks: true,
						Paths: []ExposePath{
							{
								ListenerPort:  80,
								Path:          "/path",
								LocalPathPort: 8081,
								Protocol:      "tcp",
							},
						},
					},
				},
			},
			Theirs: &capi.ProxyConfigEntry{
				Name: "name",
				Kind: capi.ProxyDefaults,
			},
		},
		"our expose config nil": {
			Ours: ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ProxyDefaultsSpec{},
			},
			Theirs: &capi.ProxyConfigEntry{
				Name: "name",
				Kind: capi.ProxyDefaults,
				Expose: capi.ExposeConfig{
					Checks: false,
					Paths: []capi.ExposePath{
						{
							ListenerPort:  8080,
							Path:          "/different-path",
							LocalPathPort: 9000,
							Protocol:      "http",
						},
					},
				},
			},
		},
		"different expose config checks": {
			Ours: ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ProxyDefaultsSpec{
					Expose: ExposeConfig{
						Checks: true,
					},
				},
			},
			Theirs: &capi.ProxyConfigEntry{
				Name: "name",
				Kind: capi.ProxyDefaults,
				Expose: capi.ExposeConfig{
					Checks: false,
				},
			},
		},
		"different expose config path listener port": {
			Ours: ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ProxyDefaultsSpec{
					Expose: ExposeConfig{
						Paths: []ExposePath{
							{
								ListenerPort: 80,
							},
						},
					},
				},
			},
			Theirs: &capi.ProxyConfigEntry{
				Name: "name",
				Kind: capi.ProxyDefaults,
				Expose: capi.ExposeConfig{
					Paths: []capi.ExposePath{
						{
							ListenerPort: 8080,
						},
					},
				},
			},
		},
		"different expose config path path": {
			Ours: ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ProxyDefaultsSpec{
					Expose: ExposeConfig{
						Paths: []ExposePath{
							{
								Path: "/path",
							},
						},
					},
				},
			},
			Theirs: &capi.ProxyConfigEntry{
				Name: "name",
				Kind: capi.ProxyDefaults,
				Expose: capi.ExposeConfig{
					Paths: []capi.ExposePath{
						{
							Path: "/different-path",
						},
					},
				},
			},
		},
		"different expose config path local path port": {
			Ours: ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ProxyDefaultsSpec{
					Expose: ExposeConfig{
						Paths: []ExposePath{
							{
								LocalPathPort: 80,
							},
						},
					},
				},
			},
			Theirs: &capi.ProxyConfigEntry{
				Name: "name",
				Kind: capi.ProxyDefaults,
				Expose: capi.ExposeConfig{
					Paths: []capi.ExposePath{
						{
							LocalPathPort: 8080,
						},
					},
				},
			},
		},
		"different expose config path protocol": {
			Ours: ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ProxyDefaultsSpec{
					Expose: ExposeConfig{
						Paths: []ExposePath{
							{
								Protocol: "https",
							},
						},
					},
				},
			},
			Theirs: &capi.ProxyConfigEntry{
				Name: "name",
				Kind: capi.ProxyDefaults,
				Expose: capi.ExposeConfig{
					Paths: []capi.ExposePath{
						{
							Protocol: "tcp",
						},
					},
				},
			},
		},
		"different config": {
			Ours: ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ProxyDefaultsSpec{
					Config: json.RawMessage(`{"test_json": "{\"http\":{\"name\":\"envoy.zipkin\"}}"}`),
				},
			},
			Theirs: &capi.ProxyConfigEntry{
				Name: "name",
				Kind: capi.ProxyDefaults,
				Config: map[string]interface{}{
					"different_json": "{\"https\": {\"name\": \"different.name\"}}",
				},
			},
		},
		"our config nil": {
			Ours: ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ProxyDefaultsSpec{},
			},
			Theirs: &capi.ProxyConfigEntry{
				Name: "name",
				Kind: capi.ProxyDefaults,
				Config: map[string]interface{}{
					"different_json": "{\"https\": {\"name\": \"different.name\"}}",
				},
			},
		},
		"their config nil": {
			Ours: ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ProxyDefaultsSpec{
					Config: json.RawMessage(`{"test_json": "{\"http\":{\"name\":\"envoy.zipkin\"}}"}`),
				},
			},
			Theirs: &capi.ProxyConfigEntry{
				Name: "name",
				Kind: capi.ProxyDefaults,
			},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			require.False(t, c.Ours.MatchesConsul(c.Theirs))
		})
	}
}

func TestProxyDefaults_ToConsul(t *testing.T) {
	cases := map[string]struct {
		Ours ProxyDefaults
		Exp  *capi.ProxyConfigEntry
	}{
		"empty fields": {
			Ours: ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ProxyDefaultsSpec{},
			},
			Exp: &capi.ProxyConfigEntry{
				Name: "name",
				Kind: capi.ProxyDefaults,
			},
		},
		"every field set": {
			Ours: ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ProxyDefaultsSpec{
					Config: json.RawMessage(`{"envoy_tracing_json": "{\"http\":{\"name\":\"envoy.zipkin\",\"config\":{\"collector_cluster\":\"zipkin\",\"collector_endpoint\":\"/api/v1/spans\",\"shared_span_context\":false}}}"}`),
					MeshGateway: MeshGatewayConfig{
						Mode: "remote",
					},
					Expose: ExposeConfig{
						Checks: true,
						Paths: []ExposePath{
							{
								ListenerPort:  80,
								Path:          "/default",
								LocalPathPort: 9091,
								Protocol:      "tcp",
							},
							{
								ListenerPort:  8080,
								Path:          "/v2",
								LocalPathPort: 3001,
								Protocol:      "https",
							},
						},
					},
				},
			},
			Exp: &capi.ProxyConfigEntry{
				Kind:      capi.ProxyDefaults,
				Name:      "name",
				Namespace: "",
				Config: map[string]interface{}{
					"envoy_tracing_json": "{\"http\":{\"name\":\"envoy.zipkin\",\"config\":{\"collector_cluster\":\"zipkin\",\"collector_endpoint\":\"/api/v1/spans\",\"shared_span_context\":false}}}",
				},
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeRemote,
				},
				Expose: capi.ExposeConfig{
					Checks: true,
					Paths: []capi.ExposePath{
						{
							ListenerPort:  80,
							Path:          "/default",
							LocalPathPort: 9091,
							Protocol:      "tcp",
						},
						{
							ListenerPort:  8080,
							Path:          "/v2",
							LocalPathPort: 3001,
							Protocol:      "https",
						},
					},
				},
			},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			act := c.Ours.ToConsul()
			resolver, ok := act.(*capi.ProxyConfigEntry)
			require.True(t, ok, "could not cast")
			require.Equal(t, c.Exp, resolver)
		})
	}
}

func TestProxyDefaults_ValidateConfigValid(t *testing.T) {
	cases := map[string]json.RawMessage{
		"envoy_tracing_json":                     json.RawMessage(`{"envoy_tracing_json": "{\"http\":{\"name\":\"envoy.zipkin\",\"config\":{\"collector_cluster\":\"zipkin\",\"collector_endpoint\":\"/api/v1/spans\",\"shared_span_context\":false}}}"}`),
		"protocol":                               json.RawMessage(`{"protocol":  "http"}`),
		"members":                                json.RawMessage(`{"members":  3}`),
		"envoy_tracing_json & protocol":          json.RawMessage(`{"envoy_tracing_json": "{\"http\":{\"name\":\"envoy.zipkin\",\"config\":{\"collector_cluster\":\"zipkin\",\"collector_endpoint\":\"/api/v1/spans\",\"shared_span_context\":false}}}","protocol":  "http"}`),
		"envoy_tracing_json & members":           json.RawMessage(`{"envoy_tracing_json": "{\"http\":{\"name\":\"envoy.zipkin\",\"config\":{\"collector_cluster\":\"zipkin\",\"collector_endpoint\":\"/api/v1/spans\",\"shared_span_context\":false}}}","members":  3}`),
		"protocol & members":                     json.RawMessage(`{"protocol": "https","members":  3}`),
		"envoy_tracing_json, protocol & members": json.RawMessage(`{"envoy_tracing_json": "{\"http\":{\"name\":\"envoy.zipkin\",\"config\":{\"collector_cluster\":\"zipkin\",\"collector_endpoint\":\"/api/v1/spans\",\"shared_span_context\":false}}}","protocol":  "http", "members": 3}`),
	}
	for name, c := range cases {
		proxyDefaults := ProxyDefaults{
			ObjectMeta: metav1.ObjectMeta{
				Name: "global",
			},
			Spec: ProxyDefaultsSpec{
				Config: c,
			},
		}
		t.Run(name, func(t *testing.T) {
			require.Nil(t, proxyDefaults.validateConfig(nil))
		})
	}
}

func TestProxyDefaults_ValidateConfigInvalid(t *testing.T) {
	cases := map[string]json.RawMessage{
		"non_map json": json.RawMessage(`"{\"http\":{\"name\":\"envoy.zipkin\",\"config\":{\"collector_cluster\":\"zipkin\",\"collector_endpoint\":\"/api/v1/spans\",\"shared_span_context\":false}}}"`),
		"yaml":         json.RawMessage(`protocol: http`),
		"json array":   json.RawMessage(`[1,2,3,4]`),
		"json literal": json.RawMessage(`1`),
	}
	for name, c := range cases {
		proxyDefaults := ProxyDefaults{
			ObjectMeta: metav1.ObjectMeta{
				Name: "global",
			},
			Spec: ProxyDefaultsSpec{
				Config: c,
			},
		}
		t.Run(name, func(t *testing.T) {
			require.Equal(t, proxyDefaults.validateConfig(field.NewPath("spec")).Detail, "must be valid map value")
		})
	}
}

func TestProxyDefaults_AddFinalizer(t *testing.T) {
	resolver := &ProxyDefaults{}
	resolver.AddFinalizer("finalizer")
	require.Equal(t, []string{"finalizer"}, resolver.ObjectMeta.Finalizers)
}

func TestProxyDefaults_RemoveFinalizer(t *testing.T) {
	resolver := &ProxyDefaults{
		ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{"f1", "f2"},
		},
	}
	resolver.RemoveFinalizer("f1")
	require.Equal(t, []string{"f2"}, resolver.ObjectMeta.Finalizers)
}

func TestProxyDefaults_SetSyncedCondition(t *testing.T) {
	resolver := &ProxyDefaults{}
	resolver.SetSyncedCondition(corev1.ConditionTrue, "reason", "message")

	require.Equal(t, corev1.ConditionTrue, resolver.Status.Conditions[0].Status)
	require.Equal(t, "reason", resolver.Status.Conditions[0].Reason)
	require.Equal(t, "message", resolver.Status.Conditions[0].Message)
	now := metav1.Now()
	require.True(t, resolver.Status.Conditions[0].LastTransitionTime.Before(&now))
}

func TestProxyDefaults_GetSyncedConditionStatus(t *testing.T) {
	cases := []corev1.ConditionStatus{
		corev1.ConditionUnknown,
		corev1.ConditionFalse,
		corev1.ConditionTrue,
	}
	for _, status := range cases {
		t.Run(string(status), func(t *testing.T) {
			resolver := &ProxyDefaults{
				Status: Status{
					Conditions: []Condition{{
						Type:   ConditionSynced,
						Status: status,
					}},
				},
			}

			require.Equal(t, status, resolver.SyncedConditionStatus())
		})
	}
}

func TestProxyDefaults_GetConditionWhenStatusNil(t *testing.T) {
	require.Nil(t, (&ProxyDefaults{}).GetCondition(ConditionSynced))
}

func TestProxyDefaults_SyncedConditionStatusWhenStatusNil(t *testing.T) {
	require.Equal(t, corev1.ConditionUnknown, (&ProxyDefaults{}).SyncedConditionStatus())
}

func TestProxyDefaults_SyncedConditionWhenStatusNil(t *testing.T) {
	status, reason, message := (&ProxyDefaults{}).SyncedCondition()
	require.Equal(t, corev1.ConditionUnknown, status)
	require.Equal(t, "", reason)
	require.Equal(t, "", message)
}

func TestProxyDefaults_ConsulKind(t *testing.T) {
	require.Equal(t, capi.ProxyDefaults, (&ProxyDefaults{}).ConsulKind())
}

func TestProxyDefaults_KubeKind(t *testing.T) {
	require.Equal(t, "proxydefaults", (&ProxyDefaults{}).KubeKind())
}

func TestProxyDefaults_ObjectMeta(t *testing.T) {
	meta := metav1.ObjectMeta{
		Name:      "name",
		Namespace: "namespace",
	}
	proxyDefaults := &ProxyDefaults{
		ObjectMeta: meta,
	}
	require.Equal(t, meta, proxyDefaults.GetObjectMeta())
}
