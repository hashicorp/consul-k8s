// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package flags

import (
	"crypto/tls"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-server-connection-manager/discovery"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
)

func TestConsulFlags_Flags(t *testing.T) {
	cases := map[string]struct {
		env      map[string]string
		expFlags *ConsulFlags
	}{
		"env vars": {
			env: map[string]string{
				AddressesEnvVar:  "consul.address",
				GRPCPortEnvVar:   "8503",
				HTTPPortEnvVar:   "8501",
				NamespaceEnvVar:  "test-ns",
				PartitionEnvVar:  "test-partition",
				DatacenterEnvVar: "test-dc",
				APITimeoutEnvVar: "10s",

				constants.UseTLSEnvVar:        "true",
				constants.CACertFileEnvVar:    "path/to/ca.pem",
				constants.CACertPEMEnvVar:     "test-ca-pem",
				constants.TLSServerNameEnvVar: "server.consul",

				ACLTokenEnvVar:             "test-token",
				ACLTokenFileEnvVar:         "/path/to/token",
				LoginAuthMethodEnvVar:      "test-auth-method",
				LoginBearerTokenFileEnvVar: "path/to/token",
				LoginDatacenterEnvVar:      "other-test-dc",
				LoginPartitionEnvVar:       "other-test-partition",
				LoginNamespaceEnvVar:       "other-test-ns",
				LoginMetaEnvVar:            "key1=value1,key2=value2",
				SkipServerWatchEnvVar:      "true",
			},
			expFlags: &ConsulFlags{
				Addresses:  "consul.address",
				GRPCPort:   8503,
				HTTPPort:   8501,
				Namespace:  "test-ns",
				Partition:  "test-partition",
				Datacenter: "test-dc",
				APITimeout: 10 * time.Second,
				ConsulTLSFlags: ConsulTLSFlags{
					UseTLS:        true,
					CACertFile:    "path/to/ca.pem",
					CACertPEM:     "test-ca-pem",
					TLSServerName: "server.consul",
				},
				ConsulACLFlags: ConsulACLFlags{
					Token:     "test-token",
					TokenFile: "/path/to/token",
					ConsulLogin: ConsulLoginFlags{
						AuthMethod:      "test-auth-method",
						BearerTokenFile: "path/to/token",
						Datacenter:      "other-test-dc",
						Partition:       "other-test-partition",
						Namespace:       "other-test-ns",
						Meta:            map[string]string{"key1": "value1", "key2": "value2"},
					},
				},
				SkipServerWatch: true,
			},
		},
		"defaults": {
			expFlags: &ConsulFlags{
				APITimeout: 5 * time.Second,
				ConsulACLFlags: ConsulACLFlags{
					ConsulLogin: ConsulLoginFlags{
						BearerTokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token",
					},
				},
			},
		},
		"ignore invalid env vars": {
			env: map[string]string{
				GRPCPortEnvVar:   "not-int-grpc-port",
				HTTPPortEnvVar:   "not-int-http-port",
				APITimeoutEnvVar: "10sec",

				constants.UseTLSEnvVar: "not-a-bool",

				LoginMetaEnvVar: "key1:value1;key2:value2",
			},
			expFlags: &ConsulFlags{
				APITimeout: 5 * time.Second,
				ConsulACLFlags: ConsulACLFlags{
					ConsulLogin: ConsulLoginFlags{
						BearerTokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token",
					},
				},
			},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			for k, v := range c.env {
				err := os.Setenv(k, v)
				require.NoError(t, err)
			}
			t.Cleanup(func() {
				for k := range c.env {
					_ = os.Unsetenv(k)
				}
			})

			cf := &ConsulFlags{}
			consulFlags := cf.Flags()
			err := consulFlags.Parse(nil)
			require.NoError(t, err)
			require.Equal(t, c.expFlags, cf)
		})
	}
}

