// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consulcluster

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

type serverConfig struct {
	Datacenter       string          `json:"datacenter"`
	DataDir          string          `json:"data_dir"`
	Server           bool            `json:"server"`
	BootstrapExpect  int             `json:"bootstrap_expect"`
	RetryJoin        []string        `json:"retry_join"`
	AdvertiseAddr    string          `json:"advertise_addr,omitempty"`
	BindAddr         string          `json:"bind_addr"`
	ClientAddr       string          `json:"client_addr"`
	LeaveOnTerminate bool            `json:"leave_on_terminate"`
	Connect          *connectConfig  `json:"connect,omitempty"`
	LogLevel         string          `json:"log_level,omitempty"`
	EnableDebug      bool            `json:"enable_debug,omitempty"`
	Ports            portConfig      `json:"ports"`
	Autopilot        autopilotConfig `json:"autopilot"`
	TLS              *tlsConfig      `json:"tls,omitempty"`
	UI               uiConfig        `json:"ui_config"`
	Telemetry        telemetryConfig `json:"telemetry"`
	Limits           limitsConfig    `json:"limits"`
}

type connectConfig struct {
	Enabled bool `json:"enabled"`
}

type portConfig struct {
	HTTP  int `json:"http"`
	HTTPS int `json:"https,omitempty"`
	GRPC  int `json:"grpc"`
	DNS   int `json:"dns"`
}

type autopilotConfig struct {
	MinQuorum               uint `json:"min_quorum"`
	DisableUpgradeMigration bool `json:"disable_upgrade_migration"`
}

type tlsConfig struct {
	Defaults tlsDefaults `json:"defaults"`
	Internal tlsDefaults `json:"internal_rpc"`
}

type tlsDefaults struct {
	CAFile         string `json:"ca_file,omitempty"`
	CertFile       string `json:"cert_file,omitempty"`
	KeyFile        string `json:"key_file,omitempty"`
	VerifyIncoming bool   `json:"verify_incoming,omitempty"`
}

type uiConfig struct {
	Enabled bool `json:"enabled"`
}

type telemetryConfig struct {
	PrometheusRetentionTime string `json:"prometheus_retention_time,omitempty"`
}

type limitsConfig struct {
	RequestLimits requestLimitsConfig `json:"request_limits,omitempty"`
}

type requestLimitsConfig struct {
	Mode      string  `json:"mode,omitempty"`
	ReadRate  float64 `json:"read_rate,omitempty"`
	WriteRate float64 `json:"write_rate,omitempty"`
}

func (r *ConsulClusterReconciler) ensureConfigMap(ctx context.Context, cluster *v1alpha1.ConsulCluster) error {
	desired, err := buildServerConfigJSON(cluster)
	if err != nil {
		return fmt.Errorf("building server config: %w", err)
	}

	name := configMapName(cluster)
	cm := &corev1.ConfigMap{}
	err = r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: name}, cm)
	if k8serrors.IsNotFound(err) {
		newCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: cluster.Namespace,
				Labels: map[string]string{
					labelApp:       labelAppValue,
					labelComponent: labelComponentValue,
					labelCluster:   cluster.Name,
				},
				OwnerReferences: []metav1.OwnerReference{ownerRef(cluster, r.Scheme)},
			},
			Data: map[string]string{"server.json": desired},
		}
		return r.Create(ctx, newCM)
	}
	if err != nil {
		return err
	}

	if cm.Data["server.json"] == desired {
		return nil
	}

	patch := client.MergeFrom(cm.DeepCopy())
	if cm.Data == nil {
		cm.Data = map[string]string{}
	}
	cm.Data["server.json"] = desired
	return r.Patch(ctx, cm, patch)
}

