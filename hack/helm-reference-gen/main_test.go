// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Test various smaller cases and special cases.
func Test(t *testing.T) {
	cases := map[string]struct {
		Input string
		Exp   string
	}{
		"string value": {
			Input: `---
# Line 1
# Line 2
key: value`,
			Exp: `- [$key$](#h-key)

## All Values

### key ((#h-key))

- $key$ ((#v-key)) ($string: value$) - Line 1\n  Line 2
`,
		},
		"integer value": {
			Input: `---
# Line 1
# Line 2
replicas: 3`,
			Exp: `- [$replicas$](#h-replicas)

## All Values

### replicas ((#h-replicas))

- $replicas$ ((#v-replicas)) ($integer: 3$) - Line 1\n  Line 2
`,
		},
		"boolean value": {
			Input: `---
# Line 1
# Line 2
enabled: true`,
			Exp: `- [$enabled$](#h-enabled)

## All Values

### enabled ((#h-enabled))

- $enabled$ ((#v-enabled)) ($boolean: true$) - Line 1\n  Line 2
`,
		},
		"map": {
			Input: `---
# Map line 1
# Map line 2
map:
  # Key line 1
  # Key line 2
  key: value`,
			Exp: `- [$map$](#h-map)

## All Values

### map ((#h-map))

- $map$ ((#v-map)) - Map line 1\n  Map line 2

  - $key$ ((#v-map-key)) ($string: value$) - Key line 1\n    Key line 2
`,
		},
		"map with multiple keys": {
			Input: `---
# Map line 1
# Map line 2
map:
  # Key line 1
  # Key line 2
  key: value
  # Int docs
  int: 1
  # Bool docs
  bool: true`,
			Exp: `- [$map$](#h-map)

## All Values

### map ((#h-map))

- $map$ ((#v-map)) - Map line 1\n  Map line 2

  - $key$ ((#v-map-key)) ($string: value$) - Key line 1
    Key line 2

  - $int$ ((#v-map-int)) ($integer: 1$) - Int docs

  - $bool$ ((#v-map-bool)) ($boolean: true$) - Bool docs
`,
		},
		"null value": {
			Input: `---
# key docs
# @type: string
key: null`,
			Exp: `- [$key$](#h-key)

## All Values

### key ((#h-key))

- $key$ ((#v-key)) ($string: null$) - key docs
`,
		},
		"description with empty line": {
			Input: `---
# line 1
#
# line 2
key: value`,
			Exp: `- [$key$](#h-key)

## All Values

### key ((#h-key))

- $key$ ((#v-key)) ($string: value$) - line 1\n\n  line 2
`,
		},
		"array of strings": {
			Input: `---
# line 1
# @type: array<string>
serverAdditionalDNSSANs: []
`,
			Exp: `- [$serverAdditionalDNSSANs$](#h-serveradditionaldnssans)

## All Values

### serverAdditionalDNSSANs ((#h-serveradditionaldnssans))

- $serverAdditionalDNSSANs$ ((#v-serveradditionaldnssans)) ($array<string>: []$) - line 1
`,
		},
		"map with empty string values": {
			Input: `---
# gossipEncryption
gossipEncryption:
  # secretName
  secretName: ""
  # secretKey
  secretKey: ""
`,
			Exp: `- [$gossipEncryption$](#h-gossipencryption)

## All Values

### gossipEncryption ((#h-gossipencryption))

- $gossipEncryption$ ((#v-gossipencryption)) - gossipEncryption

  - $secretName$ ((#v-gossipencryption-secretname)) ($string: ""$) - secretName

  - $secretKey$ ((#v-gossipencryption-secretkey)) ($string: ""$) - secretKey
`,
		},
		"map with null string values": {
			Input: `---
bootstrapToken:
  # @type: string
  secretName: null
  # @type: string
  secretKey: null
`,
			Exp: `- [$bootstrapToken$](#h-bootstraptoken)

## All Values

### bootstrapToken ((#h-bootstraptoken))

- $bootstrapToken$ ((#v-bootstraptoken))

  - $secretName$ ((#v-bootstraptoken-secretname)) ($string: null$)

  - $secretKey$ ((#v-bootstraptoken-secretkey)) ($string: null$)
`,
		},
		"resource settings": {
			Input: `---
# lifecycle
lifecycleSidecarContainer:
  # The resource requests and limits (CPU, memory, etc.)
  # for each of the lifecycle sidecar containers. This should be a YAML map of a Kubernetes
  # [ResourceRequirements](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/) object.
  #
  # Example:
  # $$$yaml
  # resources:
  #   requests:
  #     memory: "25Mi"
  #     cpu: "20m"
  #   limits:
  #     memory: "50Mi"
  #     cpu: "20m"
  # $$$
  resources:
    requests:
      memory: "25Mi"
      cpu: "20m"
    limits:
      memory: "50Mi"
      cpu: "20m"
`,
			Exp: `- [$lifecycleSidecarContainer$](#h-lifecyclesidecarcontainer)

## All Values

### lifecycleSidecarContainer ((#h-lifecyclesidecarcontainer))

- $lifecycleSidecarContainer$ ((#v-lifecyclesidecarcontainer)) - lifecycle

  - $resources$ ((#v-lifecyclesidecarcontainer-resources)) - The resource requests and limits (CPU, memory, etc.)
    for each of the lifecycle sidecar containers. This should be a YAML map of a Kubernetes
    [ResourceRequirements](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/) object.

    Example:
    $$$yaml
    resources:
      requests:
        memory: "25Mi"
        cpu: "20m"
      limits:
        memory: "50Mi"
        cpu: "20m"
    $$$

    - $requests$ ((#v-lifecyclesidecarcontainer-resources-requests))

      - $memory$ ((#v-lifecyclesidecarcontainer-resources-requests-memory)) ($string: 25Mi$)

      - $cpu$ ((#v-lifecyclesidecarcontainer-resources-requests-cpu)) ($string: 20m$)

    - $limits$ ((#v-lifecyclesidecarcontainer-resources-limits))

      - $memory$ ((#v-lifecyclesidecarcontainer-resources-limits-memory)) ($string: 50Mi$)

      - $cpu$ ((#v-lifecyclesidecarcontainer-resources-limits-cpu)) ($string: 20m$)
`,
		},
		"default as dash": {
			Input: `---
server:
  # If true, the chart will install all the resources necessary for a
  # Consul server cluster. If you're running Consul externally and want agents
  # within Kubernetes to join that cluster, this should probably be false.
  # @default: global.enabled
  # @type: boolean
  enabled: "-"
`,
			Exp: `- [$server$](#h-server)

## All Values

### server ((#h-server))

- $server$ ((#v-server))

  - $enabled$ ((#v-server-enabled)) ($boolean: global.enabled$) - If true, the chart will install all the resources necessary for a
    Consul server cluster. If you're running Consul externally and want agents
    within Kubernetes to join that cluster, this should probably be false.
`,
		},
		"extraConfig {}": {
			Input: `---
extraConfig: |
  {}
`,
			Exp: `- [$extraConfig$](#h-extraconfig)

## All Values

### extraConfig ((#h-extraconfig))

- $extraConfig$ ((#v-extraconfig)) ($string: {}$)
`,
		},
		"affinity": {
			Input: `---
# Affinity Settings
affinity: |
  podAntiAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
      - labelSelector:
          matchLabels:
            app: {{ template "consul.name" . }}
            release: "{{ .Release.Name }}"
            component: server
        topologyKey: kubernetes.io/hostname
`,
			Exp: `- [$affinity$](#h-affinity)

## All Values

### affinity ((#h-affinity))

- $affinity$ ((#v-affinity)) ($string$) - Affinity Settings
`,
		},
		"k8sAllowNamespaces": {
			Input: `---
# @type: array<string>
k8sAllowNamespaces: ["*"]`,
			Exp: `- [$k8sAllowNamespaces$](#h-k8sallownamespaces)

## All Values

### k8sAllowNamespaces ((#h-k8sallownamespaces))

- $k8sAllowNamespaces$ ((#v-k8sallownamespaces)) ($array<string>: ["*"]$)
`,
		},
		"k8sDenyNamespaces": {
			Input: `---
# @type: array<string>
k8sDenyNamespaces: ["kube-system", "kube-public"]`,
			Exp: `- [$k8sDenyNamespaces$](#h-k8sdenynamespaces)

## All Values

### k8sDenyNamespaces ((#h-k8sdenynamespaces))

- $k8sDenyNamespaces$ ((#v-k8sdenynamespaces)) ($array<string>: ["kube-system", "kube-public"]$)
`,
		},
		"gateways": {
			Input: `---
# @type: array<map>
gateways:
  - name: ingress-gateway`,
			Exp: `- [$gateways$](#h-gateways)

## All Values

### gateways ((#h-gateways))

- $gateways$ ((#v-gateways)) ($array<map>$)

  - $name$ ((#v-gateways-name)) ($string: ingress-gateway$)
`,
		},
		"enterprise alert": {
			Input: `---
# [Enterprise Only] line 1
# line 2
key: value
`,
			Exp: `- [$key$](#h-key)

## All Values

### key ((#h-key))

- $key$ ((#v-key)) ($string: value$) - <EnterpriseAlert inline /> line 1\n  line 2
`,
		},
		"yaml comments in examples": {
			Input: `---
# Examples:
#
# $$$yaml
# # Consul 1.5.0
# image: "consul:1.5.0"
# # Consul Enterprise 1.5.0
# image: "hashicorp/consul-enterprise:1.5.0-ent"
# $$$
key: value
`,
			Exp: `- [$key$](#h-key)

## All Values

### key ((#h-key))

- $key$ ((#v-key)) ($string: value$) - Examples:

  $$$yaml
  # Consul 1.5.0
  image: "consul:1.5.0"
  # Consul Enterprise 1.5.0
  image: "hashicorp/consul-enterprise:1.5.0-ent"
  $$$
`,
		},
		"type override uses last match": {
			Input: `---
# @type: override-1
# @type: override-2
key: value
`,
			Exp: `- [$key$](#h-key)

## All Values

### key ((#h-key))

- $key$ ((#v-key)) ($override-2: value$)
`,
		},
		"recurse false": {
			Input: `---
key: value
# port docs
# @type: array<map>
# @recurse: false
ports:
- port: 8080
  nodePort: null
- port: 8443
  nodePort: null
`,
			Exp: `- [$key$](#h-key)
- [$ports$](#h-ports)

## All Values

### key ((#h-key))

- $key$ ((#v-key)) ($string: value$)

### ports ((#h-ports))

- $ports$ ((#v-ports)) ($array<map>$) - port docs
`,
		},
		"@type: map": {
			Input: `---
# @type: map
key: null
`,
			Exp: `- [$key$](#h-key)

## All Values

### key ((#h-key))

- $key$ ((#v-key)) ($map$)
`,
		},
		"if of type map and not annotated with @type": {
			Input: `---
key:
  foo: bar
`,
			Exp: `- [$key$](#h-key)

## All Values

### key ((#h-key))

- $key$ ((#v-key))

  - $foo$ ((#v-key-foo)) ($string: bar$)
`,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			// Swap $ for `.
			input := strings.ReplaceAll(c.Input, "$", "`")

			out, err := GenerateDocs(input)
			require.NoError(t, err)

			// Swap $ for `.
			exp := strings.ReplaceAll(c.Exp, "$", "`")

			// Swap \n for real \n.
			exp = strings.ReplaceAll(exp, "\\n", "\n")

			exp = tocPrefix + exp

			require.Equal(t, exp, out)
		})
	}
}

// Test against a full values file and compare against a golden file.
func TestFullValues(t *testing.T) {
	inputBytes, err := os.ReadFile(filepath.Join("fixtures", "full-values.yaml"))
	require.NoError(t, err)
	expBytes, err := os.ReadFile(filepath.Join("fixtures", "full-values.golden"))
	require.NoError(t, err)

	actual, err := GenerateDocs(string(inputBytes))
	require.NoError(t, err)
	if actual != string(expBytes) {
		require.NoError(t, os.WriteFile(filepath.Join("fixtures", "full-values.actual"), []byte(actual), 0644))
		require.FailNow(t, "output not equal, actual output to full-values.actual")
	}
}