func TestConsulFlags_ConsulServerConnMgrConfig(t *testing.T) {
	cases := map[string]struct {
		flags     ConsulFlags
		expConfig discovery.Config
	}{
		"basic flags without TLS or ACLs": {
			flags: ConsulFlags{
				Addresses: "consul.address",
				GRPCPort:  8502,
			},
			expConfig: discovery.Config{
				Addresses: "consul.address",
				GRPCPort:  8502,
			},
		},
		"default TLS": {
			flags: ConsulFlags{
				Addresses: "consul.address",
				ConsulTLSFlags: ConsulTLSFlags{
					UseTLS: true,
				},
			},
			expConfig: discovery.Config{
				Addresses: "consul.address",
				TLS:       &tls.Config{},
			},
		},
		"ACL Auth method": {
			flags: ConsulFlags{
				Addresses: "consul.address",
				ConsulACLFlags: ConsulACLFlags{
					ConsulLogin: ConsulLoginFlags{
						AuthMethod: "test-auth-method",
						Namespace:  "test-ns",
						Partition:  "test-partition",
						Datacenter: "test-dc",
						Meta:       map[string]string{"key1": "value1", "key2": "value2"},
					},
				},
			},
			expConfig: discovery.Config{
				Addresses: "consul.address",
				Credentials: discovery.Credentials{
					Type: discovery.CredentialsTypeLogin,
					Login: discovery.LoginCredential{
						AuthMethod:  "test-auth-method",
						Namespace:   "test-ns",
						Partition:   "test-partition",
						Datacenter:  "test-dc",
						BearerToken: "bearer-token",
						Meta:        map[string]string{"key1": "value1", "key2": "value2"},
					},
				},
			},
		},
		"Static ACL token": {
			flags: ConsulFlags{
				Addresses: "consul.address",
				ConsulACLFlags: ConsulACLFlags{
					Token: "test-token",
				},
			},
			expConfig: discovery.Config{
				Addresses: "consul.address",
				Credentials: discovery.Credentials{
					Type: discovery.CredentialsTypeStatic,
					Static: discovery.StaticTokenCredential{
						Token: "test-token",
					},
				},
			},
		},
		"Static ACL token file": {
			flags: ConsulFlags{
				Addresses: "consul.address",
				ConsulACLFlags: ConsulACLFlags{
					// This is the content of the token that we will
					// write to a temp file and expect the config to have this in its contents
					TokenFile: "test-token",
				},
			},
			expConfig: discovery.Config{
				Addresses: "consul.address",
				Credentials: discovery.Credentials{
					Type: discovery.CredentialsTypeStatic,
					Static: discovery.StaticTokenCredential{
						Token: "test-token",
					},
				},
			},
		},
		"skip server watch to server watch disabled": {
			flags: ConsulFlags{
				Addresses:       "consul.address",
				GRPCPort:        8502,
				SkipServerWatch: true,
			},
			expConfig: discovery.Config{
				Addresses:           "consul.address",
				GRPCPort:            8502,
				ServerWatchDisabled: true,
			},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if c.flags.ConsulLogin.AuthMethod != "" {
				tokenFile, err := os.CreateTemp("", "")
				require.NoError(t, err)
				t.Cleanup(func() {
					_ = os.RemoveAll(tokenFile.Name())
				})
				_, err = tokenFile.WriteString("bearer-token")
				require.NoError(t, err)
				c.flags.ConsulLogin.BearerTokenFile = tokenFile.Name()
			} else if c.flags.TokenFile != "" {
				tokenFile, err := os.CreateTemp("", "")
				require.NoError(t, err)
				t.Cleanup(func() {
					_ = os.RemoveAll(tokenFile.Name())
				})
				_, err = tokenFile.WriteString(c.flags.TokenFile)
				require.NoError(t, err)
				c.flags.TokenFile = tokenFile.Name()
			}
			cfg, err := c.flags.ConsulServerConnMgrConfig()
			require.NoError(t, err)
			require.Equal(t, c.expConfig, cfg)
		})
	}
}

func TestConsulFlags_ConsulServerConnMgrConfig_TLS(t *testing.T) {
	caFile, err := os.CreateTemp("", "")
	t.Cleanup(func() {
		_ = os.RemoveAll(caFile.Name())
	})
	require.NoError(t, err)
	_, err = caFile.WriteString(testCA)
	require.NoError(t, err)

	cases := map[string]struct {
		flags ConsulFlags
	}{
		"default TLS": {
			flags: ConsulFlags{
				Addresses: "consul.address",
				ConsulTLSFlags: ConsulTLSFlags{
					UseTLS: true,
				},
			},
		},
		"TLS with CA File": {
			flags: ConsulFlags{
				Addresses: "consul.address",
				ConsulTLSFlags: ConsulTLSFlags{
					UseTLS:     true,
					CACertFile: caFile.Name(),
				},
			},
		},
		"TLS with CA Pem": {
			flags: ConsulFlags{
				Addresses: "consul.address",
				ConsulTLSFlags: ConsulTLSFlags{
					UseTLS:    true,
					CACertPEM: testCA,
				},
			},
		},
		"TLS server name": {
			flags: ConsulFlags{
				Addresses: "consul.address",
				ConsulTLSFlags: ConsulTLSFlags{
					UseTLS:        true,
					TLSServerName: "server.consul",
				},
			},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, err := c.flags.ConsulServerConnMgrConfig()
			require.NoError(t, err)
			require.NotNil(t, cfg.TLS)
			if c.flags.CACertFile != "" || c.flags.CACertPEM != "" {
				require.NotNil(t, cfg.TLS.RootCAs)
			}
			require.Equal(t, c.flags.TLSServerName, cfg.TLS.ServerName)
		})
	}
}

