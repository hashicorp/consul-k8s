// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gateways

const (
	// General environment variables.
	envPodName      = "POD_NAME"
	envPodNamespace = "POD_NAMESPACE"
	envNodeName     = "NODE_NAME"
	envTmpDir       = "TMPDIR"

	// Dataplane Configuration Environment variables.
	envDPProxyId             = "DP_PROXY_ID"
	envDPCredentialLoginMeta = "DP_CREDENTIAL_LOGIN_META"

	// Init Container Configuration Environment variables.
	envConsulAddresses            = "CONSUL_ADDRESSES"
	envConsulGRPCPort             = "CONSUL_GRPC_PORT"
	envConsulHTTPPort             = "CONSUL_HTTP_PORT"
	envConsulAPITimeout           = "CONSUL_API_TIMEOUT"
	envConsulNodeName             = "CONSUL_NODE_NAME"
	envConsulLoginAuthMethod      = "CONSUL_LOGIN_AUTH_METHOD"
	envConsulLoginBearerTokenFile = "CONSUL_LOGIN_BEARER_TOKEN_FILE"
	envConsulLoginMeta            = "CONSUL_LOGIN_META"
	envConsulLoginPartition       = "CONSUL_LOGIN_PARTITION"
	envConsulNamespace            = "CONSUL_NAMESPACE"
	envConsulPartition            = "CONSUL_PARTITION"

	// defaultBearerTokenFile is the default location where the init container will store the bearer token for the dataplane container to read.
	defaultBearerTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"
)
