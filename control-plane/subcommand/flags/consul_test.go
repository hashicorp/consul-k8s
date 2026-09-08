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

				ClientCertFileEnvVar: "path/to/cert.pem",
				ClientKeyFileEnvVar:  "path/to/key.pem",

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
					UseTLS:         true,
					CACertFile:     "path/to/ca.pem",
					CACertPEM:      "test-ca-pem",
					TLSServerName:  "server.consul",
					ClientCertFile: "path/to/cert.pem",
					ClientKeyFile:  "path/to/key.pem",
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
	t.Cleanup(func() { _ = os.RemoveAll(caFile.Name()) })
	require.NoError(t, err)
	_, err = caFile.WriteString(testCA)
	require.NoError(t, err)

	certFile, err := os.CreateTemp("", "")
	t.Cleanup(func() { _ = os.RemoveAll(certFile.Name()) })
	require.NoError(t, err)
	_, err = certFile.WriteString(testClientCert)
	require.NoError(t, err)

	keyFile, err := os.CreateTemp("", "")
	t.Cleanup(func() { _ = os.RemoveAll(keyFile.Name()) })
	require.NoError(t, err)
	_, err = keyFile.WriteString(testClientKey)
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
		"mutual TLS": {
			flags: ConsulFlags{
				Addresses: "consul.address",
				ConsulTLSFlags: ConsulTLSFlags{
					UseTLS:         true,
					ClientCertFile: certFile.Name(),
					ClientKeyFile:  keyFile.Name(),
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
			if c.flags.ClientCertFile != "" {
				require.Len(t, cfg.TLS.Certificates, 1)
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
		"mutual TLS cert and key files provided": {
			flags: ConsulFlags{
				ConsulTLSFlags: ConsulTLSFlags{
					UseTLS:         true,
					ClientCertFile: "path/to/cert",
					ClientKeyFile:  "path/to/key",
				},
			},
			expConfig: &api.Config{
				Scheme: "https",
				TLSConfig: api.TLSConfig{
					CertFile: "path/to/cert",
					KeyFile:  "path/to/key",
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

const (
	testCA = `
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

	testClientCert = `
-----BEGIN CERTIFICATE-----
MIIE/jCCAuYCAQEwDQYJKoZIhvcNAQELBQAwRTELMAkGA1UEBhMCQVUxEzARBgNV
BAgMClNvbWUtU3RhdGUxITAfBgNVBAoMGEludGVybmV0IFdpZGdpdHMgUHR5IEx0
ZDAeFw0yMzExMDYxNDQyNDVaFw0zMzExMDMxNDQyNDVaMEUxCzAJBgNVBAYTAkFV
MRMwEQYDVQQIDApTb21lLVN0YXRlMSEwHwYDVQQKDBhJbnRlcm5ldCBXaWRnaXRz
IFB0eSBMdGQwggIiMA0GCSqGSIb3DQEBAQUAA4ICDwAwggIKAoICAQDQx4nDjDQx
EOtKXGimThOnZtgUOCoyLfjQZEDE4XklVWhCEovnBNLt+u39+yDFIFxyDVvAGpEQ
0f6Cl8C8A5+gX0Qj2PIJF7kmxeHKusmtliOi0VGBxlCQ48rTqDo44ixas3WAaMnY
nG2GX6JlENQf5jMz4+d+wvcMA5vL7nJDzUXBGM5mQtqPCMC0b9a2ou8XL/I3NURT
uAsrTyBQIBBVJhXIZjxAL8P339gY7r6KnvrWSpRaZGI3e4cISVs2A5fu0nPbeEKu
P8bAgEsWP7sQ8GmB6oRsbTvKPV+7JEMhB7ZWik1xbAYzsY2mWQXeWVsfMf/vNb4v
UZaboM/2R+PBXMRZRbGdraWe8aj6ClRDeQI3WXpqgZmoCi7Ui6LQkRHqzU2ogIAu
UCCvz91jgQgmX3zhugy191wUcH8S3/3KFwtJxcFSKcXgtCmsWI9P7JLMFdobXkdA
TZujlq+f3BJpgn+KE8ZTrPnkQCcZfd0iT6x4T7ygXWFK1VKsyXWY4whIuwD8P0FS
6Kx9dcJ8V2lW/VkJ1acsSYYN5QNBqI0nY92uw49waKUs8XIJYD1Gbf0RPmy1U3kj
d2vKPiOrOgGJiaRjMKPxhnr1h9YHt/pE+h/394168ongY3iOJsHPWffEI4jQK842
6JmGYenf1EEPPWDKHucXW36CpbTquRHVIQIDAQABMA0GCSqGSIb3DQEBCwUAA4IC
AQBgVAGEDSG0+NHav8fNroZH3G/tFidzdCOONS8ZGFuM4mNPEG1T9RDHDYkrfMmo
tSP548G8XwhbjroLyAR5Td6z29YGzdVumjNxN15Srf4QHmGRJw3Ni+wCBj89eCAa
Gkink4Fq6CfmN0VxtaIOUnfyQtT4FzoxT/6ccPhdJ8WXs1no/j0/ou49CH5Ujutw
rT59NHWpJzEPdT6+i/q8zwmxmHwpOLmwrYx2iZ30KOAowD1mEZjZhjv7yukwTlf5
NzKUjLaa714RsiDpyU1g8P+DwnjbQzRRrvQQRC43OgFj771UnhzBPIzc7YpN99Bc
OZtevCRC2cuSd87MsVC/cjJ5xMKWvqJT0usAD3uhRHI6yqpuTNzcO2ID+YoYMvse
DAv91MBAXygT0Z/A6l67pLKnEjQwv4rrfnTlfrWuGMVgI+xnPTwDFJbjgAfLs+RT
tvQvGzdX/Vih2ElInQf0KqVQiGVhoQErQE0dh9yxGMHdAtIfFDYtbxALL16xmvnr
173rxfzuAxpksqX43DmkZFo4WnkrV0ge/05c34ghISjet9HHkAWx2rR1sAPBRJLI
g/BreGrTX0brt5JpmuDyxTGOgLkYCfcq3puuEYENXPgMG/CE4FUrPpzk7y/nid+z
XyFVvUT1KsJ6Qe4XaPHUpEW+vw4FlC7tq+nnAOyDFjFmhw==
-----END CERTIFICATE-----`

	testClientKey = `
-----BEGIN PRIVATE KEY-----
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
hP/L8Bb5JnGGmJL6EjWQ+53DrWzK2g==
-----END PRIVATE KEY-----`
)