func buildServerConfigJSON(cluster *v1alpha1.ConsulCluster) (string, error) {
	datacenter := cluster.Spec.DatacenterName
	if datacenter == "" {
		datacenter = "dc1"
	}

	bootstrapExpect := cluster.Spec.Size
	if cluster.Spec.BootstrapExpect != nil {
		bootstrapExpect = *cluster.Spec.BootstrapExpect
	}

	retryJoin := fmt.Sprintf("%s.%s.svc", headlessServiceName(cluster), cluster.Namespace)

	minQuorum := uint(bootstrapExpect/2) + 1

	prometheusRetention := "60s"
	if cluster.Spec.Metrics != nil && cluster.Spec.Metrics.RetentionTime != "" {
		prometheusRetention = cluster.Spec.Metrics.RetentionTime
	}

	requestLimitsMode := "disabled"
	var readRate, writeRate float64
	if cluster.Spec.Limits != nil && cluster.Spec.Limits.RequestLimits != nil {
		rl := cluster.Spec.Limits.RequestLimits
		if rl.Mode != "" {
			requestLimitsMode = rl.Mode
		}
		readRate = rl.ReadRate
		writeRate = rl.WriteRate
	}

	cfg := serverConfig{
		Datacenter:       datacenter,
		DataDir:          "/consul/data",
		Server:           true,
		BootstrapExpect:  bootstrapExpect,
		RetryJoin:        []string{retryJoin},
		BindAddr:         "0.0.0.0",
		ClientAddr:       "0.0.0.0",
		LeaveOnTerminate: true,
		LogLevel:         cluster.Spec.LogLevel,
		EnableDebug:      cluster.Spec.EnableAgentDebug,
		Ports: portConfig{
			HTTP:  8500,
			HTTPS: 8501,
			GRPC:  8502,
			DNS:   8600,
		},
		Autopilot: autopilotConfig{
			MinQuorum:               minQuorum,
			DisableUpgradeMigration: true,
		},
		UI: uiConfig{Enabled: true},
		Telemetry: telemetryConfig{
			PrometheusRetentionTime: prometheusRetention,
		},
		Limits: limitsConfig{
			RequestLimits: requestLimitsConfig{
				Mode:      requestLimitsMode,
				ReadRate:  readRate,
				WriteRate: writeRate,
			},
		},
	}

	if cluster.Spec.Connect {
		cfg.Connect = &connectConfig{Enabled: true}
	}

	if cluster.Spec.TLS != nil && cluster.Spec.TLS.Enabled {
		cfg.TLS = &tlsConfig{
			Defaults: tlsDefaults{
				CAFile:         "/consul/tls/ca/tls.crt",
				CertFile:       "/consul/tls/server/tls.crt",
				KeyFile:        "/consul/tls/server/tls.key",
				VerifyIncoming: false,
			},
			Internal: tlsDefaults{
				VerifyIncoming: true,
			},
		}
		if cluster.Spec.TLS.HTTPSOnly {
			cfg.Ports.HTTP = -1
		}
	}

	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	result := string(b)

	if cluster.Spec.ExtraConfig != "" {
		result, err = mergeJSON(result, cluster.Spec.ExtraConfig)
		if err != nil {
			return "", fmt.Errorf("merging extraConfig: %w", err)
		}
	}

	return result, nil
}

func mergeJSON(base, extra string) (string, error) {
	// Unwrap if the value was accidentally double-encoded as a JSON string.
	if len(extra) >= 2 && extra[0] == '"' && extra[len(extra)-1] == '"' {
		var unwrapped string
		if err := json.Unmarshal([]byte(extra), &unwrapped); err == nil {
			extra = unwrapped
		}
	}
	var baseMap map[string]interface{}
	if err := json.Unmarshal([]byte(base), &baseMap); err != nil {
		return "", fmt.Errorf("parsing base config: %w", err)
	}
	var extraMap map[string]interface{}
	if err := json.Unmarshal([]byte(extra), &extraMap); err != nil {
		return "", fmt.Errorf("parsing extra config: %w", err)
	}
	for k, v := range extraMap {
		baseMap[k] = v
	}
	merged, err := json.MarshalIndent(baseMap, "", "  ")
	if err != nil {
		return "", err
	}
	return string(merged), nil
}
