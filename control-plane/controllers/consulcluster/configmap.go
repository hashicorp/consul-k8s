// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consulcluster

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

type serverConfig struct {
	// AutoReloadConfig lets Consul pick up the subset of settings it can reload
	// without a restart. The pod template also carries a checksum of this file,
	// so anything Consul cannot reload still rolls the servers.
	AutoReloadConfig bool            `json:"auto_reload_config"`
	Datacenter       string          `json:"datacenter"`
	Domain           string          `json:"domain"`
	Recursors        []string        `json:"recursors,omitempty"`
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
	Peering          peeringConfig   `json:"peering"`
	// EnableCentralServiceConfig is required for service-defaults and the rest
	// of the config-entry system to take effect.
	EnableCentralServiceConfig bool       `json:"enable_central_service_config"`
	ACL                        *aclConfig `json:"acl,omitempty"`
}

type peeringConfig struct {
	Enabled bool `json:"enabled"`
}

type aclConfig struct {
	Enabled                bool   `json:"enabled"`
	DefaultPolicy          string `json:"default_policy"`
	DownPolicy             string `json:"down_policy"`
	EnableTokenPersistence bool   `json:"enable_token_persistence"`
}

type connectConfig struct {
	Enabled bool `json:"enabled"`
}

type portConfig struct {
	HTTP    int `json:"http"`
	HTTPS   int `json:"https,omitempty"`
	GRPC    int `json:"grpc"`
	GRPCTLS int `json:"grpc_tls"`
	DNS     int `json:"dns"`
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
	CAFile               string `json:"ca_file,omitempty"`
	CertFile             string `json:"cert_file,omitempty"`
	KeyFile              string `json:"key_file,omitempty"`
	VerifyIncoming       bool   `json:"verify_incoming,omitempty"`
	VerifyOutgoing       bool   `json:"verify_outgoing,omitempty"`
	VerifyServerHostname bool   `json:"verify_server_hostname,omitempty"`
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

// ensureConfigMap writes the generated server config and returns a checksum of
// it. The checksum goes onto the pod template so that a config change produces a
// new StatefulSet revision and rolls the servers; Consul reloads only a subset
// of its settings at runtime, so a restart is the only way to guarantee a change
// takes effect.
func (r *ConsulClusterReconciler) ensureConfigMap(ctx context.Context, cluster *v1alpha1.ConsulCluster) (string, error) {
	desired, err := buildServerConfigJSON(cluster)
	if err != nil {
		return "", fmt.Errorf("building server config: %w", err)
	}

	sum := sha256.Sum256([]byte(desired))
	checksum := hex.EncodeToString(sum[:])

	name := configMapName(cluster)
	cm := &corev1.ConfigMap{}
	err = r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: name}, cm)
	if k8serrors.IsNotFound(err) {
		newCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:            name,
				Namespace:       cluster.Namespace,
				Labels:          serverPodLabels(cluster),
				OwnerReferences: []metav1.OwnerReference{ownerRef(cluster, r.Scheme)},
			},
			Data: map[string]string{"server.json": desired},
		}
		return checksum, r.Create(ctx, newCM)
	}
	if err != nil {
		return "", err
	}

	if cm.Data["server.json"] == desired {
		return checksum, nil
	}

	patch := client.MergeFrom(cm.DeepCopy())
	if cm.Data == nil {
		cm.Data = map[string]string{}
	}
	cm.Data["server.json"] = desired
	return checksum, r.Patch(ctx, cm, patch)
}

func buildServerConfigJSON(cluster *v1alpha1.ConsulCluster) (string, error) {
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
		AutoReloadConfig:           true,
		Datacenter:                 datacenterName(cluster),
		Domain:                     consulDomain(cluster),
		Recursors:                  cluster.Spec.Recursors,
		DataDir:                    "/consul/data",
		Server:                     true,
		BootstrapExpect:            bootstrapExpect,
		RetryJoin:                  []string{retryJoin},
		BindAddr:                   "0.0.0.0",
		ClientAddr:                 "0.0.0.0",
		LeaveOnTerminate:           true,
		LogLevel:                   cluster.Spec.LogLevel,
		EnableDebug:                cluster.Spec.EnableAgentDebug,
		Peering:                    peeringConfig{Enabled: true},
		EnableCentralServiceConfig: true,
		Ports: portConfig{
			HTTP: 8500,
			DNS:  8600,
			// gRPC is served either in plaintext or over TLS, never both. The
			// disabled one must be set to -1 rather than left unset, which
			// would default it back on.
			GRPC:    8502,
			GRPCTLS: -1,
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

	if cluster.Spec.ACLs != nil && cluster.Spec.ACLs.Enabled {
		cfg.ACL = &aclConfig{
			Enabled:                true,
			DefaultPolicy:          "deny",
			DownPolicy:             "extend-cache",
			EnableTokenPersistence: true,
		}
	}

	if tlsEnabled(cluster) {
		cfg.Ports.HTTPS = 8501
		cfg.Ports.GRPC = -1
		cfg.Ports.GRPCTLS = 8502
		cfg.TLS = &tlsConfig{
			Defaults: tlsDefaults{
				CAFile:         "/consul/tls/ca/tls.crt",
				CertFile:       "/consul/tls/server/tls.crt",
				KeyFile:        "/consul/tls/server/tls.key",
				VerifyOutgoing: true,
			},
			Internal: tlsDefaults{
				VerifyIncoming:       true,
				VerifyServerHostname: true,
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