func TestConsulFlags_ConsulAPIClientConfig(t *testing.T) {
	cases := map[string]struct {
		flags     ConsulFlags
		expConfig *api.Config
	}{
		"basic config": {
			flags: ConsulFlags{
				Namespace:  "test-ns",
				Partition:  "test-partition",
				Datacenter: "test-dc",
			},
			expConfig: &api.Config{
				Namespace:  "test-ns",
				Partition:  "test-partition",
				Datacenter: "test-dc",
				Scheme:     "http",
			},
		},
		"with TLS": {
			flags: ConsulFlags{
				ConsulTLSFlags: ConsulTLSFlags{
					UseTLS: true,
				},
			},
			expConfig: &api.Config{
				Scheme: "https",
			},
		},
		"TLS: infer TLS server name when addresses is not an executable": {
			flags: ConsulFlags{
				Addresses: "consul",
				ConsulTLSFlags: ConsulTLSFlags{
					UseTLS: true,
				},
			},
			expConfig: &api.Config{
				Scheme: "https",
				TLSConfig: api.TLSConfig{
					Address: "consul",
				},
			},
		},
		"TLS: doesn't infer TLS server name when addresses is an executable": {
			flags: ConsulFlags{
				Addresses: "exec=echo 1.1.1.1",
				ConsulTLSFlags: ConsulTLSFlags{
					UseTLS: true,
				},
			},
			expConfig: &api.Config{
				Scheme: "https",
			},
		},
		"TLS CA File provided": {
			flags: ConsulFlags{
				ConsulTLSFlags: ConsulTLSFlags{
					UseTLS:     true,
					CACertFile: "path/to/ca",
				},
			},
			expConfig: &api.Config{
				Scheme: "https",
				TLSConfig: api.TLSConfig{
					CAFile: "path/to/ca",
				},
			},
		},
		"TLS CA PEM provided": {
			flags: ConsulFlags{
				ConsulTLSFlags: ConsulTLSFlags{
					UseTLS:    true,
					CACertPEM: testCA,
				},
			},
			expConfig: &api.Config{
				Scheme: "https",
				TLSConfig: api.TLSConfig{
					CAPem: []byte(testCA),
				},
			},
		},
		"ACL token provided": {
			flags: ConsulFlags{
				ConsulACLFlags: ConsulACLFlags{
					Token: "test-token",
				},
			},
			expConfig: &api.Config{
				Scheme: "http",
				Token:  "test-token",
			},
		},
		"ACL token file provided": {
			flags: ConsulFlags{
				ConsulACLFlags: ConsulACLFlags{
					TokenFile: "/path/to/token",
				},
			},
			expConfig: &api.Config{
				Scheme:    "http",
				TokenFile: "/path/to/token",
			},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, c.expConfig, c.flags.ConsulClientConfig().APIClientConfig)
		})
	}
}

const testCA = `
-----BEGIN CERTIFICATE-----
MIIC7TCCApOgAwIBAgIQbHoocPoQq7qR3MTNUXdLVDAKBggqhkjOPQQDAjCBuTEL
MAkGA1UEBhMCVVMxCzAJBgNVBAgTAkNBMRYwFAYDVQQHEw1TYW4gRnJhbmNpc2Nv
MRowGAYDVQQJExExMDEgU2Vjb25kIFN0cmVldDEOMAwGA1UEERMFOTQxMDUxFzAV
BgNVBAoTDkhhc2hpQ29ycCBJbmMuMUAwPgYDVQQDEzdDb25zdWwgQWdlbnQgQ0Eg
MTQ0MTkwOTA0MDA4ODQxOTE3MTQzNDM4MjEzMTEzMjA0NjU2OTgwMB4XDTIyMDkx
NjE4NDUwNloXDTI3MDkxNTE4NDUwNlowgbkxCzAJBgNVBAYTAlVTMQswCQYDVQQI
EwJDQTEWMBQGA1UEBxMNU2FuIEZyYW5jaXNjbzEaMBgGA1UECRMRMTAxIFNlY29u
ZCBTdHJlZXQxDjAMBgNVBBETBTk0MTA1MRcwFQYDVQQKEw5IYXNoaUNvcnAgSW5j
LjFAMD4GA1UEAxM3Q29uc3VsIEFnZW50IENBIDE0NDE5MDkwNDAwODg0MTkxNzE0
MzQzODIxMzExMzIwNDY1Njk4MDBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABA9w
J9aqbpdoVXQLdYTfUpBM2bgElznRYQP/GcNQUtvopvVywPjC7obFuZP1oM7YX7Wy
hGyeudV4pvF1lz9nVeOjezB5MA4GA1UdDwEB/wQEAwIBhjAPBgNVHRMBAf8EBTAD
AQH/MCkGA1UdDgQiBCA9dZuoEX3yrbebyEEzsN4L2rr7FJd6FsjIioR6KbMIhTAr
BgNVHSMEJDAigCA9dZuoEX3yrbebyEEzsN4L2rr7FJd6FsjIioR6KbMIhTAKBggq
hkjOPQQDAgNIADBFAiARhJR88w9EXLsq5A932auHvLFAw+uQ0a2TLSaJF54fyAIh
APQczkCoIFiLlGp0GYeHEfjvrdm2g8Q3BUDjeAUfZPaW
-----END CERTIFICATE-----`
