## 1.4.8 (January 15, 2025)

BUG FIXES:

* cli: fix issue where the `consul-k8s proxy list` command does not include API gateways. [[GH-4426](https://github.com/hashicorp/consul-k8s/issues/4426)]
* connect-inject: fix issue where the ACL policy for the connect-injector included the `acl = "write"` rule twice when namespaces were not enabled. [[GH-4434](https://github.com/hashicorp/consul-k8s/issues/4434)]

SECURITY:

* updated golang.org/x/net dependency to 0.34.0 to fix vulnerability [[GO-2024-3333](https://pkg.go.dev/vuln/GO-2024-3333)] in CLI, CNI, acceptance and control-plane submodule.[[PR-4458](https://github.com/hashicorp/consul-k8s/pull/4458)]


## 1.4.7 (November 4, 2023)

SECURITY:

* Upgrade Go to use 1.22.7. This addresses CVE 
[CVE-2024-34155](https://nvd.nist.gov/vuln/detail/CVE-2024-34155) [[GH-4313](https://github.com/hashicorp/consul-k8s/issues/4313)]
* crd: Add `contains` and `ignoreCase` to the Intentions CRD to support configuring L7 Header intentions resilient to variable casing and multiple header values. [[GH-4385](https://github.com/hashicorp/consul-k8s/issues/4385)]
* crd: Add `http.incoming.requestNormalization` to the Mesh CRD to support configuring service traffic request normalization. [[GH-4385](https://github.com/hashicorp/consul-k8s/issues/4385)]

IMPROVEMENTS:

* connect-inject: remove unnecessary resource permissions from connect-inject ClusterRole [[GH-4307](https://github.com/hashicorp/consul-k8s/issues/4307)]
* helm: Exclude gke namespaces from being connect-injected when the connect-inject: default: true value is set. [[GH-4333](https://github.com/hashicorp/consul-k8s/issues/4333)]

BUG FIXES:

* api-gateway: `global.imagePullSecrets` are now configured on the `ServiceAccount` for `Gateways`.

Note: the referenced image pull Secret(s) must be present in the same namespace the `Gateway` is deployed to. [[GH-4316](https://github.com/hashicorp/consul-k8s/issues/4316)]
* helm: fix issue where the API Gateway GatewayClassConfig tolerations can not be parsed by the Helm chart. [[GH-4315](https://github.com/hashicorp/consul-k8s/issues/4315)]
* sync-catalog: Enable the user to purge the registered services by passing parent node and necessary filters. [[GH-4255](https://github.com/hashicorp/consul-k8s/issues/4255)]

## 1.4.6 (August 30, 2024)

SECURITY:

* Bump Go to 1.22.5 to address [CVE-2024-24791](https://nvd.nist.gov/vuln/detail/CVE-2024-24791) [[GH-4228](https://github.com/hashicorp/consul-k8s/issues/4228)]
* Upgrade Docker cli to use v.27.1. This addresses CVE
[CVE-2024-41110](https://nvd.nist.gov/vuln/detail/CVE-2024-41110) [[GH-4228](https://github.com/hashicorp/consul-k8s/issues/4228)]

IMPROVEMENTS:

* docker: update go-discover binary [[GH-4287](https://github.com/hashicorp/consul-k8s/issues/4287)]
* docker: update ubi base image to `ubi9-minimal:9.4`. [[GH-4287](https://github.com/hashicorp/consul-k8s/issues/4287)]
* helm: Adds `webhookCertManager.resources` field which can be configured to override the `resource` settings for the `webhook-cert-manager` deployment. [[GH-4184](https://github.com/hashicorp/consul-k8s/issues/4184)]
* helm: Adds `connectInject.apiGateway.managedGatewayClass.resourceJob.resources` field which can be configured to override the `resource` settings for the `gateway-resources-job` job. [[GH-4184](https://github.com/hashicorp/consul-k8s/issues/4184)]
* config-entry: add validate_clusters to mesh config entry [[GH-4256](https://github.com/hashicorp/consul-k8s/issues/4256)]

BUG FIXES:

* Fixes install of Consul on GKE Autopilot where the option 'manageNonStandardCRDs' was not being used for the TCPRoute CRD. [[GH-4213](https://github.com/hashicorp/consul-k8s/issues/4213)]
* api-gateway: fix nil pointer deref bug when the section name in a gateway policy is not specified [[GH-4247](https://github.com/hashicorp/consul-k8s/issues/4247)]
* control-plane: add missing  `$HOST_IP` environment variable to to consul-dataplane sidecar containers [[GH-3916](https://github.com/hashicorp/consul-k8s/issues/3916)]
* helm: adds imagePullSecret to the gateway-resources job and the gateway-cleanup job, would fail before if the image was in a private registry [[GH-4210](https://github.com/hashicorp/consul-k8s/issues/4210)]
* openshift: order SecurityContextConstraint volumes alphabetically to match OpenShift behavior.
This ensures that diff detection tools like ArgoCD consider the source and reconciled resources to be identical. [[GH-4227](https://github.com/hashicorp/consul-k8s/issues/4227)]
* sync-catalog: fix infinite retry loop when the catalog fails to connect to consul-server during the sync process [[GH-4266](https://github.com/hashicorp/consul-k8s/issues/4266)]

## 1.4.5 (August 29, 2024)

Release redacted, use `1.4.6`

## 1.4.4 (July 15, 2024)

SECURITY:

* Upgrade go version to 1.22.5 to address [CVE-2024-24791](https://cve.mitre.org/cgi-bin/cvename.cgi?name=CVE-2024-24791) [[GH-4154](https://github.com/hashicorp/consul-k8s/issues/4154)]
* Upgrade go-retryablehttp to v0.7.7 to address [GHSA-v6v8-xj6m-xwqh](https://github.com/advisories/GHSA-v6v8-xj6m-xwqh) [[GH-4169](https://github.com/hashicorp/consul-k8s/issues/4169)]

IMPROVEMENTS:

* upgrade go version to v1.22.4. [[GH-4085](https://github.com/hashicorp/consul-k8s/issues/4085)]
* api-gateways: Change security settings to make root file system read only and to not allow privilage escalation. [[GH-3959](https://github.com/hashicorp/consul-k8s/issues/3959)]
* cni: package `consul-cni` as .deb and .rpm files [[GH-4040](https://github.com/hashicorp/consul-k8s/issues/4040)]
* control-plane: Remove anyuid Security Context Constraints (SCC) requirement in OpenShift. [[GH-4152](https://github.com/hashicorp/consul-k8s/issues/4152)]
* partition-init: Role no longer includes unnecessary access to Secrets resource. [[GH-4053](https://github.com/hashicorp/consul-k8s/issues/4053)]

BUG FIXES:

* api-gateway: fix issue where API Gateway specific acl roles/policy were not being cleaned up on deletion of an api-gateway [[GH-4060](https://github.com/hashicorp/consul-k8s/issues/4060)]
* cni: fix incorrect release version due to unstable submodule pinning [[GH-4091](https://github.com/hashicorp/consul-k8s/issues/4091)]
* connect-inject: add NET_BIND_SERVICE capability when injecting consul-dataplane sidecar [[GH-4152](https://github.com/hashicorp/consul-k8s/issues/4152)]
* endpoints-controller: graceful shutdown logic should not run on a new pod with the same name. Fixes a case where statefulset rollouts could get stuck in graceful shutdown when the new pods come up. [[GH-4059](https://github.com/hashicorp/consul-k8s/issues/4059)]

## 1.4.2 (May 20, 2024)

SECURITY:

* Upgrade Go to use 1.21.10. This addresses CVEs 
[CVE-2024-24787](https://nvd.nist.gov/vuln/detail/CVE-2024-24787) and
[CVE-2024-24788](https://nvd.nist.gov/vuln/detail/CVE-2024-24788) [[GH-3980](https://github.com/hashicorp/consul-k8s/issues/3980)]
* Upgrade `helm/v3` to 3.14.4. This resolves the following security vulnerabilities:
[CVE-2024-25620](https://osv.dev/vulnerability/CVE-2024-25620)
[CVE-2024-26147](https://osv.dev/vulnerability/CVE-2024-26147) [[GH-3935](https://github.com/hashicorp/consul-k8s/issues/3935)]
* Upgrade to use Go `1.21.9`. This resolves CVE
[CVE-2023-45288](https://nvd.nist.gov/vuln/detail/CVE-2023-45288) (`http2`). [[GH-3893](https://github.com/hashicorp/consul-k8s/issues/3893)]
* Upgrade to use golang.org/x/net `v0.24.0`. This resolves CVE
[CVE-2023-45288](https://nvd.nist.gov/vuln/detail/CVE-2023-45288) (`x/net`). [[GH-3893](https://github.com/hashicorp/consul-k8s/issues/3893)]

FEATURES:

* Add support for configuring graceful startup proxy lifecycle management settings. [[GH-3878](https://github.com/hashicorp/consul-k8s/issues/3878)]

IMPROVEMENTS:

* control-plane: support <space>, <comma> and <\n> as upstream separators. [[GH-3956](https://github.com/hashicorp/consul-k8s/issues/3956)]
* ConfigEntries controller: Only error for config entries from different datacenters when the config entries are different [[GH-3873](https://github.com/hashicorp/consul-k8s/issues/3873)]
* control-plane: Add support for receiving iptables configuration via CNI arguments, to support Nomad transparent proxy [[GH-3795](https://github.com/hashicorp/consul-k8s/issues/3795)]
* control-plane: Remove anyuid Security Context Constraints (SCC) requirement in OpenShift. [[GH-3813](https://github.com/hashicorp/consul-k8s/issues/3813)]
* helm: only create the default Prometheus path annotation when it's not already specified within the component-specific
annotations. For example if the `client.annotations` value sets prometheus.io/path annotation, don't overwrite it with
the default value. [[GH-3846](https://github.com/hashicorp/consul-k8s/issues/3846)]
* helm: support sync-lb-services-endpoints flag for syncCatalog [[GH-3905](https://github.com/hashicorp/consul-k8s/issues/3905)]
* terminating-gateways: Remove unnecessary permissions from terminating gateways role [[GH-3928](https://github.com/hashicorp/consul-k8s/issues/3928)]

BUG FIXES:

* Create Consul service with mode transparent-proxy even when a cluster IP is not assigned to the service.. [[GH-3974](https://github.com/hashicorp/consul-k8s/issues/3974)]
* api-gateway: Fix order of initialization for creating ACL role/policy to avoid error logs in consul when upgrading between versions. [[GH-3918](https://github.com/hashicorp/consul-k8s/issues/3918)]
* api-gateway: fix bug where multiple logical APIGateways would share the same ACL policy. [[GH-4000](https://github.com/hashicorp/consul-k8s/issues/4000)]
* consul-cni: Fixed a bug where the output of `-version` did not include the version of the binary [[GH-3829](https://github.com/hashicorp/consul-k8s/issues/3829)]
* control-plane: fix a panic when an upstream annotation is malformed. [[GH-3956](https://github.com/hashicorp/consul-k8s/issues/3956)]
* connect-inject: Fixed issue where on restart, if a managed-gateway-acl-role already existed the container would error [[GH-3978](https://github.com/hashicorp/consul-k8s/issues/3978)]

## 1.4.1 (March 28, 2024)

SECURITY:

* Update `google.golang.org/protobuf` to v1.33.0 to address [CVE-2024-24786](https://nvd.nist.gov/vuln/detail/CVE-2024-24786). [[GH-3719](https://github.com/hashicorp/consul-k8s/issues/3719)]
* Update the Consul Build Go base image to `alpine3.19`. This resolves CVEs
[CVE-2023-52425](https://nvd.nist.gov/vuln/detail/CVE-2023-52425)
[CVE-2023-52426⁠](https://nvd.nist.gov/vuln/detail/CVE-2023-52426) [[GH-3741](https://github.com/hashicorp/consul-k8s/issues/3741)]
* Upgrade to use Go `1.21.8`. This resolves CVEs
[CVE-2024-24783](https://nvd.nist.gov/vuln/detail/CVE-2024-24783) (`crypto/x509`).
[CVE-2023-45290](https://nvd.nist.gov/vuln/detail/CVE-2023-45290) (`net/http`).
[CVE-2023-45289](https://nvd.nist.gov/vuln/detail/CVE-2023-45289) (`net/http`, `net/http/cookiejar`).
[CVE-2024-24785](https://nvd.nist.gov/vuln/detail/CVE-2024-24785) (`html/template`).
[CVE-2024-24784](https://nvd.nist.gov/vuln/detail/CVE-2024-24784) (`net/mail`). [[GH-3741](https://github.com/hashicorp/consul-k8s/issues/3741)]

IMPROVEMENTS:

* api-gateway: Expose prometheus scrape metrics on api-gateway pods. [[GH-3811](https://github.com/hashicorp/consul-k8s/issues/3811)]
* catalog: Topology zone and region information is now read from the Kubernetes endpoints and associated node and added to registered consul services under Metadata. [[GH-3693](https://github.com/hashicorp/consul-k8s/issues/3693)]

BUG FIXES:

* api-gateway: Fix order of initialization for creating ACL role/policy to avoid error logs in consul. [[GH-3779](https://github.com/hashicorp/consul-k8s/issues/3779)]
* control-plane: fix an issue where ACL token cleanup did not respect a pod's GracefulShutdownPeriodSeconds and
tokens were invalidated immediately on pod entering Terminating state. [[GH-3736](https://github.com/hashicorp/consul-k8s/issues/3736)]
* control-plane: fix an issue where ACL tokens would prematurely be deleted and services would be deregistered if there
was a K8s API error fetching the pod. [[GH-3758](https://github.com/hashicorp/consul-k8s/issues/3758)]

## 1.4.0 (February 29, 2024)

> NOTE: Consul K8s 1.4.x is compatible with Consul 1.18.x and Consul Dataplane 1.4.x. Refer to our [compatibility matrix](https://developer.hashicorp.com/consul/docs/k8s/compatibility) for more info.

BREAKING CHANGES:

* server: set `autopilot.min_quorum` to the correct quorum value to ensure autopilot doesn't prune servers needed for quorum. Also set `autopilot. disable_upgrade_migration` to `true` as that setting is meant for blue/green deploys, not rolling deploys.

This setting makes sense for most use-cases, however if you had a specific reason to use the old settings you can use the following config to keep them:

    server:
      extraConfig: |
        {"autopilot": {"min_quorum": 0, "disable_upgrade_migration": false}} [[GH-3000](https://github.com/hashicorp/consul-k8s/issues/3000)]
* server: set `leave_on_terminate` to `true` and set the server pod disruption budget `maxUnavailable` to `1`.

This change makes server rollouts faster and more reliable. However, there is now a potential for reduced reliability if users accidentally
scale the statefulset down. Now servers will leave the raft pool when they are stopped gracefully which reduces the fault
tolerance. For example, with 5 servers, you can tolerate a loss of 2 servers' data as raft guarantees data is replicated to
a majority of nodes (3). However, if you accidentally scale the statefulset down to 3, then the raft quorum will now be 2, and
if you lose 2 servers, you may lose data. Before this change, the quorum would have remained at 3.

During a regular rollout, the number of servers will be reduced by 1 at a time, which doesn't affect quorum when running
an odd number of servers, e.g. quorum for 5 servers is 3, and quorum for 4 servers is also 3. That's why the pod disruption
budget is being set to 1 now.

If a server is stopped ungracefully, e.g. due to a node loss, it will not leave the raft pool, and so fault tolerance won't be affected.

For the vast majority of users, this change will be beneficial, however if you wish to remain with the old settings you
can set:

    server:
      extraConfig: |
        {"leave_on_terminate": false}
      disruptionBudget:
        maxUnavailable: <previous setting> [[GH-3000](https://github.com/hashicorp/consul-k8s/issues/3000)]

SECURITY:

* Update Envoy version to 1.25.11 to address [CVE-2023-44487](https://github.com/envoyproxy/envoy/security/advisories/GHSA-jhv4-f7mr-xx76) [[GH-3116](https://github.com/hashicorp/consul-k8s/issues/3116)]
* Upgrade `helm/v3` to 3.11.3. This resolves the following security vulnerabilities:
[CVE-2023-25165](https://osv.dev/vulnerability/CVE-2023-25165)
[CVE-2022-23524](https://osv.dev/vulnerability/CVE-2022-23524)
[CVE-2022-23526](https://osv.dev/vulnerability/CVE-2022-23526)
[CVE-2022-23525](https://osv.dev/vulnerability/CVE-2022-23525) [[GH-3625](https://github.com/hashicorp/consul-k8s/issues/3625)]
* Upgrade docker/distribution to 2.8.3+incompatible (latest) to resolve [CVE-2023-2253](https://osv.dev/vulnerability/CVE-2023-2253). [[GH-3625](https://github.com/hashicorp/consul-k8s/issues/3625)]
* Upgrade docker/docker to 25.0.3+incompatible (latest) to resolve [GHSA-jq35-85cj-fj4p](https://osv.dev/vulnerability/GHSA-jq35-85cj-fj4p). [[GH-3625](https://github.com/hashicorp/consul-k8s/issues/3625)]
* Upgrade filepath-securejoin to 0.2.4 (latest) to resolve [GO-2023-2048](https://osv.dev/vulnerability/GO-2023-2048). [[GH-3625](https://github.com/hashicorp/consul-k8s/issues/3625)]
* Upgrade containerd to 1.7.13 (latest) to resolve [GHSA-7ww5-4wqc-m92c](https://osv.dev/vulnerability/GO-2023-2412). [[GH-3625](https://github.com/hashicorp/consul-k8s/issues/3625)]

IMPROVEMENTS:

* control-plane: publish `consul-k8s-control-plane` and `consul-k8s-control-plane-fips` images to official HashiCorp AWS ECR. [[GH-3668](https://github.com/hashicorp/consul-k8s/issues/3668)]
* helm: Kubernetes v1.29 is now supported. Minimum tested version of Kubernetes is now v1.26. [[GH-3675](https://github.com/hashicorp/consul-k8s/issues/3675)]
* cni: When CNI is enabled, set ReadOnlyRootFilesystem=true and AllowPrivilegeEscalation=false for mesh pod init containers and AllowPrivilegeEscalation=false for consul-dataplane containers (ReadOnlyRootFilesystem was already true for consul-dataplane containers). [[GH-3498](https://github.com/hashicorp/consul-k8s/issues/3498)]
* control-plane: Add `CaseInsensitive` flag to service-routers that allows paths and path prefixes to ignore URL upper and lower casing. [[GH-3502](https://github.com/hashicorp/consul-k8s/issues/3502)]

BUG FIXES:

* consul-telemetry-collector: fix args to consul-dataplane when global.acls.manageSystemACLs [[GH-3184](https://github.com/hashicorp/consul-k8s/issues/3184)]

NOTES:

* build: Releases will now also be available as Debian and RPM packages for the arm64 architecture, refer to the
[Official Packaging Guide](https://www.hashicorp.com/official-packaging-guide) for more information. [[GH-3428](https://github.com/hashicorp/consul-k8s/issues/3428)]

## 1.3.3 (February 15, 2024)

FEATURES:

* helm: introduces `global.metrics.datadog` overrides to streamline consul-k8s datadog integration.
helm: introduces `server.enableAgentDebug` to expose agent [`enable_debug`](https://developer.hashicorp.com/consul/docs/agent/config/config-files#enable_debug) configuration.
helm: introduces `global.metrics.disableAgentHostName` to expose agent [`telemetry.disable_hostname`](https://developer.hashicorp.com/consul/docs/agent/config/config-files#telemetry-disable_hostname) configuration.
helm: introduces `global.metrics.enableHostMetrics` to expose agent [`telemetry.enable_host_metrics`](https://developer.hashicorp.com/consul/docs/agent/config/config-files#telemetry-enable_host_metrics) configuration.
helm: introduces `global.metrics.prefixFilter` to expose agent [`telemetry.prefix_filter`](https://developer.hashicorp.com/consul/docs/agent/config/config-files#telemetry-prefix_filter) configuration.
helm: introduces `global.metrics.datadog.dogstatsd.dogstatsdAddr` to expose agent [`telemetry.dogstatsd_addr`](https://developer.hashicorp.com/consul/docs/agent/config/config-files#telemetry-dogstatsd_addr) configuration.
helm: introduces `global.metrics.datadog.dogstatsd.dogstatsdTags` to expose agent [`telemetry.dogstatsd_tags`](https://developer.hashicorp.com/consul/docs/agent/config/config-files#telemetry-dogstatsd_tags) configuration.
helm: introduces required `ad.datadoghq.com/` annotations and `tags.datadoghq.com/` labels for integration with [Datadog Autodiscovery](https://docs.datadoghq.com/integrations/consul/?tab=containerized) and [Datadog Unified Service Tagging](https://docs.datadoghq.com/getting_started/tagging/unified_service_tagging/?tab=kubernetes#serverless-environment) for Consul.
helm: introduces automated unix domain socket hostPath mounting for containerized integration with datadog within consul-server statefulset.
helm: introduces `global.metrics.datadog.otlp` override options to allow OTLP metrics forwarding to Datadog Agent.
control-plane: adds `server-acl-init` datadog agent token creation for datadog integration. [[GH-3407](https://github.com/hashicorp/consul-k8s/issues/3407)]

IMPROVEMENTS:

* Upgrade to use Go 1.21.7. [[GH-3591](https://github.com/hashicorp/consul-k8s/issues/3591)]
* api-gateway: Apply `connectInject.initContainer.resources` to the init container for API gateway Pods. [[GH-3531](https://github.com/hashicorp/consul-k8s/issues/3531)]
* cni: When CNI is enabled, set ReadOnlyRootFilesystem=true and AllowPrivilegeEscalation=false for mesh pod init containers and AllowPrivilegeEscalation=false for consul-dataplane containers (ReadOnlyRootFilesystem was already true for consul-dataplane containers). [[GH-3498](https://github.com/hashicorp/consul-k8s/issues/3498)]
* control-plane: Add `CaseInsensitive` flag to service-routers that allows paths and path prefixes to ignore URL upper and lower casing. [[GH-3502](https://github.com/hashicorp/consul-k8s/issues/3502)]
* helm: Change `/bin/sh -ec "<command>"` to `/bin/sh -ec "exec <command>"` in helm deployments [[GH-3548](https://github.com/hashicorp/consul-k8s/issues/3548)]

BUG FIXES:

* api-gateway: fix issue where external annotations and labels are being incorrectly deleted on services controlled by the API Gateway [[GH-3597](https://github.com/hashicorp/consul-k8s/issues/3597)]
* mesh-gw: update capabilities on the security context needed for the dataplane container.
Adds NET_BIND_SERVICE to capabilities.add
Adds ALL to capabilities.drop unless .Values.meshGateway.hostNetwork is true [[GH-3549](https://github.com/hashicorp/consul-k8s/issues/3549)]

## 1.2.6 (February 15, 2024)

FEATURES:

* helm: introduces `global.metrics.datadog` overrides to streamline consul-k8s datadog integration.
helm: introduces `server.enableAgentDebug` to expose agent [`enable_debug`](https://developer.hashicorp.com/consul/docs/agent/config/config-files#enable_debug) configuration.
helm: introduces `global.metrics.disableAgentHostName` to expose agent [`telemetry.disable_hostname`](https://developer.hashicorp.com/consul/docs/agent/config/config-files#telemetry-disable_hostname) configuration.
helm: introduces `global.metrics.enableHostMetrics` to expose agent [`telemetry.enable_host_metrics`](https://developer.hashicorp.com/consul/docs/agent/config/config-files#telemetry-enable_host_metrics) configuration.
helm: introduces `global.metrics.prefixFilter` to expose agent [`telemetry.prefix_filter`](https://developer.hashicorp.com/consul/docs/agent/config/config-files#telemetry-prefix_filter) configuration.
helm: introduces `global.metrics.datadog.dogstatsd.dogstatsdAddr` to expose agent [`telemetry.dogstatsd_addr`](https://developer.hashicorp.com/consul/docs/agent/config/config-files#telemetry-dogstatsd_addr) configuration.
helm: introduces `global.metrics.datadog.dogstatsd.dogstatsdTags` to expose agent [`telemetry.dogstatsd_tags`](https://developer.hashicorp.com/consul/docs/agent/config/config-files#telemetry-dogstatsd_tags) configuration.
helm: introduces required `ad.datadoghq.com/` annotations and `tags.datadoghq.com/` labels for integration with [Datadog Autodiscovery](https://docs.datadoghq.com/integrations/consul/?tab=containerized) and [Datadog Unified Service Tagging](https://docs.datadoghq.com/getting_started/tagging/unified_service_tagging/?tab=kubernetes#serverless-environment) for Consul.
helm: introduces automated unix domain socket hostPath mounting for containerized integration with datadog within consul-server statefulset.
helm: introduces `global.metrics.datadog.otlp` override options to allow OTLP metrics forwarding to Datadog Agent.
control-plane: adds `server-acl-init` datadog agent token creation for datadog integration. [[GH-3407](https://github.com/hashicorp/consul-k8s/issues/3407)]

IMPROVEMENTS:

* Upgrade to use Go 1.21.7. [[GH-3591](https://github.com/hashicorp/consul-k8s/issues/3591)]
* api-gateway: Apply `connectInject.initContainer.resources` to the init container for API gateway Pods. [[GH-3531](https://github.com/hashicorp/consul-k8s/issues/3531)]
* cni: When CNI is enabled, set ReadOnlyRootFilesystem=true and AllowPrivilegeEscalation=false for mesh pod init containers and AllowPrivilegeEscalation=false for consul-dataplane containers (ReadOnlyRootFilesystem was already true for consul-dataplane containers). [[GH-3498](https://github.com/hashicorp/consul-k8s/issues/3498)]
* control-plane: Changed the container ordering in connect-inject to insert consul-dataplane container first if lifecycle is enabled. Container ordering is unchanged if lifecycle is disabled. [[GH-2743](https://github.com/hashicorp/consul-k8s/issues/2743)]
* helm: Change `/bin/sh -ec "<command>"` to `/bin/sh -ec "exec <command>"` in helm deployments [[GH-3548](https://github.com/hashicorp/consul-k8s/issues/3548)]

BUG FIXES:

* api-gateway: fix issue where external annotations and labels are being incorrectly deleted on services controlled by the API Gateway [[GH-3597](https://github.com/hashicorp/consul-k8s/issues/3597)]
* mesh-gw: update capabilities on the security context needed for the dataplane container.
Adds NET_BIND_SERVICE to capabilities.add
Adds ALL to capabilities.drop unless .Values.meshGateway.hostNetwork is true [[GH-3549](https://github.com/hashicorp/consul-k8s/issues/3549)]

## 1.1.10 (February 15, 2024)

IMPROVEMENTS:

* Upgrade to use Go 1.21.7. [[GH-3591](https://github.com/hashicorp/consul-k8s/issues/3591)]
* cni: When CNI is enabled, set ReadOnlyRootFilesystem=true and AllowPrivilegeEscalation=false for mesh pod init containers and AllowPrivilegeEscalation=false for consul-dataplane containers (ReadOnlyRootFilesystem was already true for consul-dataplane containers). [[GH-3498](https://github.com/hashicorp/consul-k8s/issues/3498)]
* helm: Change `/bin/sh -ec "<command>"` to `/bin/sh -ec "exec <command>"` in helm deployments [[GH-3548](https://github.com/hashicorp/consul-k8s/issues/3548)]

BUG FIXES:

* mesh-gw: update capabilities on the security context needed for the dataplane container.
Adds NET_BIND_SERVICE to capabilities.add
Adds ALL to capabilities.drop unless .Values.meshGateway.hostNetwork is true [[GH-3549](https://github.com/hashicorp/consul-k8s/issues/3549)]

## 1.3.2 (Jan 25, 2024)

SECURITY:

* Update `golang.org/x/crypto` to v0.17.0 to address [CVE-2023-48795](https://nvd.nist.gov/vuln/detail/CVE-2023-48795). [[GH-3442](https://github.com/hashicorp/consul-k8s/issues/3442)]
* Upgrade OpenShift container images to use `ubi-minimal:9.3` as the base image. [[GH-3418](https://github.com/hashicorp/consul-k8s/issues/3418)]

IMPROVEMENTS:

* Upgrade to use Go 1.21.6. [[GH-3478](https://github.com/hashicorp/consul-k8s/issues/3478)]
* control-plane: Add new `consul.hashicorp.com/sidecar-proxy-startup-failure-seconds` and `consul.hashicorp.com/sidecar-proxy-liveness-failure-seconds` annotations that allow users to manually configure startup and liveness probes for Envoy sidecar proxies. [[GH-3450](https://github.com/hashicorp/consul-k8s/issues/3450)]
* control-plane: reduce Consul Catalog API requests required for endpoints reconcile in large clusters [[GH-3322](https://github.com/hashicorp/consul-k8s/issues/3322)]

BUG FIXES:

* api-gateway: fix issue where deleting an http-route in a non-default namespace would not remove the route from Consul. [[GH-3440](https://github.com/hashicorp/consul-k8s/issues/3440)]

## 1.2.5 (Jan 25, 2024)

SECURITY:

* Update `golang.org/x/crypto` to v0.17.0 to address [CVE-2023-48795](https://nvd.nist.gov/vuln/detail/CVE-2023-48795). [[GH-3442](https://github.com/hashicorp/consul-k8s/issues/3442)]
* Upgrade to use `ubi-minimal:9.3` for OpenShift container images. [[GH-3418](https://github.com/hashicorp/consul-k8s/issues/3418)]

IMPROVEMENTS:

* Upgrade to use Go 1.21.6. [[GH-3478](https://github.com/hashicorp/consul-k8s/issues/3478)]
* control-plane: Add new `consul.hashicorp.com/sidecar-proxy-startup-failure-seconds` and `consul.hashicorp.com/sidecar-proxy-liveness-failure-seconds` annotations that allow users to manually configure startup and liveness probes for Envoy sidecar proxies. [[GH-3450](https://github.com/hashicorp/consul-k8s/issues/3450)]
* control-plane: reduce Consul Catalog API requests required for endpoints reconcile in large clusters [[GH-3322](https://github.com/hashicorp/consul-k8s/issues/3322)]

BUG FIXES:

* api-gateway: fix issue where deleting an http-route in a non-default namespace would not remove the route from Consul. [[GH-3440](https://github.com/hashicorp/consul-k8s/issues/3440)]

## 1.1.9 (Jan 25, 2024)

SECURITY:

* Update `golang.org/x/crypto` to v0.17.0 to address [CVE-2023-48795](https://nvd.nist.gov/vuln/detail/CVE-2023-48795). [[GH-3442](https://github.com/hashicorp/consul-k8s/issues/3442)]
* Upgrade to use `ubi-minimal:9.3` for OpenShift container images. [[GH-3418](https://github.com/hashicorp/consul-k8s/issues/3418)]

IMPROVEMENTS:

* Upgrade to use Go 1.21.6. [[GH-3478](https://github.com/hashicorp/consul-k8s/issues/3478)]
* control-plane: Add new `consul.hashicorp.com/sidecar-proxy-startup-failure-seconds` and `consul.hashicorp.com/sidecar-proxy-liveness-failure-seconds` annotations that allow users to manually configure startup and liveness probes for Envoy sidecar proxies. [[GH-3450](https://github.com/hashicorp/consul-k8s/issues/3450)]
* control-plane: reduce Consul Catalog API requests required for endpoints reconcile in large clusters [[GH-3322](https://github.com/hashicorp/consul-k8s/issues/3322)]

## 1.3.1 (December 19, 2023)

SECURITY:

* Update Envoy version to 1.25.11 to address [CVE-2023-44487](https://github.com/envoyproxy/envoy/security/advisories/GHSA-jhv4-f7mr-xx76) [[GH-3118](https://github.com/hashicorp/consul-k8s/issues/3118)]
* Update `github.com/golang-jwt/jwt/v4` to v4.5.0 to address [PRISMA-2022-0270](https://github.com/golang-jwt/jwt/issues/258). [[GH-3237](https://github.com/hashicorp/consul-k8s/issues/3237)]
* Upgrade to use Go 1.20.12. This resolves CVEs
[CVE-2023-45283](https://nvd.nist.gov/vuln/detail/CVE-2023-45283): (`path/filepath`) recognize \??\ as a Root Local Device path prefix (Windows)
[CVE-2023-45284](https://nvd.nist.gov/vuln/detail/CVE-2023-45285): recognize device names with trailing spaces and superscripts (Windows)
[CVE-2023-39326](https://nvd.nist.gov/vuln/detail/CVE-2023-39326): (`net/http`) limit chunked data overhead
[CVE-2023-45285](https://nvd.nist.gov/vuln/detail/CVE-2023-45285): (`cmd/go`) go get may unexpectedly fallback to insecure git [[GH-3312](https://github.com/hashicorp/consul-k8s/issues/3312)]

FEATURES:

* control-plane: adds a named port, `prometheus`, to the `consul-dataplane` sidecar for use with [Prometheus operator](https://github.com/prometheus-operator/prometheus-operator/blob/main/Documentation/api.md#podmetricsendpoint). [[GH-3222](https://github.com/hashicorp/consul-k8s/issues/3222)]
* crd: adds the [`retryOn`](https://developer.hashicorp.com/consul/docs/connect/config-entries/service-router#routes-destination-retryon) field to the ServiceRouter CRD. [[GH-3308](https://github.com/hashicorp/consul-k8s/issues/3308)]
* helm: add persistentVolumeClaimRetentionPolicy variable for managing Statefulsets PVC retain policy when deleting or downsizing the statefulset. [[GH-3180](https://github.com/hashicorp/consul-k8s/issues/3180)]

IMPROVEMENTS:

* cli: Add -o json (-output-format json) to `consul-k8s proxy list` command that returns the result in json format. [[GH-3221](https://github.com/hashicorp/consul-k8s/issues/3221)]
* cli: Add consul-k8s proxy stats command line interface that outputs the localhost:19000/stats of envoy in the pod [[GH-3158](https://github.com/hashicorp/consul-k8s/issues/3158)]
* control-plane: Add new `consul.hashicorp.com/proxy-config-map` annotation that allows for setting values in the opaque config map for proxy service registrations. [[GH-3347](https://github.com/hashicorp/consul-k8s/issues/3347)]
* helm: add validation that global.cloud.enabled is not set with externalServers.hosts set to HCP-managed clusters [[GH-3315](https://github.com/hashicorp/consul-k8s/issues/3315)]

BUG FIXES:

* consul-telemetry-collector: add telemetryCollector.cloud.resourceId that works even when not global.cloud.enabled [[GH-3219](https://github.com/hashicorp/consul-k8s/issues/3219)]
* consul-telemetry-collector: fix deployments to non-default namespaces when global.enableConsulNamespaces [[GH-3215](https://github.com/hashicorp/consul-k8s/issues/3215)]
* consul-telemetry-collector: fix args to consul-dataplane when global.acls.manageSystemACLs [[GH-3184](https://github.com/hashicorp/consul-k8s/issues/3184)]
* control-plane: Fixes a bug with the control-plane CLI validation where the consul-dataplane sidecar CPU request is compared against the memory limit instead of the CPU limit. [[GH-3209](https://github.com/hashicorp/consul-k8s/issues/3209)]
* control-plane: Only delete ACL tokens matched Pod UID in Service Registration metadata [[GH-3210](https://github.com/hashicorp/consul-k8s/issues/3210)]
* control-plane: fixes an issue with the server-acl-init job where the job would fail on upgrades due to consul server ip address changes. [[GH-3137](https://github.com/hashicorp/consul-k8s/issues/3137)]
* control-plane: only alert on valid errors, not timeouts in gateway [[GH-3128](https://github.com/hashicorp/consul-k8s/issues/3128)]
* control-plane: remove extraneous error log in v2 pod controller when a pod is scheduled, but not yet allocated an IP. [[GH-3162](https://github.com/hashicorp/consul-k8s/issues/3162)]
* control-plane: remove extraneous error log in v2 pod controller when attempting to delete ACL tokens. [[GH-3172](https://github.com/hashicorp/consul-k8s/issues/3172)]
* control-plane: Remove virtual nodes in the Consul Catalog when they do not have any services listed. [[GH-3307](https://github.com/hashicorp/consul-k8s/issues/3307)]
* mesh: prevent extra-config from being loaded twice (and erroring for segment config) on clients and servers. [[GH-3337](https://github.com/hashicorp/consul-k8s/issues/3337)]

## 1.2.4 (December 19, 2023)

SECURITY:

* Update `github.com/golang-jwt/jwt/v4` to v4.5.0 to address [PRISMA-2022-0270](https://github.com/golang-jwt/jwt/issues/258). [[GH-3237](https://github.com/hashicorp/consul-k8s/issues/3237)]
* Upgrade to use Go 1.20.12. This resolves CVEs
[CVE-2023-45283](https://nvd.nist.gov/vuln/detail/CVE-2023-45283): (`path/filepath`) recognize \??\ as a Root Local Device path prefix (Windows)
[CVE-2023-45284](https://nvd.nist.gov/vuln/detail/CVE-2023-45285): recognize device names with trailing spaces and superscripts (Windows)
[CVE-2023-39326](https://nvd.nist.gov/vuln/detail/CVE-2023-39326): (`net/http`) limit chunked data overhead
[CVE-2023-45285](https://nvd.nist.gov/vuln/detail/CVE-2023-45285): (`cmd/go`) go get may unexpectedly fallback to insecure git [[GH-3312](https://github.com/hashicorp/consul-k8s/issues/3312)]

FEATURES:

* crd: adds the [`retryOn`](https://developer.hashicorp.com/consul/docs/connect/config-entries/service-router#routes-destination-retryon) field to the ServiceRouter CRD. [[GH-3308](https://github.com/hashicorp/consul-k8s/issues/3308)]
* helm: add persistentVolumeClaimRetentionPolicy variable for managing Statefulsets PVC retain policy when deleting or downsizing the statefulset. [[GH-3180](https://github.com/hashicorp/consul-k8s/issues/3180)]

IMPROVEMENTS:

* cli: Add -o json (-output-format json) to `consul-k8s proxy list` command that returns the result in json format. [[GH-3221](https://github.com/hashicorp/consul-k8s/issues/3221)]
* cli: Add consul-k8s proxy stats command line interface that outputs the localhost:19000/stats of envoy in the pod [[GH-3158](https://github.com/hashicorp/consul-k8s/issues/3158)]
* control-plane: Add new `consul.hashicorp.com/proxy-config-map` annotation that allows for setting values in the opaque config map for proxy service registrations. [[GH-3347](https://github.com/hashicorp/consul-k8s/issues/3347)]
* helm: add validation that global.cloud.enabled is not set with externalServers.hosts set to HCP-managed clusters [[GH-3315](https://github.com/hashicorp/consul-k8s/issues/3315)]

BUG FIXES:

* consul-telemetry-collector: add telemetryCollector.cloud.resourceId that works even when not global.cloud.enabled [[GH-3219](https://github.com/hashicorp/consul-k8s/issues/3219)]
* consul-telemetry-collector: fix deployments to non-default namespaces when global.enableConsulNamespaces [[GH-3215](https://github.com/hashicorp/consul-k8s/issues/3215)]
* consul-telemetry-collector: fix args to consul-dataplane when global.acls.manageSystemACLs [[GH-3184](https://github.com/hashicorp/consul-k8s/issues/3215)]
* control-plane: Only delete ACL tokens matched Pod UID in Service Registration metadata [[GH-3210](https://github.com/hashicorp/consul-k8s/issues/3210)]
* control-plane: fixes an issue with the server-acl-init job where the job would fail on upgrades due to consul server ip address changes. [[GH-3137](https://github.com/hashicorp/consul-k8s/issues/3137)]
* control-plane: normalize the `partition` and `namespace` fields in V1 CRDs when comparing with saved version of the config-entry. [[GH-3284](https://github.com/hashicorp/consul-k8s/issues/3284)]
* control-plane: Remove virtual nodes in the Consul Catalog when they do not have any services listed. [[GH-3307](https://github.com/hashicorp/consul-k8s/issues/3307)]
* mesh: prevent extra-config from being loaded twice (and erroring for segment config) on clients and servers. [[GH-3337](https://github.com/hashicorp/consul-k8s/issues/3337)]

## 1.1.8 (December 19, 2023)

SECURITY:

* Update `github.com/golang-jwt/jwt/v4` to v4.5.0 to address [PRISMA-2022-0270](https://github.com/golang-jwt/jwt/issues/258). [[GH-3237](https://github.com/hashicorp/consul-k8s/issues/3237)]
* Upgrade to use Go 1.20.12. This resolves CVEs
[CVE-2023-45283](https://nvd.nist.gov/vuln/detail/CVE-2023-45283): (`path/filepath`) recognize \??\ as a Root Local Device path prefix (Windows)
[CVE-2023-45284](https://nvd.nist.gov/vuln/detail/CVE-2023-45285): recognize device names with trailing spaces and superscripts (Windows)
[CVE-2023-39326](https://nvd.nist.gov/vuln/detail/CVE-2023-39326): (`net/http`) limit chunked data overhead
[CVE-2023-45285](https://nvd.nist.gov/vuln/detail/CVE-2023-45285): (`cmd/go`) go get may unexpectedly fallback to insecure git [[GH-3312](https://github.com/hashicorp/consul-k8s/issues/3312)]

FEATURES:

* crd: adds the [`retryOn`](https://developer.hashicorp.com/consul/docs/connect/config-entries/service-router#routes-destination-retryon) field to the ServiceRouter CRD. [[GH-3308](https://github.com/hashicorp/consul-k8s/issues/3308)]
* helm: add persistentVolumeClaimRetentionPolicy variable for managing Statefulsets PVC retain policy when deleting or downsizing the statefulset. [[GH-3180](https://github.com/hashicorp/consul-k8s/issues/3180)]

IMPROVEMENTS:

* cli: Add -o json (-output-format json) to `consul-k8s proxy list` command that returns the result in json format. [[GH-3221](https://github.com/hashicorp/consul-k8s/issues/3221)]
* cli: Add consul-k8s proxy stats command line interface that outputs the localhost:19000/stats of envoy in the pod [[GH-3158](https://github.com/hashicorp/consul-k8s/issues/3158)]
* control-plane: Add new `consul.hashicorp.com/proxy-config-map` annotation that allows for setting values in the opaque config map for proxy service registrations. [[GH-3347](https://github.com/hashicorp/consul-k8s/issues/3347)]
* helm: add validation that global.cloud.enabled is not set with externalServers.hosts set to HCP-managed clusters [[GH-3315](https://github.com/hashicorp/consul-k8s/issues/3315)]

BUG FIXES:

* consul-telemetry-collector: add telemetryCollector.cloud.resourceId that works even when not global.cloud.enabled [[GH-3219](https://github.com/hashicorp/consul-k8s/issues/3219)]
* consul-telemetry-collector: fix deployments to non-default namespaces when global.enableConsulNamespaces [[GH-3215](https://github.com/hashicorp/consul-k8s/issues/3215)]
* consul-telemetry-collector: fix args to consul-dataplane when global.acls.manageSystemACLs [[GH-3184](https://github.com/hashicorp/consul-k8s/issues/3184)]
* control-plane: Only delete ACL tokens matched Pod UID in Service Registration metadata [[GH-3210](https://github.com/hashicorp/consul-k8s/issues/3210)]
* control-plane: fixes an issue with the server-acl-init job where the job would fail on upgrades due to consul server ip address changes. [[GH-3137](https://github.com/hashicorp/consul-k8s/issues/3137)]
* control-plane: Remove virtual nodes in the Consul Catalog when they do not have any services listed. [[GH-3137](https://github.com/hashicorp/consul-k8s/issues/3137)]
* mesh: prevent extra-config from being loaded twice (and erroring for segment config) on clients and servers. [[GH-3337](https://github.com/hashicorp/consul-k8s/issues/3337)]

## 1.3.0 (November 8, 2023)

SECURITY:

* Update Envoy version to 1.25.11 to address [CVE-2023-44487](https://github.com/envoyproxy/envoy/security/advisories/GHSA-jhv4-f7mr-xx76) [[GH-3116](https://github.com/hashicorp/consul-k8s/issues/3116)]

FEATURES:

* :tada: This release provides the ability to preview Consul's v2 Catalog and Resource API if enabled.
The new model supports multi-port application deployments with only a single Envoy proxy.
Note that the v1 and v2 catalogs are not cross compatible, and not all Consul features are available within this v2 feature preview.
See the [v2 Catalog and Resource API documentation](https://developer.hashicorp.com/consul/docs/k8s/multiport) for more information.
The v2 Catalog and Resources API should be considered a feature preview within this release and should not be used in production environments.

### Limitations
* The v1 and v2 catalog APIs cannot run concurrently.
* The Consul UI must be disable. It does not support multi-port services or the v2 catalog API in this release.
* HCP Consul does not support multi-port services or the v2 catalog API in this release.

[[GH-2868]](https://github.com/hashicorp/consul-k8s/pull/2868)
[[GH-2883]](https://github.com/hashicorp/consul-k8s/pull/2883)
[[GH-2930]](https://github.com/hashicorp/consul-k8s/pull/2930)
[[GH-2967]](https://github.com/hashicorp/consul-k8s/pull/2967) [[GH-2941](https://github.com/hashicorp/consul-k8s/issues/2941)]
* Add the `PrioritizeByLocality` field to the `ServiceResolver` and `ProxyDefaults` CRDs. [[GH-2784](https://github.com/hashicorp/consul-k8s/issues/2784)]
* Set locality on services registered with connect-inject. [[GH-2346](https://github.com/hashicorp/consul-k8s/issues/2346)]
* api-gateway: Add support for response header modifiers in HTTPRoute filters [[GH-2904](https://github.com/hashicorp/consul-k8s/issues/2904)]
* api-gateway: add RouteRetryFilter and RouteTimeoutFilter CRDs [[GH-2735](https://github.com/hashicorp/consul-k8s/issues/2735)]
* helm: (Consul Enterprise) Adds rate limiting config to serviceDefaults CRD [[GH-2844](https://github.com/hashicorp/consul-k8s/issues/2844)]
* helm: add persistentVolumeClaimRetentionPolicy variable for managing Statefulsets PVC retain policy when deleting or downsizing the statefulset. [[GH-3180](https://github.com/hashicorp/consul-k8s/issues/3180)]

IMPROVEMENTS:

* (Consul Enterprise) Add support to provide inputs via helm for audit log related configuration [[GH-2265](https://github.com/hashicorp/consul-k8s/issues/2265)]
* cli: Add consul-k8s proxy stats command line interface that outputs the localhost:19000/stats of envoy in the pod [[GH-3158](https://github.com/hashicorp/consul-k8s/issues/3158)]
* control-plane: Changed the container ordering in connect-inject to insert consul-dataplane container first if lifecycle is enabled. Container ordering is unchanged if lifecycle is disabled. [[GH-2743](https://github.com/hashicorp/consul-k8s/issues/2743)]
* helm: Kubernetes v1.28 is now supported. Minimum tested version of Kubernetes is now v1.25. [[GH-3138](https://github.com/hashicorp/consul-k8s/issues/3138)]

BUG FIXES:

* control-plane: Set locality on sidecar proxies in addition to services when registering with connect-inject. [[GH-2748](https://github.com/hashicorp/consul-k8s/issues/2748)]
* control-plane: remove extraneous error log in v2 pod controller when a pod is scheduled, but not yet allocated an IP. [[GH-3162](https://github.com/hashicorp/consul-k8s/issues/3162)]
* control-plane: remove extraneous error log in v2 pod controller when attempting to delete ACL tokens. [[GH-3172](https://github.com/hashicorp/consul-k8s/issues/3172)]

## 1.2.3 (November 2, 2023)

SECURITY:

* Update Envoy version to 1.25.11 to address [CVE-2023-44487](https://github.com/envoyproxy/envoy/security/advisories/GHSA-jhv4-f7mr-xx76) [[GH-3119](https://github.com/hashicorp/consul-k8s/issues/3119)]
* Upgrade `google.golang.org/grpc` to 1.56.3.
This resolves vulnerability [CVE-2023-44487](https://nvd.nist.gov/vuln/detail/CVE-2023-44487). [[GH-3139](https://github.com/hashicorp/consul-k8s/issues/3139)]
* Upgrade to use Go 1.20.10 and `x/net` 0.17.0.
This resolves [CVE-2023-39325](https://nvd.nist.gov/vuln/detail/CVE-2023-39325)
/ [CVE-2023-44487](https://nvd.nist.gov/vuln/detail/CVE-2023-44487). [[GH-3085](https://github.com/hashicorp/consul-k8s/issues/3085)]

BUG FIXES:

* api-gateway: fix issue where missing `NET_BIND_SERVICE` capability prevented api-gateway `Pod` from starting up when deployed to OpenShift [[GH-3070](https://github.com/hashicorp/consul-k8s/issues/3070)]
* control-plane: only alert on valid errors, not timeouts in gateway [[GH-3128](https://github.com/hashicorp/consul-k8s/issues/3128)]
* crd: fix misspelling of preparedQuery field in ControlPlaneRequestLimit CRD [[GH-3001](https://github.com/hashicorp/consul-k8s/issues/3001)]

## 1.1.7 (November 2, 2023)

SECURITY:

* Update Envoy version to 1.25.11 to address [CVE-2023-44487](https://github.com/envoyproxy/envoy/security/advisories/GHSA-jhv4-f7mr-xx76) [[GH-3120](https://github.com/hashicorp/consul-k8s/issues/3120)]
* Upgrade `google.golang.org/grpc` to 1.56.3.
This resolves vulnerability [CVE-2023-44487](https://nvd.nist.gov/vuln/detail/CVE-2023-44487). [[GH-3139](https://github.com/hashicorp/consul-k8s/issues/3139)]
* Upgrade to use Go 1.20.10 and `x/net` 0.17.0.
This resolves [CVE-2023-39325](https://nvd.nist.gov/vuln/detail/CVE-2023-39325)
/ [CVE-2023-44487](https://nvd.nist.gov/vuln/detail/CVE-2023-44487). [[GH-3085](https://github.com/hashicorp/consul-k8s/issues/3085)]

## 1.0.11 (November 2, 2023)

SECURITY:

* Update Envoy version to 1.24.12 to address [CVE-2023-44487](https://github.com/envoyproxy/envoy/security/advisories/GHSA-jhv4-f7mr-xx76) [[GH-3121](https://github.com/hashicorp/consul-k8s/issues/3121)]
* Upgrade `google.golang.org/grpc` to 1.56.3.
This resolves vulnerability [CVE-2023-44487](https://nvd.nist.gov/vuln/detail/CVE-2023-44487). [[GH-3139](https://github.com/hashicorp/consul-k8s/issues/3139)]
* Upgrade to use Go 1.20.10 and `x/net` 0.17.0.
This resolves [CVE-2023-39325](https://nvd.nist.gov/vuln/detail/CVE-2023-39325)
/ [CVE-2023-44487](https://nvd.nist.gov/vuln/detail/CVE-2023-44487). [[GH-3085](https://github.com/hashicorp/consul-k8s/issues/3085)]

## 1.2.2 (September 21, 2023)

SECURITY:

* Upgrade to use Go 1.20.8. This resolves CVEs
  [CVE-2023-39320](https://github.com/advisories/GHSA-rxv8-v965-v333) (`cmd/go`),
  [CVE-2023-39318](https://github.com/advisories/GHSA-vq7j-gx56-rxjh) (`html/template`),
  [CVE-2023-39319](https://github.com/advisories/GHSA-vv9m-32rr-3g55) (`html/template`),
  [CVE-2023-39321](https://github.com/advisories/GHSA-9v7r-x7cv-v437) (`crypto/tls`), and
  [CVE-2023-39322](https://github.com/advisories/GHSA-892h-r6cr-53g4) (`crypto/tls`) [[GH-2936](https://github.com/hashicorp/consul-k8s/issues/2936)]

FEATURES:

* Add support for new observability service principal in cloud preset [[GH-2958](https://github.com/hashicorp/consul-k8s/issues/2958)]
* helm: Add ability to configure resource requests and limits for Gateway API deployments. [[GH-2723](https://github.com/hashicorp/consul-k8s/issues/2723)]

IMPROVEMENTS:

* Add NET_BIND_SERVICE capability to restricted security context used for consul-dataplane [[GH-2787](https://github.com/hashicorp/consul-k8s/issues/2787)]
* Add new value `global.argocd.enabled`. Set this to `true` when using ArgoCD to deploy this chart. [[GH-2785](https://github.com/hashicorp/consul-k8s/issues/2785)]
* Add support for running on GKE Autopilot. [[GH-2952](https://github.com/hashicorp/consul-k8s/issues/2952)]
* api-gateway: reduce log output when disconnecting from consul server [[GH-2880](https://github.com/hashicorp/consul-k8s/issues/2880)]
* control-plane: Improve performance for pod deletions by reducing the number of fetched tokens. [[GH-2910](https://github.com/hashicorp/consul-k8s/issues/2910)]
* control-plane: prevent updation of anonymous-token-policy and anonymous-token if anonymous-token-policy is already attached to the anonymous-token [[GH-2790](https://github.com/hashicorp/consul-k8s/issues/2790)]
* helm: Add `JWKSCluster` field to `JWTProvider` CRD. [[GH-2881](https://github.com/hashicorp/consul-k8s/issues/2881)]
* vault: Adds `namespace` to `secretsBackend.vault.connectCA` in Helm chart and annotation: "vault.hashicorp.com/namespace: namespace" to
  secretsBackend.vault.agentAnnotations, if "vault.hashicorp.com/namespace" annotation is not present.
  This provides a more convenient way to specify the Vault namespace than nested JSON in `connectCA.additionalConfig`. [[GH-2841](https://github.com/hashicorp/consul-k8s/issues/2841)]

BUG FIXES:

* audit-log: fix parsing error for some audit log configuration fields fail with uncovertible string to integer errors. [[GH-2905](https://github.com/hashicorp/consul-k8s/issues/2905)]
* bug: Remove `global.acls.nodeSelector` and `global.acls.annotations` from Gateway Resources Jobs [[GH-2869](https://github.com/hashicorp/consul-k8s/issues/2869)]
* control-plane: Fix issue where ACL tokens would have an empty pod name that prevented proper token cleanup. [[GH-2808](https://github.com/hashicorp/consul-k8s/issues/2808)]
* control-plane: When using transparent proxy or CNI, reduced required permissions by setting privileged to false. Privileged must be true when using OpenShift without CNI. [[GH-2755](https://github.com/hashicorp/consul-k8s/issues/2755)]
* helm: Update prometheus port and scheme annotations if tls is enabled [[GH-2782](https://github.com/hashicorp/consul-k8s/issues/2782)]
* ingress-gateway: Adds missing PassiveHealthCheck to IngressGateways CRD and updates missing fields on ServiceDefaults CRD [[GH-2796](https://github.com/hashicorp/consul-k8s/issues/2796)]

## 1.1.6 (September 21, 2023)

SECURITY:

* Upgrade to use Go 1.20.8. This resolves CVEs
  [CVE-2023-39320](https://github.com/advisories/GHSA-rxv8-v965-v333) (`cmd/go`),
  [CVE-2023-39318](https://github.com/advisories/GHSA-vq7j-gx56-rxjh) (`html/template`),
  [CVE-2023-39319](https://github.com/advisories/GHSA-vv9m-32rr-3g55) (`html/template`),
  [CVE-2023-39321](https://github.com/advisories/GHSA-9v7r-x7cv-v437) (`crypto/tls`), and
  [CVE-2023-39322](https://github.com/advisories/GHSA-892h-r6cr-53g4) (`crypto/tls`) [[GH-2936](https://github.com/hashicorp/consul-k8s/issues/2936)]

IMPROVEMENTS:

* control-plane: Improve performance for pod deletions by reducing the number of fetched tokens. [[GH-2910](https://github.com/hashicorp/consul-k8s/issues/2910)]
* vault: Adds `namespace` to `secretsBackend.vault.connectCA` in Helm chart and annotation: "vault.hashicorp.com/namespace: namespace" to
  secretsBackend.vault.agentAnnotations, if "vault.hashicorp.com/namespace" annotation is not present.
  This provides a more convenient way to specify the Vault namespace than nested JSON in `connectCA.additionalConfig`. [[GH-2841](https://github.com/hashicorp/consul-k8s/issues/2841)]

BUG FIXES:

* audit-log: fix parsing error for some audit log configuration fields fail with uncovertible string to integer errors. [[GH-2905](https://github.com/hashicorp/consul-k8s/issues/2905)]

## 1.0.10 (September 21, 2023)

SECURITY:

* Upgrade to use Go 1.19.13. This resolves CVEs
  [CVE-2023-39320](https://github.com/advisories/GHSA-rxv8-v965-v333) (`cmd/go`),
  [CVE-2023-39318](https://github.com/advisories/GHSA-vq7j-gx56-rxjh) (`html/template`),
  [CVE-2023-39319](https://github.com/advisories/GHSA-vv9m-32rr-3g55) (`html/template`),
  [CVE-2023-39321](https://github.com/advisories/GHSA-9v7r-x7cv-v437) (`crypto/tls`), and
  [CVE-2023-39322](https://github.com/advisories/GHSA-892h-r6cr-53g4) (`crypto/tls`) [[GH-2938](https://github.com/hashicorp/consul-k8s/issues/2938)]

IMPROVEMENTS:

* Add NET_BIND_SERVICE capability to restricted security context used for consul-dataplane [[GH-2787](https://github.com/hashicorp/consul-k8s/issues/2787)]
* Add new value `global.argocd.enabled`. Set this to `true` when using ArgoCD to deploy this chart. [[GH-2785](https://github.com/hashicorp/consul-k8s/issues/2785)]
* control-plane: Improve performance for pod deletions by reducing the number of fetched tokens. [[GH-2910](https://github.com/hashicorp/consul-k8s/issues/2910)]
* control-plane: prevent updation of anonymous-token-policy and anonymous-token if anonymous-token-policy is already attached to the anonymous-token [[GH-2790](https://github.com/hashicorp/consul-k8s/issues/2790)]
* vault: Adds `namespace` to `secretsBackend.vault.connectCA` in Helm chart and annotation: "vault.hashicorp.com/namespace: namespace" to
  secretsBackend.vault.agentAnnotations, if "vault.hashicorp.com/namespace" annotation is not present.
  This provides a more convenient way to specify the Vault namespace than nested JSON in `connectCA.additionalConfig`. [[GH-2841](https://github.com/hashicorp/consul-k8s/issues/2841)]

BUG FIXES:

* audit-log: fix parsing error for some audit log configuration fields fail with uncovertible string to integer errors. [[GH-2905](https://github.com/hashicorp/consul-k8s/issues/2905)]
* control-plane: Fix issue where ACL tokens would have an empty pod name that prevented proper token cleanup. [[GH-2808](https://github.com/hashicorp/consul-k8s/issues/2808)]
* control-plane: When using transparent proxy or CNI, reduced required permissions by setting privileged to false. Privileged must be true when using OpenShift without CNI. [[GH-2755](https://github.com/hashicorp/consul-k8s/issues/2755)]
* helm: Update prometheus port and scheme annotations if tls is enabled [[GH-2782](https://github.com/hashicorp/consul-k8s/issues/2782)]

## 1.2.1 (Aug 10, 2023)
BREAKING CHANGES:

* control-plane: All policies managed by consul-k8s will now be updated on upgrade. If you previously edited the policies after install, your changes will be overwritten. [[GH-2392](https://github.com/hashicorp/consul-k8s/issues/2392)]

SECURITY:

* Upgrade to use Go 1.20.6 and `x/net/http` 0.12.0.
  This resolves [CVE-2023-29406](https://github.com/advisories/GHSA-f8f7-69v5-w4vx)(`net/http`). [[GH-2642](https://github.com/hashicorp/consul-k8s/issues/2642)]
* Upgrade to use Go 1.20.7 and `x/net` 0.13.0.
  This resolves [CVE-2023-29409](https://nvd.nist.gov/vuln/detail/CVE-2023-29409)(`crypto/tls`)
  and [CVE-2023-3978](https://nvd.nist.gov/vuln/detail/CVE-2023-3978)(`net/html`). [[GH-2710](https://github.com/hashicorp/consul-k8s/issues/2710)]

FEATURES:

* Add support for configuring graceful shutdown proxy lifecycle management settings. [[GH-2233](https://github.com/hashicorp/consul-k8s/issues/2233)]
* api-gateway: adds ability to map privileged ports on Gateway listeners to unprivileged ports so that containers do not require additional privileges [[GH-2707](https://github.com/hashicorp/consul-k8s/issues/2707)]
* api-gateway: support deploying to OpenShift 4.11 [[GH-2184](https://github.com/hashicorp/consul-k8s/issues/2184)]
* helm: Adds `acls.resources` field which can be configured to override the `resource` settings for the `server-acl-init` and `server-acl-init-cleanup` Jobs. [[GH-2416](https://github.com/hashicorp/consul-k8s/issues/2416)]
* sync-catalog: add ability to support weighted loadbalancing by service annotation `consul.hashicorp.com/service-weight: <number>` [[GH-2293](https://github.com/hashicorp/consul-k8s/issues/2293)]

IMPROVEMENTS:

* (Consul Enterprise) Add support to provide inputs via helm for audit log related configuration [[GH-2370](https://github.com/hashicorp/consul-k8s/issues/2370)]
* (api-gateway) make API gateway controller less verbose [[GH-2524](https://github.com/hashicorp/consul-k8s/issues/2524)]
* Add support to provide the logLevel flag via helm for multiple low level components. Introduces the following fields
1. `global.acls.logLevel`
2. `global.tls.logLevel`
3. `global.federation.logLevel`
4. `global.gossipEncryption.logLevel`
5. `server.logLevel`
6. `client.logLevel`
7. `meshGateway.logLevel`
8. `ingressGateways.logLevel`
9. `terminatingGateways.logLevel`
10. `telemetryCollector.logLevel` [[GH-2302](https://github.com/hashicorp/consul-k8s/issues/2302)]
* control-plane: increase timeout after login for ACL replication to 60 seconds [[GH-2656](https://github.com/hashicorp/consul-k8s/issues/2656)]
* helm: adds values for `securityContext` and `annotations` on TLS and ACL init/cleanup jobs. [[GH-2525](https://github.com/hashicorp/consul-k8s/issues/2525)]
* helm: set container securityContexts to match the `restricted` Pod Security Standards policy to support running Consul in a namespace with restricted PSA enforcement enabled [[GH-2572](https://github.com/hashicorp/consul-k8s/issues/2572)]
* helm: update `imageConsulDataplane` value to `hashicorp/consul-dataplane:1.2.0` [[GH-2476](https://github.com/hashicorp/consul-k8s/issues/2476)]
* helm: update `image` value to `hashicorp/consul:1.16.0` [[GH-2476](https://github.com/hashicorp/consul-k8s/issues/2476)]

BUG FIXES:

* api-gateway: Fix creation of invalid Kubernetes Service when multiple Gateway listeners have the same port. [[GH-2413](https://github.com/hashicorp/consul-k8s/issues/2413)]
* api-gateway: fix helm install when setting copyAnnotations or nodeSelector [[GH-2597](https://github.com/hashicorp/consul-k8s/issues/2597)]
* api-gateway: fixes bug where envoy will silently reject RSA keys less than 2048 bits in length when not in FIPS mode, and
  will reject keys that are not 2048, 3072, or 4096 bits in length in FIPS mode. We now validate
  and reject invalid certs earlier. [[GH-2478](https://github.com/hashicorp/consul-k8s/issues/2478)]
* api-gateway: set route condition appropriately when parent ref includes non-existent section name [[GH-2420](https://github.com/hashicorp/consul-k8s/issues/2420)]
* control-plane: Always update ACL policies upon upgrade. [[GH-2392](https://github.com/hashicorp/consul-k8s/issues/2392)]
* control-plane: fix bug in endpoints controller when deregistering services from consul when a node is deleted. [[GH-2571](https://github.com/hashicorp/consul-k8s/issues/2571)]
* helm: fix CONSUL_LOGIN_DATACENTER for consul client-daemonset. [[GH-2652](https://github.com/hashicorp/consul-k8s/issues/2652)]
* helm: fix ui ingress manifest formatting, and exclude `ingressClass` when not defined. [[GH-2687](https://github.com/hashicorp/consul-k8s/issues/2687)]
* transparent-proxy: Fix issue where connect-inject lacked sufficient `mesh:write` privileges in some deployments,
  which prevented virtual IPs from persisting properly. [[GH-2520](https://github.com/hashicorp/consul-k8s/issues/2520)]

## 1.1.4 (Aug 10, 2023)

SECURITY:

* Upgrade to use Go 1.20.6 and `x/net/http` 0.12.0.
  This resolves [CVE-2023-29406](https://github.com/advisories/GHSA-f8f7-69v5-w4vx)(`net/http`). [[GH-2642](https://github.com/hashicorp/consul-k8s/issues/2642)]
* Upgrade to use Go 1.20.7 and `x/net` 0.13.0.
  This resolves [CVE-2023-29409](https://nvd.nist.gov/vuln/detail/CVE-2023-29409)(`crypto/tls`)
  and [CVE-2023-3978](https://nvd.nist.gov/vuln/detail/CVE-2023-3978)(`net/html`). [[GH-2710](https://github.com/hashicorp/consul-k8s/issues/2710)]

IMPROVEMENTS:

* Add support to provide the logLevel flag via helm for multiple low level components. Introduces the following fields
1. `global.acls.logLevel`
2. `global.tls.logLevel`
3. `global.federation.logLevel`
4. `global.gossipEncryption.logLevel`
5. `server.logLevel`
6. `client.logLevel`
7. `meshGateway.logLevel`
8. `ingressGateways.logLevel`
9. `terminatingGateways.logLevel`
10. `telemetryCollector.logLevel` [[GH-2302](https://github.com/hashicorp/consul-k8s/issues/2302)]
* control-plane: increase timeout after login for ACL replication to 60 seconds [[GH-2656](https://github.com/hashicorp/consul-k8s/issues/2656)]
* helm: adds values for `securityContext` and `annotations` on TLS and ACL init/cleanup jobs. [[GH-2525](https://github.com/hashicorp/consul-k8s/issues/2525)]
* helm: do not set container securityContexts by default on OpenShift < 4.11 [[GH-2678](https://github.com/hashicorp/consul-k8s/issues/2678)]
* helm: set container securityContexts to match the `restricted` Pod Security Standards policy to support running Consul in a namespace with restricted PSA enforcement enabled [[GH-2572](https://github.com/hashicorp/consul-k8s/issues/2572)]

BUG FIXES:

* control-plane: fix bug in endpoints controller when deregistering services from consul when a node is deleted. [[GH-2571](https://github.com/hashicorp/consul-k8s/issues/2571)]
* helm: fix CONSUL_LOGIN_DATACENTER for consul client-daemonset. [[GH-2652](https://github.com/hashicorp/consul-k8s/issues/2652)]
* helm: fix ui ingress manifest formatting, and exclude `ingressClass` when not defined. [[GH-2687](https://github.com/hashicorp/consul-k8s/issues/2687)]

## 1.0.9 (Aug 10, 2023)

SECURITY:

* Upgrade to use Go 1.19.11 and `x/net/http` 0.12.0.
  This resolves [CVE-2023-29406](https://github.com/advisories/GHSA-f8f7-69v5-w4vx)(`net/http`). [[GH-2650](https://github.com/hashicorp/consul-k8s/issues/2650)]
* Upgrade to use Go 1.19.12 and `x/net` 0.13.0.
  This resolves [CVE-2023-29409](https://nvd.nist.gov/vuln/detail/CVE-2023-29409)(`crypto/tls`)
  and [CVE-2023-3978](https://nvd.nist.gov/vuln/detail/CVE-2023-3978)(`net/html`). [[GH-2717](https://github.com/hashicorp/consul-k8s/issues/2717)]

IMPROVEMENTS:

* Add support to provide the logLevel flag via helm for multiple low level components. Introduces the following fields
1. `global.acls.logLevel`
2. `global.tls.logLevel`
3. `global.federation.logLevel`
4. `global.gossipEncryption.logLevel`
5. `server.logLevel`
6. `client.logLevel`
7. `meshGateway.logLevel`
8. `ingressGateways.logLevel`
9. `terminatingGateways.logLevel` [[GH-2302](https://github.com/hashicorp/consul-k8s/issues/2302)]
* control-plane: increase timeout after login for ACL replication to 60 seconds [[GH-2656](https://github.com/hashicorp/consul-k8s/issues/2656)]
* helm: adds values for `securityContext` and `annotations` on TLS and ACL init/cleanup jobs. [[GH-2525](https://github.com/hashicorp/consul-k8s/issues/2525)]
* helm: do not set container securityContexts by default on OpenShift < 4.11 [[GH-2678](https://github.com/hashicorp/consul-k8s/issues/2678)]
* helm: set container securityContexts to match the `restricted` Pod Security Standards policy to support running Consul in a namespace with restricted PSA enforcement enabled [[GH-2572](https://github.com/hashicorp/consul-k8s/issues/2572)]

BUG FIXES:

* control-plane: fix bug in endpoints controller when deregistering services from consul when a node is deleted. [[GH-2571](https://github.com/hashicorp/consul-k8s/issues/2571)]
* helm: fix CONSUL_LOGIN_DATACENTER for consul client-daemonset. [[GH-2652](https://github.com/hashicorp/consul-k8s/issues/2652)]
* helm: fix ui ingress manifest formatting, and exclude `ingressClass` when not defined. [[GH-2687](https://github.com/hashicorp/consul-k8s/issues/2687)]

## 0.49.8 (July 12, 2023)

IMPROVEMENTS:

* helm: Add `connectInject.prepareDataplanesUpgrade` setting for help upgrading to dataplanes. This setting is required if upgrading from non-dataplanes to dataplanes when ACLs are enabled. See https://developer.hashicorp.com/consul/docs/k8s/upgrade#upgrading-to-consul-dataplane for more information. [[GH-2514](https://github.com/hashicorp/consul-k8s/issues/2514)]

## 1.2.0 (June 28, 2023)

FEATURES:

* Add support for configuring Consul server-side rate limiting [[GH-2166](https://github.com/hashicorp/consul-k8s/issues/2166)]
* api-gateway: Add API Gateway for Consul on Kubernetes leveraging Consul native API Gateway configuration. [[GH-2152](https://github.com/hashicorp/consul-k8s/issues/2152)]
* crd: Add `mutualTLSMode` to the ProxyDefaults and ServiceDefaults CRDs and `allowEnablingPermissiveMutualTLS` to the Mesh CRD to support configuring permissive mutual TLS. [[GH-2100](https://github.com/hashicorp/consul-k8s/issues/2100)]
* helm: Add `JWTProvider` CRD for configuring the `jwt-provider` config entry. [[GH-2209](https://github.com/hashicorp/consul-k8s/issues/2209)]
* helm: Update the ServiceIntentions CRD to support `JWT` fields. [[GH-2213](https://github.com/hashicorp/consul-k8s/issues/2213)]

IMPROVEMENTS:

* cli: update minimum go version for project to 1.20. [[GH-2102](https://github.com/hashicorp/consul-k8s/issues/2102)]
* control-plane: add FIPS support [[GH-2165](https://github.com/hashicorp/consul-k8s/issues/2165)]
* control-plane: server ACL Init always appends both, the secrets from the serviceAccount's secretRefs and the one created by the Helm chart, to support Openshift secret handling. [[GH-1770](https://github.com/hashicorp/consul-k8s/issues/1770)]
* control-plane: set agent localities on Consul servers to the server node's `topology.kubernetes.io/region` label. [[GH-2093](https://github.com/hashicorp/consul-k8s/issues/2093)]
* control-plane: update alpine to 3.17 in the Docker image. [[GH-1934](https://github.com/hashicorp/consul-k8s/issues/1934)]
* control-plane: update minimum go version for project to 1.20. [[GH-2102](https://github.com/hashicorp/consul-k8s/issues/2102)]
* helm: Kubernetes v1.27 is now supported. Minimum tested version of Kubernetes is now v1.24. [[GH-2304](https://github.com/hashicorp/consul-k8s/issues/2304)]
* helm: Update the default amount of memory used by the connect-inject controller so that its less likely to get OOM killed. [[GH-2249](https://github.com/hashicorp/consul-k8s/issues/2249)]
* helm: add failover policy field to service resolver and proxy default CRDs [[GH-2030](https://github.com/hashicorp/consul-k8s/issues/2030)]
* helm: add samenessGroup CRD [[GH-2048](https://github.com/hashicorp/consul-k8s/issues/2048)]
* helm: add samenessGroup field to exported services CRD [[GH-2075](https://github.com/hashicorp/consul-k8s/issues/2075)]
* helm: add samenessGroup field to service resolver CRD [[GH-2086](https://github.com/hashicorp/consul-k8s/issues/2086)]
* helm: add samenessGroup field to source intention CRD [[GH-2097](https://github.com/hashicorp/consul-k8s/issues/2097)]
* helm: update `imageConsulDataplane` value to `hashicorp/consul-dataplane:1.1.0`. [[GH-1953](https://github.com/hashicorp/consul-k8s/issues/1953)]

SECURITY:

* Update [Go-Discover](https://github.com/hashicorp/go-discover) in the container has been updated to address [CVE-2020-14040](https://github.com/advisories/GHSA-5rcv-m4m3-hfh7) [[GH-2390](https://github.com/hashicorp/consul-k8s/issues/2390)]
* Bump Dockerfile base image to `alpine:3.18`. Resolves [CVE-2023-2650](https://github.com/advisories/GHSA-gqxg-9vfr-p9cg) vulnerability in openssl@3.0.8-r4 [[GH-2284](https://github.com/hashicorp/consul-k8s/issues/2284)]
* Fix Prometheus CVEs by bumping controller-runtime. [[GH-2183](https://github.com/hashicorp/consul-k8s/issues/2183)]
* Upgrade to use Go 1.20.4.
  This resolves vulnerabilities [CVE-2023-24537](https://github.com/advisories/GHSA-9f7g-gqwh-jpf5)(`go/scanner`),
  [CVE-2023-24538](https://github.com/advisories/GHSA-v4m2-x4rp-hv22)(`html/template`),
  [CVE-2023-24534](https://github.com/advisories/GHSA-8v5j-pwr7-w5f8)(`net/textproto`) and
  [CVE-2023-24536](https://github.com/advisories/GHSA-9f7g-gqwh-jpf5)(`mime/multipart`).
  Also, `golang.org/x/net` has been updated to v0.7.0 to resolve CVEs [CVE-2022-41721
  ](https://github.com/advisories/GHSA-fxg5-wq6x-vr4w
  ), [CVE-2022-27664](https://github.com/advisories/GHSA-69cg-p879-7622) and [CVE-2022-41723
  ](https://github.com/advisories/GHSA-vvpx-j8f3-3w6h
  .) [[GH-2102](https://github.com/hashicorp/consul-k8s/issues/2102)]

BUG FIXES:

* control-plane: Fix casing of the Enforce Consecutive 5xx field on Service Defaults and acceptance test fixtures. [[GH-2266](https://github.com/hashicorp/consul-k8s/issues/2266)]
* control-plane: fix issue where consul-connect-injector acl token was unintentionally being deleted and not recreated when a container was restarted due to a livenessProbe failure. [[GH-1914](https://github.com/hashicorp/consul-k8s/issues/1914)]

## 1.1.3 (June 28, 2023)
BREAKING CHANGES:

* control-plane: All policies managed by consul-k8s will now be updated on upgrade. If you previously edited the policies after install, your changes will be overwritten. [[GH-2392](https://github.com/hashicorp/consul-k8s/issues/2392)]

SECURITY:

* Bump Dockerfile base image to `alpine:3.18`. Resolves [CVE-2023-2650](https://github.com/advisories/GHSA-gqxg-9vfr-p9cg) vulnerability in openssl@3.0.8-r4 [[GH-2284](https://github.com/hashicorp/consul-k8s/issues/2284)]
* Update [Go-Discover](https://github.com/hashicorp/go-discover) in the container has been updated to address [CVE-2020-14040](https://github.com/advisories/GHSA-5rcv-m4m3-hfh7) [[GH-2390](https://github.com/hashicorp/consul-k8s/issues/2390)]

FEATURES:

* Add support for configuring graceful shutdown proxy lifecycle management settings. [[GH-2233](https://github.com/hashicorp/consul-k8s/issues/2233)]
* helm: Adds `acls.resources` field which can be configured to override the `resource` settings for the `server-acl-init` and `server-acl-init-cleanup` Jobs. [[GH-2416](https://github.com/hashicorp/consul-k8s/issues/2416)]
* sync-catalog: add ability to support weighted loadbalancing by service annotation `consul.hashicorp.com/service-weight: <number>` [[GH-2293](https://github.com/hashicorp/consul-k8s/issues/2293)]

IMPROVEMENTS:

* (Consul Enterprise) Add support to provide inputs via helm for audit log related configuration [[GH-2369](https://github.com/hashicorp/consul-k8s/issues/2369)]
* helm: Update the default amount of memory used by the connect-inject controller so that its less likely to get OOM killed. [[GH-2249](https://github.com/hashicorp/consul-k8s/issues/2249)]

BUG FIXES:

* control-plane: Always update ACL policies upon upgrade. [[GH-2392](https://github.com/hashicorp/consul-k8s/issues/2392)]
* control-plane: Fix casing of the Enforce Consecutive 5xx field on Service Defaults and acceptance test fixtures. [[GH-2266](https://github.com/hashicorp/consul-k8s/issues/2266)]

## 1.0.8 (June 28, 2023)
BREAKING CHANGES:

* control-plane: All policies managed by consul-k8s will now be updated on upgrade. If you previously edited the policies after install, your changes will be overwritten. [[GH-2392](https://github.com/hashicorp/consul-k8s/issues/2392)]

SECURITY:

* Bump Dockerfile base image for RedHat UBI `consul-k8s-control-plane` image to `ubi-minimal:9.2`. [[GH-2204](https://github.com/hashicorp/consul-k8s/issues/2204)]
* Bump Dockerfile base image to `alpine:3.18`. Resolves [CVE-2023-2650](https://github.com/advisories/GHSA-gqxg-9vfr-p9cg) vulnerability in openssl@3.0.8-r4 [[GH-2284](https://github.com/hashicorp/consul-k8s/issues/2284)]
* Bump `controller-runtime` to address CVEs in dependencies. [[GH-2225](https://github.com/hashicorp/consul-k8s/issues/2225)]
* Update [Go-Discover](https://github.com/hashicorp/go-discover) in the container has been updated to address [CVE-2020-14040](https://github.com/advisories/GHSA-5rcv-m4m3-hfh7) [[GH-2390](https://github.com/hashicorp/consul-k8s/issues/2390)]

FEATURES:

* Add support for configuring graceful shutdown proxy lifecycle management settings. [[GH-2233](https://github.com/hashicorp/consul-k8s/issues/2233)]
* helm: Adds `acls.resources` field which can be configured to override the `resource` settings for the `server-acl-init` and `server-acl-init-cleanup` Jobs. [[GH-2416](https://github.com/hashicorp/consul-k8s/issues/2416)]
* sync-catalog: add ability to support weighted loadbalancing by service annotation `consul.hashicorp.com/service-weight: <number>` [[GH-2293](https://github.com/hashicorp/consul-k8s/issues/2293)]

IMPROVEMENTS:

* (Consul Enterprise) Add support to provide inputs via helm for audit log related configuration [[GH-2265](https://github.com/hashicorp/consul-k8s/issues/2265)]
* helm: Update the default amount of memory used by the connect-inject controller so that its less likely to get OOM killed. [[GH-2249](https://github.com/hashicorp/consul-k8s/issues/2249)]

BUG FIXES:

* control-plane: Always update ACL policies upon upgrade. [[GH-2392](https://github.com/hashicorp/consul-k8s/issues/2392)]
* control-plane: Fix casing of the Enforce Consecutive 5xx field on Service Defaults and acceptance test fixtures. [[GH-2266](https://github.com/hashicorp/consul-k8s/issues/2266)]
* control-plane: add support for idleTimeout in the Service Router config [[GH-2156](https://github.com/hashicorp/consul-k8s/issues/2156)]
* control-plane: fix issue with json tags of service defaults fields EnforcingConsecutive5xx, MaxEjectionPercent and BaseEjectionTime. [[GH-2159](https://github.com/hashicorp/consul-k8s/issues/2159)]
* control-plane: fix issue with multiport pods crashlooping due to dataplane port conflicts by ensuring dns redirection is disabled for non-tproxy pods [[GH-2176](https://github.com/hashicorp/consul-k8s/issues/2176)]
* crd: fix bug on service intentions CRD causing some updates to be ignored. [[GH-2194](https://github.com/hashicorp/consul-k8s/issues/2194)]


## 0.49.7 (June 28, 2023)
BREAKING CHANGES:

* control-plane: All policies managed by consul-k8s will now be updated on upgrade. If you previously edited the policies after install, your changes will be overwritten. [[GH-2392](https://github.com/hashicorp/consul-k8s/issues/2392)]

SECURITY:

* Bump Dockerfile base image for RedHat UBI `consul-k8s-control-plane` image to `ubi-minimal:9.2`. [[GH-2204](https://github.com/hashicorp/consul-k8s/issues/2204)]
* Bump Dockerfile base image to `alpine:3.18`. Resolves [CVE-2023-2650](https://github.com/advisories/GHSA-gqxg-9vfr-p9cg) vulnerability in openssl@3.0.8-r4 [[GH-2284](https://github.com/hashicorp/consul-k8s/issues/2284)]

FEATURES:

* helm: Adds `acls.resources` field which can be configured to override the `resource` settings for the `server-acl-init` and `server-acl-init-cleanup` Jobs. [[GH-2416](https://github.com/hashicorp/consul-k8s/issues/2416)]

IMPROVEMENTS:

* (Consul Enterprise) Add support to provide inputs via helm for audit log related configuration [[GH-2265](https://github.com/hashicorp/consul-k8s/issues/2265)]
* helm: Update the default amount of memory used by the connect-inject controller so that its less likely to get OOM killed. [[GH-2249](https://github.com/hashicorp/consul-k8s/issues/2249)]

BUG FIXES:

* control-plane: Always update ACL policies upon upgrade. [[GH-2392](https://github.com/hashicorp/consul-k8s/issues/2392)]
* crd: fix bug on service intentions CRD causing some updates to be ignored. [[GH-2194](https://github.com/hashicorp/consul-k8s/issues/2194)]

## 1.1.2 (June 5, 2023)

SECURITY:

* Bump Dockerfile base image for RedHat UBI `consul-k8s-control-plane` image to `ubi-minimal:9.2`. [[GH-2204](https://github.com/hashicorp/consul-k8s/issues/2204)]
* Bump `controller-runtime` to address CVEs in dependencies. [[GH-2226](https://github.com/hashicorp/consul-k8s/issues/2226)]
* Upgrade to use Go 1.20.4.
This resolves vulnerabilities [CVE-2023-24537](https://github.com/advisories/GHSA-9f7g-gqwh-jpf5)(`go/scanner`),
[CVE-2023-24538](https://github.com/advisories/GHSA-v4m2-x4rp-hv22)(`html/template`),
[CVE-2023-24534](https://github.com/advisories/GHSA-8v5j-pwr7-w5f8)(`net/textproto`) and
[CVE-2023-24536](https://github.com/advisories/GHSA-9f7g-gqwh-jpf5)(`mime/multipart`).
Also, `golang.org/x/net` has been updated to v0.7.0 to resolve CVEs [CVE-2022-41721
](https://github.com/advisories/GHSA-fxg5-wq6x-vr4w
), [CVE-2022-27664](https://github.com/advisories/GHSA-69cg-p879-7622) and [CVE-2022-41723
](https://github.com/advisories/GHSA-vvpx-j8f3-3w6h
.) [[GH-2104](https://github.com/hashicorp/consul-k8s/issues/2104)]

FEATURES:

* Add support for consul-telemetry-collector to forward envoy metrics to an otelhttp compatible receiver or HCP [[GH-2134](https://github.com/hashicorp/consul-k8s/issues/2134)]
* consul-telemetry-collector: Configure envoy proxy config during registration when consul-telemetry-collector is enabled. [[GH-2143](https://github.com/hashicorp/consul-k8s/issues/2143)]
* sync-catalog: add ability to sync hostname from a Kubernetes Ingress resource to the Consul Catalog during service registration. [[GH-2098](https://github.com/hashicorp/consul-k8s/issues/2098)]

IMPROVEMENTS:

* cli: Add `consul-k8s config read` command that returns the helm configuration in yaml format. [[GH-2078](https://github.com/hashicorp/consul-k8s/issues/2078)]
* cli: add consul-telemetry-gateway allow-all intention for -demo [[GH-2262](https://github.com/hashicorp/consul-k8s/issues/2262)]
* cli: update cloud preset to enable telemetry collector [[GH-2205](https://github.com/hashicorp/consul-k8s/issues/2205)]
* consul-telemetry-collector: add acceptance tests for consul telemetry collector component [[GH-2195](https://github.com/hashicorp/consul-k8s/issues/2195)]

BUG FIXES:

* crd: fix bug on service intentions CRD causing some updates to be ignored. [[GH-2194](https://github.com/hashicorp/consul-k8s/issues/2083)]
* api-gateway: fix issue where the API Gateway controller is unable to start up successfully when Vault is configured as the secrets backend [[GH-2083](https://github.com/hashicorp/consul-k8s/issues/2083)]
* control-plane: add support for idleTimeout in the Service Router config [[GH-2156](https://github.com/hashicorp/consul-k8s/issues/2156)]
* control-plane: fix issue with json tags of service defaults fields EnforcingConsecutive5xx, MaxEjectionPercent and BaseEjectionTime. [[GH-2160](https://github.com/hashicorp/consul-k8s/issues/2160)]
* control-plane: fix issue with multiport pods crashlooping due to dataplane port conflicts by ensuring dns redirection is disabled for non-tproxy pods [[GH-2176](https://github.com/hashicorp/consul-k8s/issues/2176)]
* helm: add missing  `$HOST_IP` environment variable to to mesh gateway deployments. [[GH-1808](https://github.com/hashicorp/consul-k8s/issues/1808)]
* sync-catalog: fix issue where the sync-catalog ACL token were set with an incorrect ENV VAR. [[GH-2068](https://github.com/hashicorp/consul-k8s/issues/2068)]

## 1.0.7 (May 17, 2023)

SECURITY:

* Upgrade to use Go 1.19.9.
This resolves vulnerabilities [CVE-2023-24537](https://github.com/advisories/GHSA-9f7g-gqwh-jpf5)(`go/scanner`),
[CVE-2023-24538](https://github.com/advisories/GHSA-v4m2-x4rp-hv22)(`html/template`),
[CVE-2023-24534](https://github.com/advisories/GHSA-8v5j-pwr7-w5f8)(`net/textproto`) and
[CVE-2023-24536](https://github.com/advisories/GHSA-9f7g-gqwh-jpf5)(`mime/multipart`).
Also, `golang.org/x/net` has been updated to v0.7.0 to resolve CVEs [CVE-2022-41721
](https://github.com/advisories/GHSA-fxg5-wq6x-vr4w
), [CVE-2022-27664](https://github.com/advisories/GHSA-69cg-p879-7622) and [CVE-2022-41723
](https://github.com/advisories/GHSA-vvpx-j8f3-3w6h
.) [[GH-2108](https://github.com/hashicorp/consul-k8s/issues/2108)]

FEATURES:

* sync-catalog: add ability to sync hostname from a Kubernetes Ingress resource to the Consul Catalog during service registration. [[GH-2098](https://github.com/hashicorp/consul-k8s/issues/2098)]

IMPROVEMENTS:

* cli: Add `consul-k8s config read` command that returns the helm configuration in yaml format. [[GH-2078](https://github.com/hashicorp/consul-k8s/issues/2078)]
* helm: update `imageConsulDataplane` value to `hashicorp/consul-dataplane:1.0.2`, `image` value to `hashicorp/consul:1.14.7`, 
and `imageEnvoy` to `envoyproxy/envoy:v1.24.7`. [[GH-2140](https://github.com/hashicorp/consul-k8s/issues/2140)]

BUG FIXES:

* api-gateway: fix issue where the API Gateway controller is unable to start up successfully when Vault is configured as the secrets backend [[GH-2083](https://github.com/hashicorp/consul-k8s/issues/2083)]
* helm: add missing  `$HOST_IP` environment variable to to mesh gateway deployments. [[GH-1808](https://github.com/hashicorp/consul-k8s/issues/1808)]
* sync-catalog: fix issue where the sync-catalog ACL token were set with an incorrect ENV VAR. [[GH-2068](https://github.com/hashicorp/consul-k8s/issues/2068)]

## 0.49.6 (May 17, 2023)

SECURITY:

* Upgrade to use Go 1.19.9.
This resolves vulnerabilities [CVE-2023-24537](https://github.com/advisories/GHSA-9f7g-gqwh-jpf5)(`go/scanner`),
[CVE-2023-24538](https://github.com/advisories/GHSA-v4m2-x4rp-hv22)(`html/template`),
[CVE-2023-24534](https://github.com/advisories/GHSA-8v5j-pwr7-w5f8)(`net/textproto`) and
[CVE-2023-24536](https://github.com/advisories/GHSA-9f7g-gqwh-jpf5)(`mime/multipart`).
Also, `golang.org/x/net` has been updated to v0.7.0 to resolve CVEs [CVE-2022-41721
](https://github.com/advisories/GHSA-fxg5-wq6x-vr4w
), [CVE-2022-27664](https://github.com/advisories/GHSA-69cg-p879-7622) and [CVE-2022-41723
](https://github.com/advisories/GHSA-vvpx-j8f3-3w6h
.) [[GH-2110](https://github.com/hashicorp/consul-k8s/issues/2110)]

IMPROVEMENTS:

* helm: Set default `limits.cpu` resource setting to `null` for `consul-connect-inject-init` container to speed up registration times when onboarding services onto the mesh during the init container lifecycle. [[GH-2008](https://github.com/hashicorp/consul-k8s/issues/2008)]

## 1.1.1 (March 31, 2023)

IMPROVEMENTS:

* helm: Set default `limits.cpu` resource setting to `null` for `consul-connect-inject-init` container to speed up registration times when onboarding services onto the mesh during the init container lifecycle. [[GH-2008](https://github.com/hashicorp/consul-k8s/issues/2008)]
* helm: When the `global.acls.bootstrapToken` field is set and the content of the secret is empty, the bootstrap ACL token is written to that secret after bootstrapping ACLs. This applies to both the Vault and Consul secrets backends. [[GH-1920](https://github.com/hashicorp/consul-k8s/issues/1920)]

BUG FIXES:

* api-gateway: fix ACL issue where when adminPartitions and ACLs are enabled, API Gateway Controller is unable to create a new namespace in Consul [[GH-2029](https://github.com/hashicorp/consul-k8s/issues/2029)]
* api-gateway: fix issue where specifying an external server SNI name while using client nodes resulted in a TLS verification error. [[GH-2013](https://github.com/hashicorp/consul-k8s/issues/2013)]

## 1.0.6 (March 20, 2023)

IMPROVEMENTS:

* helm: Set default `limits.cpu` resource setting to `null` for `consul-connect-inject-init` container to speed up registration times when onboarding services onto the mesh during the init container lifecycle. [[GH-2008](https://github.com/hashicorp/consul-k8s/issues/2008)]

BUG FIXES:

* api-gateway: fix issue where specifying an external server SNI name while using client nodes resulted in a TLS verification error. [[GH-2013](https://github.com/hashicorp/consul-k8s/issues/2013)]

## 1.0.5 (March 9, 2023)

SECURITY:

* upgrade to use Go 1.19.6. This resolves vulnerabilities CVE-2022-41724 in crypto/tls and CVE-2022-41723 in net/http. [[GH-1976](https://github.com/hashicorp/consul-k8s/issues/1976)]

IMPROVEMENTS:

* control-plane: server ACL Init always appends both, the secrets from the serviceAccount's secretRefs and the one created by the Helm chart, to support Openshift secret handling. [[GH-1770](https://github.com/hashicorp/consul-k8s/issues/1770)]
* control-plane: update alpine to 3.17 in the Docker image. [[GH-1934](https://github.com/hashicorp/consul-k8s/issues/1934)]
* helm: update `imageConsulDataplane` value to `hashicorp/consul-dataplane:1.1.0`. [[GH-1953](https://github.com/hashicorp/consul-k8s/issues/1953)]

## 0.49.5 (March 9, 2023)

SECURITY:

* upgrade to use Go 1.19.6. This resolves vulnerabilities CVE-2022-41724 in crypto/tls and CVE-2022-41723 in net/http. [[GH-1975](https://github.com/hashicorp/consul-k8s/issues/1975)]

IMPROVEMENTS:

* cli: update minimum go version for project to 1.19. [[GH-1975](https://github.com/hashicorp/consul-k8s/issues/1975)]
* control-plane: server ACL Init always appends both, the secrets from the serviceAccount's secretRefs and the one created by the Helm chart, to support Openshift secret handling. [[GH-1770](https://github.com/hashicorp/consul-k8s/issues/1770)]
* control-plane: update alpine to 3.17 in the Docker image. [[GH-1934](https://github.com/hashicorp/consul-k8s/issues/1934)]
* control-plane: update minimum go version for project to 1.19. [[GH-1975](https://github.com/hashicorp/consul-k8s/issues/1975)]

BUG FIXES:

* control-plane: fix issue where consul-connect-injector acl token was unintentionally being deleted and not recreated when a container was restarted due to a livenessProbe failure. [[GH-1914](https://github.com/hashicorp/consul-k8s/issues/1914)]

## 1.1.0 (February 27, 2023)

BREAKING CHANGES:
* Helm:
  * Change defaults to exclude the `openebs` namespace from sidecar injection. If you previously had pods in that namespace
    that you wanted to be injected, you must now set `namespaceSelector` as follows:
  
    ```yaml
    connectInject:
      namespaceSelector: |
        matchExpressions:
        - key: "kubernetes.io/metadata.name"
          operator: "NotIn"
          values: ["kube-system","local-path-storage"]
    ```
    [[GH-1869](https://github.com/hashicorp/consul-k8s/pull/1869)]

IMPROVEMENTS:
* Helm:
  * CNI: Add `connectInject.cni.namespace` stanza which allows the CNI plugin resources to be deployed in a namespace other than the namespace that Consul is installed. [[GH-1756](https://github.com/hashicorp/consul-k8s/pull/1756)]
  * Kubernetes v1.26 is now supported. Minimum tested version of Kubernetes is now v1.23. [[GH-1852](https://github.com/hashicorp/consul-k8s/pull/1852)]
  * Add a `global.extraLabels` stanza to allow setting global Kubernetes labels for all components deployed by the `consul-k8s` Helm chart. [[GH-1778](https://github.com/hashicorp/consul-k8s/pull/1778)]
  * Add the `accessLogs` field to the `ProxyDefaults` CRD. [[GH-1816](https://github.com/hashicorp/consul-k8s/pull/1816)]
  * Add the `envoyExtensions` field to the `ProxyDefaults` and `ServiceDefaults` CRD. [[GH-1823]](https://github.com/hashicorp/consul-k8s/pull/1823)
  * Add the `balanceInboundConnections` field to the `ServiceDefaults` CRD. [[GH-1823]](https://github.com/hashicorp/consul-k8s/pull/1823)
  * Add the `upstreamConfig.overrides[].peer` field to the `ServiceDefaults` CRD. [[GH-1853]](https://github.com/hashicorp/consul-k8s/pull/1853)
* Control-Plane
  * Update minimum go version for project to 1.20 [[GH-1908](https://github.com/hashicorp/consul-k8s/pull/1908)]
  * Add support for the annotation `consul.hashicorp.com/use-proxy-health-check`. When this annotation is used by a service, it configures a readiness endpoint on Consul Dataplane and queries it instead of the proxy's inbound port which forwards requests to the application. [[GH-1824](https://github.com/hashicorp/consul-k8s/pull/1824)], [[GH-1841](https://github.com/hashicorp/consul-k8s/pull/1841)]
  * Add health check for synced services based on the status of the Kubernetes readiness probe on synced pod. [[GH-1821](https://github.com/hashicorp/consul-k8s/pull/1821)]
  * Remove extraneous `gnupg` dependency from `consul-k8s-control-plane` since it is no longer needed for validating binary artifacts prior to release. [[GH-1882](https://github.com/hashicorp/consul-k8s/pull/1882)]
  * Server ACL Init always appends both, the secrets from the serviceAccount's secretRefs and the one created by the Helm chart, to support Openshift secret handling. [[GH-1770](https://github.com/hashicorp/consul-k8s/pull/1770)]
  * Update alpine to 3.17 in the Docker image. [[GH-1934](https://github.com/hashicorp/consul-k8s/pull/1934)]
* CLI:
  * Update minimum go version for project to 1.20 [[GH-1908](https://github.com/hashicorp/consul-k8s/pull/1908)]
  * Add `consul-k8s proxy log podname` command for displaying and modifying Envoy log levels for a given Pod. [GH-1844](https://github.com/hashicorp/consul-k8s/pull/1844), [GH-1849](https://github.com/hashicorp/consul-k8s/pull/1849), [GH-1864](https://github.com/hashicorp/consul-k8s/pull/1864)

BUG FIXES:
* Control Plane
  * Don't incorrectly diff intention config entries when upgrading from Consul pre-1.12 to 1.12+ [[GH-1804](https://github.com/hashicorp/consul-k8s/pull/1804)]
  * Add discover binary to control-plane image [[GH-1749](https://github.com/hashicorp/consul-k8s/pull/1749)]
* Helm:
  * Don't pass in a CA file to the API Gateway controller when `externalServers.useSystemRoots` is `true`. [[GH-1743](https://github.com/hashicorp/consul-k8s/pull/1743)]
  * Use the correct autogenerated cert for the API Gateway Controller when connecting to servers versus clients. [[GH-1753](https://github.com/hashicorp/consul-k8s/pull/1753)]
* Security:
  * Upgrade to use Go 1.20.1 This resolves vulnerabilities [CVE-2022-41724](https://go.dev/issue/58001) in `crypto/tls` and [CVE-2022-41723](https://go.dev/issue/57855) in `net/http`. [[GH-1908](https://github.com/hashicorp/consul-k8s/pull/1908)]

## 1.0.3 (January 30, 2023)

IMPROVEMENTS:
* Helm:
  * Kubernetes v1.26 is now supported. Minimum tested version of Kubernetes is now v1.23. [[GH-1852](https://github.com/hashicorp/consul-k8s/pull/1852)]
  * Add a `global.extraLabels` stanza to allow setting global Kubernetes labels for all components deployed by the `consul-k8s` Helm chart. [[GH-1778](https://github.com/hashicorp/consul-k8s/pull/1778)]
* Control-Plane
  * Add support for the annotation `consul.hashicorp.com/use-proxy-health-check`. When this annotation is used by a service, it configures a readiness endpoint on Consul Dataplane and queries it instead of the proxy's inbound port which forwards requests to the application. [[GH-1824](https://github.com/hashicorp/consul-k8s/pull/1824)], [[GH-1841](https://github.com/hashicorp/consul-k8s/pull/1841)]
  * Add health check for synced services based on the status of the Kubernetes readiness probe on synced pod. [[GH-1821](https://github.com/hashicorp/consul-k8s/pull/1821)]

BUG FIXES:
* Control Plane
   * Don't incorrectly diff intention config entries when upgrading from Consul pre-1.12 to 1.12+ [[GH-1804](https://github.com/hashicorp/consul-k8s/pull/1804)]

## 0.49.3 (January 30, 2023)

IMPROVEMENTS:
* Helm:
  * Add a `global.extraLabels` stanza to allow setting global Kubernetes labels for all components deployed by the `consul-k8s` Helm chart. [[GH-1778](https://github.com/hashicorp/consul-k8s/pull/1778)]
* Control-Plane
  * Add support for the annotation `consul.hashicorp.com/use-proxy-health-check`. When this annotation is used by a service, it configures a readiness endpoint on Consul Dataplane and queries it instead of the proxy's inbound port which forwards requests to the application. [[GH-1824](https://github.com/hashicorp/consul-k8s/pull/1824)], [[GH-1843](https://github.com/hashicorp/consul-k8s/pull/1843)]
  * Add health check for synced services based on the status of the Kubernetes readiness probe on synced pod. [[GH-1821](https://github.com/hashicorp/consul-k8s/pull/1821)]

BUG FIXES:
* Control Plane
   * Don't incorrectly diff intention config entries when upgrading from Consul pre-1.12 to 1.12+ [[GH-1804](https://github.com/hashicorp/consul-k8s/pull/1804)]

## 1.0.2 (December 1, 2022)

IMPROVEMENTS:
* Helm:
  * CNI: Add `connectInject.cni.namespace` stanza which allows the CNI plugin resources to be deployed in a namespace other than the namespace that Consul is installed. [[GH-1756](https://github.com/hashicorp/consul-k8s/pull/1756)]

BUG FIXES:
* Helm:
  * Use the correct autogenerated cert for the API Gateway Controller when connecting to servers versus clients. [[GH-1753](https://github.com/hashicorp/consul-k8s/pull/1753)]
  * Don't mount the CA cert when `externalServers.useSystemRoots` is `true`. [[GH-1753](https://github.com/hashicorp/consul-k8s/pull/1753)]

## 0.49.2 (December 1, 2022)

IMPROVEMENTS:
* Control Plane
   * Bump Dockerfile base image for RedHat UBI `consul-k8s-control-plane` image to `ubi-minimal:9.1`. [[GH-1725](https://github.com/hashicorp/consul-k8s/pull/1725)]
* Helm
  * Add fields `localConnectTimeoutMs` and `localRequestTimeoutMs` to the `ServiceDefaults` CRD. [[GH-1647](https://github.com/hashicorp/consul-k8s/pull/1647)]

BUG FIXES:
* Helm:
  * Disable PodSecurityPolicies templating for `gossip-encryption-autogenerate` and `partition-init` when `global.enablePodSecurityPolicies` is `false`. [[GH-1693](https://github.com/hashicorp/consul-k8s/pull/1693)]

## 1.0.1 (November 21, 2022)

BUG FIXES:
* Control Plane
  * Add discover binary to control-plane image [[GH-1749](https://github.com/hashicorp/consul-k8s/pull/1749)]
* Helm:
  * Don't pass in a CA file to the API Gateway controller when `externalServers.useSystemRoots` is `true`. [[GH-1743](https://github.com/hashicorp/consul-k8s/pull/1743)]

## 1.0.0 (November 17, 2022)

BREAKING CHANGES:
* Admin Partitions **(Consul Enterprise only)**: Remove the partition service. When configuring Admin Partitions, the expose-servers service should be used instead.
* Consul Dataplane:
  * Consul client agents are no longer deployed by default, and Consul service mesh no longer uses Consul clients to operate. This change affects several main areas listed below. [[GH-1552](https://github.com/hashicorp/consul-k8s/pull/1552)]
  * A new component `consul-dataplane` is now injected as a sidecar-proxy instead of plain Envoy. `consul-dataplane` manages the Envoy proxy process and proxies xDS requests from Envoy to Consul servers.
  * All services on the service mesh are now registered directly with the central catalog in Consul servers.
  * All service-mesh consul-k8s components are configured to talk directly to Consul servers.
  * Mesh, ingress, and terminating gateways are now registered centrally by the endpoints controller, similar to how service-mesh services are registered.
* CLI:
  * Change default behavior of `consul-k8s install` to perform the installation when no answer is provided to the prompt. [[GH-1673](https://github.com/hashicorp/consul-k8s/pull/1673)]
* Helm:
  * Kubernetes-1.25 is now supported with the caveat that `global.enablePodSecurityPolicies` is not supported since PodSecurityPolicies have been removed in favor of PodSecurityStandards in Kubernetes-1.25. Full support for PodSecurityStandards will be added in a follow-on commit. [[GH-1726](https://github.com/hashicorp/consul-k8s/pull/1726)]
  * Support simplified default deployment values to allow for easier quick starts and testing:
    * Set `connectInject.replicas` to 1 [[GH-1702](https://github.com/hashicorp/consul-k8s/pull/1702)]
    * Set `meshGateway.affinity` to null and `meshGateway.replicas` to 1 [[GH-1702](https://github.com/hashicorp/consul-k8s/pull/1702)]
    * Set `ingressGateways.defaults.affinity` to null and `ingressGateways.defaults.replicas` to 1 [[GH-1702](https://github.com/hashicorp/consul-k8s/pull/1702)]
    * Set `terminatingGateways.defaults.affinity` to null and `terminatingGateways.defaults.replicas` to 1 [[GH-1702](https://github.com/hashicorp/consul-k8s/pull/1702)]
    * Set `server.replicas` to `1`. Formerly, this defaulted to `3`. [[GH-1551](https://github.com/hashicorp/consul-k8s/pull/1551)]
  * `client.enabled` now defaults to `false`. Setting it to `true` will deploy client agents, however, none of the consul-k8s components will use clients for their operation.
  * `global.imageEnvoy` is no longer used for sidecar proxies, as well as mesh, terminating, and ingress gateways.
  * `externalServers.grpcPort` default is now `8502` instead of `8503`.
  * `externalServers.hosts` no longer supports [cloud auto-join](https://developer.hashicorp.com/consul/docs/install/cloud-auto-join) strings directly. Instead, include an [`exec=`](https://github.com/hashicorp/go-netaddrs#command-line-tool-usage) string in the `externalServers.hosts` list to invoke the `discover` CLI. For example, the following string invokes the `discover` CLI with a cloud auto-join string: `exec=discover -q addrs provider=aws region=us-west-2 tag_key=consul-server tag_value=true`. The `discover` CLI is included in the official `hashicorp/consul-dataplane` images by default.
  * `meshGateway.service.enabled` value is removed. Mesh gateways now will always have a Kubernetes service as this is required to register them as a service with Consul.
  * `meshGateway.initCopyConsulContainer`, `ingressGateways.initCopyConsulContainer`, `terminatingGateways.initCopyConsulContainer` values are removed.
  * `connectInject.enabled` now defaults to `true`. [[GH-1551](https://github.com/hashicorp/consul-k8s/pull/1551)]
  * `syncCatalog.consulNamespaces.mirroringK8S` now defaults to `true`. [[GH-1601](https://github.com/hashicorp/consul-k8s/pull/1601)]
  * `connectInject.consulNamespaces.mirroringK8S` now defaults to `true`. [[GH-1601](https://github.com/hashicorp/consul-k8s/pull/1601)]
  * Remove `controller` section from the values file as the controller has now been merged into the connect-inject deployment. [[GH-1697](https://github.com/hashicorp/consul-k8s/pull/1697)]
  * Remove `global.consulSidecarContainer` from values file as there is no longer a consul sidecar. [[GH-1635](https://github.com/hashicorp/consul-k8s/pull/1635)]
  * Consul snapshot-agent now runs as a sidecar with Consul servers. [[GH-1620](https://github.com/hashicorp/consul-k8s/pull/1620)]

    This results in the following changes to Helm values:
      * Move `client.snapshotAgent` values to `server.snapshotAgent`, with the exception of the following values:
        * `client.snaphostAgent.replicas`
        * `client.snaphostAgent.serviceAccount`
      * Remove `global.secretsBackend.vault.consulSnapshotAgentRole` value. You should now use the `global.secretsBackend.vault.consulServerRole` for access to any Vault secrets.
  * Change `dns.enabled` and `dns.enableRedirection` to default to the value of `connectInject.transparentProxy.defaultEnabled`.
    Previously, `dns.enabled` defaulted to the value of `global.enabled` and `dns.enableRedirection` defaulted to the
    value to `false`. [[GH-1688](https://github.com/hashicorp/consul-k8s/pull/1688)]
  * Remove `global.imageEnvoy` and replace with `global.imageConsulDataplane` for running the sidecar proxy.
  * Add `apiGateway.imageEnvoy` as for configuring the version of Envoy that the API Gateway uses. [[GH-1698](https://github.com/hashicorp/consul-k8s/pull/1698)]
* Peering:
  * Rename `PeerName` to `Peer` in ExportedServices CRD. [[GH-1596](https://github.com/hashicorp/consul-k8s/pull/1596)]
  * Remove support for customizing the server addresses in peering token generation. Instead, mesh gateways should be used
    to establish peering connections if the server pods are not directly reachable. [[GH-1610](https://github.com/hashicorp/consul-k8s/pull/1610)]
  * Require `global.tls.enabled` when peering is enabled. [[GH-1610](https://github.com/hashicorp/consul-k8s/pull/1610)]
  * Require `meshGateway.enabled` when peering is enabled. [[GH-1683](https://github.com/hashicorp/consul-k8s/pull/1683)]

FEATURES:
* CLI:
  * Add the ability to install HCP self-managed clusters.  [[GH-1540](https://github.com/hashicorp/consul-k8s/pull/1540)]
  * Add the ability to install the HashiCups demo application via the -demo flag. [[GH-1540](https://github.com/hashicorp/consul-k8s/pull/1540)]
* Consul Dataplane:
  * Support merged metrics with consul-dataplane. [[GH-1635](https://github.com/hashicorp/consul-k8s/pull/1635)]
  * Support transparent proxying when using consul-dataplane. [[GH-1625](https://github.com/hashicorp/consul-k8s/pull/1478),[GH-1632](https://github.com/hashicorp/consul-k8s/pull/1632)]
  * Enable sync-catalog to only talk to Consul servers. [[GH-1659](https://github.com/hashicorp/consul-k8s/pull/1659)]
* Ingress Gateway
  * Add support for MaxConnections, MaxConcurrentRequests, and MaxPendingRequests to Ingress Gateway CRD. [[GH-1691](https://github.com/hashicorp/consul-k8s/pull/1691)]
* Peering:
  * Support peering over mesh gateways.
    * Add support for `PeerThroughMeshGateways` in Mesh CRD. [[GH-1478](https://github.com/hashicorp/consul-k8s/pull/1478)]

IMPROVEMENTS:
* CLI
  * `consul-k8s status` command will only show status of servers if they are expected to be present in the Kubernetes cluster. [[GH-1603](https://github.com/hashicorp/consul-k8s/pull/1603)]
  * Update demo charts and CLI command to not presume tproxy when using HCP preset. Also, use the most recent version of hashicups. [[GH-1657](https://github.com/hashicorp/consul-k8s/pull/1657)]
  * Update minimum go version for project to 1.19 [[GH-1633](https://github.com/hashicorp/consul-k8s/pull/1633)]
  * Enable `consul-k8s uninstall` to delete custom resources when uninstalling Consul. This is done by default. [[GH-1623](https://github.com/hashicorp/consul-k8s/pull/1623)] 
* Control Plane
  * Update minimum go version for project to 1.19 [[GH-1633](https://github.com/hashicorp/consul-k8s/pull/1633)]
  * Remove unneeded `agent:read` ACL permissions from mesh gateway policy. [[GH-1255](https://github.com/hashicorp/consul-k8s/pull/1255)]
  * Support updating health checks on consul clients during an upgrade to agentless. [[GH-1690](https://github.com/hashicorp/consul-k8s/pull/1690)]
  * Remove unused curl from docker images [[1624](https://github.com/hashicorp/consul-k8s/pull/1624)]
  * Bump Dockerfile base image for RedHat UBI `consul-k8s-control-plane` image to `ubi-minimal:9.1`. [[GH-1725][https://github.com/hashicorp/consul-k8s/pull/1725]]
* Helm:
  * Remove deprecated annotation `service.alpha.kubernetes.io/tolerate-unready-endpoints: "true"` in the `server-service` template. [[GH-1619](https://github.com/hashicorp/consul-k8s/pull/1619)]
  * Support `minAvailable` on connect injector `PodDisruptionBudget`. [[GH-1557](https://github.com/hashicorp/consul-k8s/pull/1557)]
  * Add `tolerations` and `nodeSelector` to Server ACL init jobs and `nodeSelector` to Webhook cert manager. [[GH-1581](https://github.com/hashicorp/consul-k8s/pull/1581)]
  * API Gateway: Add `tolerations` to `apiGateway.managedGatewayClass` and `apiGateway.controller` [[GH-1650](https://github.com/hashicorp/consul-k8s/pull/1650)]
  * API Gateway: Create PodSecurityPolicy for controller when `global.enablePodSecurityPolicies=true`. [[GH-1656](https://github.com/hashicorp/consul-k8s/pull/1656)]
  * API Gateway: Create PodSecurityPolicy and allow controller to bind it to ServiceAccounts that it creates for Gateway Deployments when `global.enablePodSecurityPolicies=true`. [[GH-1672](https://github.com/hashicorp/consul-k8s/pull/1672)]
  * Deploy `expose-servers` service only when Admin Partitions(ENT) is enabled. [[GH-1683](https://github.com/hashicorp/consul-k8s/pull/1683)]
  * Use a distroless image for `consul-dataplane`. [[GH-1676](https://github.com/hashicorp/consul-k8s/pull/1676)]
  * The Envoy version is now 1.24.0 for `consul-dataplane`. [[GH-1676](https://github.com/hashicorp/consul-k8s/pull/1676)]
  * Allow addition of extra labels to Connect Inject pods. [[GH-1678](https://github.com/hashicorp/consul-k8s/pull/1678)]
  * Add fields `localConnectTimeoutMs` and `localRequestTimeoutMs` to the `ServiceDefaults` CRD. [[GH-1647](https://github.com/hashicorp/consul-k8s/pull/1647)]
  * API Gateway: Enable API Gateways to directly connect to Consul servers when running in the agentless configuration. [[GH-1694](https://github.com/hashicorp/consul-k8s/pull/1694)]
  * Add `connectInject.consulNode.meta` to allow users to provide custom metadata to append to the NodeMeta [[GH-1707](https://github.com/hashicorp/consul-k8s/pull/1707)]
  * Add `externalServers.skipServerWatch` which prevents consul-dataplane from consuming the server update stream. This is useful for situations where Consul servers are behind a load balancer. [[GH-1686](https://github.com/hashicorp/consul-k8s/pull/1686)]
  * API Gateway: Allow controller to read MeshServices for use as a route backend. [[GH-1574](https://github.com/hashicorp/consul-k8s/pull/1574)]
  * API Gateway: Add support for using dynamic server discovery strings when running without agents. [[GH-1732](https://github.com/hashicorp/consul-k8s/pull/1732)]

BUG FIXES:
* CLI
  * Allow optional environment variables for use in the cloud preset to the CLI for cluster bootstrapping. [[GH-1608](https://github.com/hashicorp/consul-k8s/pull/1608)]
  * Configure `-tls-server-name` when `global.cloud.enabled=true` so that it matches the server certificate created via HCP [[GH-1591](https://github.com/hashicorp/consul-k8s/pull/1591)]
  * Do not query clients in the status command since clients no longer exist. [[GH-1573](https://github.com/hashicorp/consul-k8s/pull/1573)]
* Peering
  * Add `peering:read` permissions to mesh gateway token to fix peering connections through the mesh gateways. [[GH-1685](https://github.com/hashicorp/consul-k8s/pull/1685)]
* Helm:
  * Disable PodSecurityPolicies in all templates when `global.enablePodSecurityPolicies` is `false`. [[GH-1693](https://github.com/hashicorp/consul-k8s/pull/1693)]

## 0.49.1 (November 14, 2022)
BREAKING CHANGES:
* Peering:
  * Rename `PeerName` to `Peer` in ExportedServices CRD. [[GH-1596](https://github.com/hashicorp/consul-k8s/pull/1596)]

FEATURES:
* Ingress Gateway
  * Add support for MaxConnections, MaxConcurrentRequests, and MaxPendingRequests to Ingress Gateway CRD. [[GH-1691](https://github.com/hashicorp/consul-k8s/pull/1691)]

IMPROVEMENTS:
* Helm:
  * Add `tolerations` and `nodeSelector` to Server ACL init jobs and `nodeSelector` to Webhook cert manager. [[GH-1581](https://github.com/hashicorp/consul-k8s/pull/1581)]
  * API Gateway: Allow controller to read MeshServices for use as a route backend. [[GH-1574](https://github.com/hashicorp/consul-k8s/pull/1574)]
  * API Gateway: Add `tolerations` to `apiGateway.managedGatewayClass` and `apiGateway.controller` [[GH-1650](https://github.com/hashicorp/consul-k8s/pull/1650)]
  * API Gateway: Create PodSecurityPolicy for controller when `global.enablePodSecurityPolicies=true`. [[GH-1656](https://github.com/hashicorp/consul-k8s/pull/1656)]
  * API Gateway: Create PodSecurityPolicy and allow controller to bind it to ServiceAccounts that it creates for Gateway Deployments when `global.enablePodSecurityPolicies=true`. [[GH-1672](https://github.com/hashicorp/consul-k8s/pull/1672)]

## 0.49.0 (September 29, 2022)

FEATURES:
* CLI:
  * Add support for tab autocompletion [[GH-1437](https://github.com/hashicorp/consul-k8s/pull/1501)]
* Consul CNI Plugin
  * Support for OpenShift and Multus CNI plugin [[GH-1527](https://github.com/hashicorp/consul-k8s/pull/1527)]

BUG FIXES:
* Control plane
  * Use global ACL auth method to provision ACL tokens for API Gateway in secondary datacenter [[GH-1481](https://github.com/hashicorp/consul-k8s/pull/1481)]
  * Peering: pass new `use_auto_cert` value to gRPC TLS config when auto-encrypt is enabled. [[GH-1541](https://github.com/hashicorp/consul-k8s/pull/1541)]
* Helm:
  * Only create Federation Secret Job when server.updatePartition is 0 [[GH-1512](https://github.com/hashicorp/consul-k8s/pull/1512)]
  * Fixes a typo in the templating of `global.connectInject.disruptionBudget.maxUnavailable`. [[GH-1530](https://github.com/hashicorp/consul-k8s/pull/1530)]

IMPROVEMENTS:
* Helm:
  * API Gateway: Set primary datacenter flag when deploying controller into secondary datacenter with federation enabled [[GH-1511](https://github.com/hashicorp/consul-k8s/pull/1511)]
  * API Gateway: Allow controller to create and update Secrets for storing Consul CA cert alongside gateway Deployments [[GH-1542](https://github.com/hashicorp/consul-k8s/pull/1542)]
  * New parameter `EnforcingConsecutive5xx` which supports a configurable percent chance of automatic ejection of a host when a consecutive number of 5xx response codes are received [[GH-1484](https://github.com/hashicorp/consul-k8s/pull/1484)]
* Control-plane:
  * Support escaped commas in service tag annotations for pods which use `consul.hashicorp.com/connect-service-tags` or `consul.hashicorp.com/service-tags`. [[GH-1532](https://github.com/hashicorp/consul-k8s/pull/1532)]

## 0.48.0 (September 01, 2022)

FEATURES:
* MaxInboundConnections in service-defaults CRD
  * Add support for MaxInboundConnections on the Service Defaults CRD. [[GH-1437](https://github.com/hashicorp/consul-k8s/pull/1437)]
* Consul CNI Plugin
  * CNI Plugin for Consul-k8s [[GH-1465](https://github.com/hashicorp/consul-k8s/pull/1456)]
* Kubernetes 1.24 Support
  * Add support for Kubernetes 1.24 where ServiceAccounts no longer have long-term JWT tokens. [[GH-1431](https://github.com/hashicorp/consul-k8s/pull/1431)]
  * Upgrade kubeVersion in helm chart to support Kubernetes 1.21+.
* Cluster Peering:
  * Add support for setting failover `Targets` on the Service Resolver CRD.  [[GH-1284](https://github.com/hashicorp/consul-k8s/pull/1284)]
  * Add support for redirecting to cluster peers on the Service Resolver CRD.  [[GH-1284](https://github.com/hashicorp/consul-k8s/pull/1284)]

BREAKING CHANGES:
* Kubernetes 1.24 Support
  * Users deploying multiple services to the same Pod (multiport) on Kubernetes 1.24 must also deploy a Kubernetes Secret for each ServiceAccount associated with the Consul service. The name of the Secret must match the ServiceAccount name and be of type `kubernetes.io/service-account-token` [[GH-1431](https://github.com/hashicorp/consul-k8s/pull/1431)]
  * Kubernetes 1.19 and 1.20 are no longer supported.

  Example:

  ```yaml
  apiVersion: v1
  kind: Secret
  metadata:
    name: svc1
    annotations:
      kubernetes.io/service-account.name: svc1
  type: kubernetes.io/service-account-token
  ---
  apiVersion: v1
  kind: Secret
  metadata:
    name: svc2
    annotations:
      kubernetes.io/service-account.name: svc2
  type: kubernetes.io/service-account-token
  ```

* Control Plane
  * Rename flag `server-address` to `token-server-address` in the `inject-connect` subcommand to avoid overloading the context of the `server-address` flag. [[GH-1426](https://github.com/hashicorp/consul-k8s/pull/1426)]

IMPROVEMENTS:
* CLI:
  * Display clusters by their short names rather than FQDNs for the `proxy read` command. [[GH-1412](https://github.com/hashicorp/consul-k8s/pull/1412)]
  * Display a message when `proxy list` returns no results. [[GH-1412](https://github.com/hashicorp/consul-k8s/pull/1412)]
  * Display a warning when a user passes a field and table filter combination to `proxy read` where the given field is not present in any of the output tables. [[GH-1412](https://github.com/hashicorp/consul-k8s/pull/1412)]
  * Extend the timeout for `consul-k8s proxy read` to establish a connection from 5s to 10s. [[GH-1442](https://github.com/hashicorp/consul-k8s/pull/1442)]
  * Expand the set of Envoy Listener Filters that may be parsed and output to the Listeners table. [[GH-1442](https://github.com/hashicorp/consul-k8s/pull/1442)]
* Helm:
  * The default Envoy proxy image is now `envoyproxy/envoy:v1.23.1`. [[GH-1473](https://github.com/hashicorp/consul-k8s/pull/1473)]

BUG FIXES:
* Helm
  * API Gateway: Configure ACL auth for controller correctly when deployed in secondary datacenter with federation enabled [[GH-1462](https://github.com/hashicorp/consul-k8s/pull/1462)]
* CLI
  * Fix issue where SNI filters for Terminating Gateways showed up as blank lines. [[GH-1442](https://github.com/hashicorp/consul-k8s/pull/1442)]
  * Fix issue where Logical DNS endpoints were being displayed alongside cluster names. [[GH-1452](https://github.com/hashicorp/consul-k8s/pull/1452)]

## 0.47.1 (August 12, 2022)

BUG FIXES:
* Helm
  * Update the version of the `imageK8S` in `values.yaml` to the latest control-plane image. [[GH-1355](https://github.com/hashicorp/consul-k8s/pull/1352)]

## 0.47.0 (August 12, 2022)

FEATURES:
* Transparent Proxy Egress
  * Add support for Destinations on the Service Defaults CRD. [[GH-1352](https://github.com/hashicorp/consul-k8s/pull/1352)]
* CLI:
  * Add `consul-k8s proxy list` command for displaying Pods running Envoy managed by Consul. [[GH-1271](https://github.com/hashicorp/consul-k8s/pull/1271)]
  * Add `consul-k8s proxy read podname` command for displaying Envoy configuration for a given Pod. [[GH-1271](https://github.com/hashicorp/consul-k8s/pull/1271)]
* [Experimental] Cluster Peering:
  * Add support for ACLs and TLS. [[GH-1343](https://github.com/hashicorp/consul-k8s/pull/1343)] [[GH-1366](https://github.com/hashicorp/consul-k8s/pull/1366)]
  * Add support for Load Balancers or external addresses in front of Consul servers for peering stream.
    * Support new expose-servers Kubernetes Service deployed by Helm chart to expose the Consul servers, and using the service address in the peering token. [[GH-1378](https://github.com/hashicorp/consul-k8s/pull/1378)]
    * Support non-default partitions by using `externalServers.hosts` as the server addresses in the peering token. [[GH-1384](https://github.com/hashicorp/consul-k8s/pull/1384)]
    * Support arbitrary addresses as the server addresses in the peering token via `global.peering.tokenGeneration.source="static"` and `global.peering.tokenGeneration.static=["sample-server-address:8502"]`. [[GH-1392](https://github.com/hashicorp/consul-k8s/pull/1392)]
  * Generate new peering token only on user-triggered events. [[GH-1399](https://github.com/hashicorp/consul-k8s/pull/1399)]

IMPROVEMENTS:
* Helm
  * Bump default Envoy version to 1.22.4. [[GH-1413](https://github.com/hashicorp/consul-k8s/pull/1413)]
  * Added support for Consul API Gateway to read ReferenceGrant custom resources. This will require either installing Consul API Gateway CRDs from the upcoming v0.4.0 release with `kubectl apply --kustomize "github.com/hashicorp/consul-api-gateway/config/crd?ref=v0.4.0"` or manually installing the ReferenceGrant CRD from the Gateway API v0.5 [Experimental Channel](https://gateway-api.sigs.k8s.io/concepts/versioning/#release-channels-eg-experimental-standard) when setting `apiGateway.enabled=true` [[GH-1299](https://github.com/hashicorp/consul-k8s/pull/1299)]

BUG FIXES:
* Helm
  * Fix permissions in client-daemonset and server-statefulset when using extra-config volumes to prevent errors on OpenShift. [[GH-1307](https://github.com/hashicorp/consul-k8s/pull/1307)]

## 0.46.1 (July 26, 2022)

IMPROVEMENTS:
* Control Plane
  * Update alpine to 3.16 in the Docker image. [[GH-1372](https://github.com/hashicorp/consul-k8s/pull/1372)]

## 0.46.0 (July 20, 2022)

FEATURES:
* [Experimental] Cluster Peering:
  * Add support for secret watchers on the Peering Acceptor and Peering Dialer controllers. [[GH-1284](https://github.com/hashicorp/consul-k8s/pull/1284)]
  * Add support for version annotation on the Peering Acceptor and Peering Dialer controllers. [[GH-1302](https://github.com/hashicorp/consul-k8s/pull/1302)]
  * Add validation webhooks for the Peering Acceptor and Peering Dialer CRDs. [[GH-1310](https://github.com/hashicorp/consul-k8s/pull/1310)]
  * Add Conditions to the status of the Peering Acceptor and Peering Dialer CRDs. [[GH-1335](https://github.com/hashicorp/consul-k8s/pull/1335)]

IMPROVEMENTS:
* Control Plane
  * Added annotations `consul.hashicorp.com/prometheus-ca-file`, `consul.hashicorp.com/prometheus-ca-path`, `consul.hashicorp.com/prometheus-cert-file`, and `consul.hashicorp.com/prometheus-key-file` for configuring TLS scraping on Prometheus metrics endpoints for Envoy sidecars. To enable, set the cert and key file annotations along with one of the ca file/path annotations. [[GH-1303](https://github.com/hashicorp/consul-k8s/pull/1303)]
  * Added annotations `consul.hashicorp.com/consul-sidecar-user-volume` and `consul.hashicorp.com/consul-sidecar-user-volume-mount` for attaching Volumes and VolumeMounts to the Envoy sidecar. Both should be JSON objects. [[GH-1315](https://github.com/hashicorp/consul-k8s/pull/1315)]
  * Update minimum go version for project to 1.18 [[GH-1292](https://github.com/hashicorp/consul-k8s/pull/1292)]
* Helm
  * Added `connectInject.annotations` and `syncCatalog.annotations` values for setting annotations on connect inject and sync catalog deployments. [[GH-775](https://github.com/hashicorp/consul-k8s/pull/775)]
  * Added PodDisruptionBudget to the connect injector deployment which can be configured using the `connectInject.disruptionBudget` stanza. [[GH-1316](https://github.com/hashicorp/consul-k8s/pull/1316)]
* CLI
  * Update minimum go version for project to 1.18 [[GH-1292](https://github.com/hashicorp/consul-k8s/pull/1292)]

BUG FIXES:
* Helm
  * When using Openshift do not set securityContext in gossip-encryption-autogenerate job. [[GH-1308](https://github.com/hashicorp/consul-k8s/pull/1308)]
* Control Plane
  * Fix missing RBAC permissions for the peering controllers to be able to update secrets. [[GH-1359](https://github.com/hashicorp/consul-k8s/pull/1359)]
  * Fix a bug in the peering controller where we tried to read the secret from the cache right after creating it. [[GH-1359](https://github.com/hashicorp/consul-k8s/pull/1359)]

## 0.45.0 (June 17, 2022)
FEATURES:
* [Experimental] Cluster Peering: Support Consul cluster peering, which allows service connectivity between two independent clusters.
  [[GH-1273](https://github.com/hashicorp/consul-k8s/pull/1273)]

  Enabling peering will deploy the peering controllers and PeeringAcceptor and PeeringDialer CRDs. The new CRDs are used
  to establish a peering connection between two clusters.

  See the [Cluster Peering on Kubernetes](https://www.consul.io/docs/connect/cluster-peering/k8s)
  for full instructions.

  Requirements:
  * Consul 1.13+
  * `global.peering.enabled=true` and `connectInject.enabled=true` must be set to enable peering.
  * Mesh gateways are required for service to service communication across peers, i.e `meshGateway.enabled=true`.

IMPROVEMENTS:
* Helm
  * Enable the configuring of snapshot intervals in the client snapshot agent via `client.snapshotAgent.interval`. [[GH-1235](https://github.com/hashicorp/consul-k8s/pull/1235)]
  * Enable configuring the pod topologySpreadConstraints for mesh, terminating, and ingress gateways. [[GH-1257](https://github.com/hashicorp/consul-k8s/pull/1257)]
  * Present Consul server CA chain when using Vault secrets backend. [[GH-1251](https://github.com/hashicorp/consul-k8s/pull/1251)]
  * API Gateway: Enable configuring of the new High Availability feature (requires Consul API Gateway v0.3.0+). [[GH-1261](https://github.com/hashicorp/consul-k8s/pull/1261)]
  * Enable the configuration of Envoy proxy concurrency via `connectInject.sidecarProxy.concurrency` which can
    be overridden at the pod level via the annotation `consul.hashicorp.com/consul-envoy-proxy-concurrency`.
    This PR also sets the default concurrency for envoy proxies to `2`. [[GH-1277](https://github.com/hashicorp/consul-k8s/pull/1277)]
  * Update Mesh CRD with Mesh HTTP Config. [[GH-1282](https://github.com/hashicorp/consul-k8s/pull/1282)]
* Control Plane
  * Bump Dockerfile base image for RedHat UBI `consul-k8s-control-plane` image to `ubi-minimal:8.6`. [[GH-1244](https://github.com/hashicorp/consul-k8s/pull/1244)]
  * Add additional metadata to service instances registered via catalog sync. [[GH-447](https://github.com/hashicorp/consul-k8s/pull/447)]
  * Enable configuring Connect Injector and Controller Webhooks' certificates to be managed by Vault. [[GH-1191](https://github.com/hashicorp/consul-k8s/pull/1191/)]

BUG FIXES:
* Helm
  * Update client-snapshot-agent so that setting `client.snapshotAgent.caCert` no longer requires root access to modify the trust store. [[GH-1190](https://github.com/hashicorp/consul-k8s/pull/1190/)]
  * Add missing vault agent annotations to the `api-gateway-controller-deployment`. [[GH-1247](https://github.com/hashicorp/consul-k8s/pull/1247)]
  * Bump default Envoy version to 1.22.2. [[GH-1276](https://github.com/hashicorp/consul-k8s/pull/1276)]

## 0.44.0 (May 17, 2022)

BREAKING CHANGES:
* Helm
  * Using the Vault integration requires Consul 1.12.0+. [[GH-1213](https://github.com/hashicorp/consul-k8s/pull/1213)], [[GH-1218](https://github.com/hashicorp/consul-k8s/pull/1218)]
  * The default Envoy proxy image is now `envoyproxy/envoy:v1.22.0` which is no longer alpine based. The default trust store location is no longer `/etc/ssl/cert.pem`, please use `/etc/ssl/certs/ca-certificates.crt` when configuring Terminating Gateway configuration entries for non-alpine based Envoy images. See [[docs](https://www.consul.io/docs/k8s/connect/terminating-gateways#create-the-configuration-entry-for-the-terminating-gateway)].

IMPROVEMENTS:
* Helm
  * Enable the ability to `configure global.consulAPITimeout` to configure how long requests to the Consul API will wait to resolve before canceling.  The default value is 5 seconds. [[GH-1178](https://github.com/hashicorp/consul-k8s/pull/1178)]

BUG FIXES:
* Security 
  * Bump golang.org/x/crypto and golang.org/x/text dependencies to address CVE-2022-27291 and CVE-2021-38561 respectively on both CLI and Control Plane. There's no known exposure within Consul on Kubernetes as the dependencies are not invoked. [[GH-1189](https://github.com/hashicorp/consul-k8s/pull/1189)]
* Control Plane
  * Endpoints Controller queuing up service registrations/deregistrations when request to agent on a terminated pod does not time out. This could result in pods not being registered and service instances not being deregistered. [[GH-1178](https://github.com/hashicorp/consul-k8s/pull/1178)]
* Helm
  * Update client-daemonset to include ca-cert volumeMount only when tls is enabled. [[GH-1194](https://github.com/hashicorp/consul-k8s/pull/1194)]
  * Update create-federation-secret-job to look up the automatically generated gossip encryption key by the right name when global.name is unset or set to something other than consul. [[GH-1196](https://github.com/hashicorp/consul-k8s/pull/1196)]
  * Add Admin Partitions support to Sync Catalog **(Consul Enterprise only)**. [[GH-1180](https://github.com/hashicorp/consul-k8s/pull/1180)]
  * Correct webhook-cert-manager-clusterrole to utilize the web-cert-manager podsecuritypolicy rather than connect-injectors when `global.enablePodSecurityPolicies` is true. [[GH-1202](https://github.com/hashicorp/consul-k8s/pull/1202)]
  * Enable Consul auto-reload-config only when Vault is enabled. [[GH-1213](https://github.com/hashicorp/consul-k8s/pull/1213)]
  * Revert TLS config to be compatible with Consul 1.11. [[GH-1218](https://github.com/hashicorp/consul-k8s/pull/1218)]

## 0.43.0 (April 21, 2022)

BREAKING CHANGES:
* Helm
  * Requires Consul 1.12.0+ as the Server statefulsets are now provisioned with Consul `-auto-reload-config` flag which monitors changes to specific Consul configuration properties and reloads itself when changes are detected. [[GH-1135](https://github.com/hashicorp/consul-k8s/pull/1135)]
  * API Gateway: Re-use connectInject.consulNamespaces instead of requiring that apiGateway.consulNamespaces have the same value when ACLs are enabled. [[GH-1169](https://github.com/hashicorp/consul-k8s/pull/1169)]

FEATURES:
* Control Plane
    * Add a `"consul.hashicorp.com/kubernetes-service"` annotation for pods to specify which Kubernetes service they want to use for registration when multiple services target the same pod. [[GH-1150](https://github.com/hashicorp/consul-k8s/pull/1150)]

BUG FIXES:
* CLI
  * Fix issue where clusters not in the same namespace as their deployment name could not be upgraded. [[GH-1115](https://github.com/hashicorp/consul-k8s/pull/1115)]
  * Fix issue where the CLI was looking for secrets in namespaces other than the namespace targeted by the release. [[GH-1156](https://github.com/hashicorp/consul-k8s/pull/1156)]
  * Fix issue where the federation secret was not being found in certain configurations. [[GH-1154](https://github.com/hashicorp/consul-k8s/pull/1154)]
* Control Plane
  * Fix issue where upgrading a deployment from non-service mesh to service mesh would cause Pods to hang in init. [[GH-1136](https://github.com/hashicorp/consul-k8s/pull/1136)]
* Helm
  * Respect client nodeSelector, tolerations, and priorityClass when scheduling `create-federation-secret` Job. [[GH-1108](https://github.com/hashicorp/consul-k8s/issues/1108)]

IMPROVEMENTS:
* Control Plane
  * Support new annotation for mounting connect-inject volume to other containers. [[GH-1111](https://github.com/hashicorp/consul-k8s/pull/1111)]
* Helm
  * API Gateway: Allow controller to read ReferencePolicy in order to determine if route is allowed for backend in different namespace. [[GH-1148](https://github.com/hashicorp/consul-k8s/pull/1148)]
  * Allow `consul` to be a destination namespace. [[GH-1163](https://github.com/hashicorp/consul-k8s/pull/1163)]
  * CRDs: Update Mesh and Ingress Gateway CRDs to support TLS config. [[GH-1168](https://github.com/hashicorp/consul-k8s/pull/1168)]

## 0.42.0 (April 04, 2022)

BREAKING CHANGES:
* Helm
  * Minimum Kubernetes version supported is 1.19 and now matches what is stated in the `README.md` file.  [[GH-1049](https://github.com/hashicorp/consul-k8s/pull/1049)] 
* ACLs
  * Support Terminating Gateway obtaining an ACL token using a k8s auth method. [[GH-1102](https://github.com/hashicorp/consul-k8s/pull/1102)]
    * **Note**: If you have updated a token with a new policy for a terminating gateway, this will not apply any more as ACL tokens will be ephemeral and are issued to the terminating gateways when the pod is created and destroyed when the pod is stopped. To achieve the same ACL permissions, you will need to assign the policy to the role for the terminating gateway, rather than the token.  
  * Support Mesh Gateway obtaining an ACL token using a k8s auth method. [[GH-1102](https://github.com/hashicorp/consul-k8s/pull/1102)]
    * **Note**: This is a breaking change if you are using a mesh gateway with mesh federation. To properly configure mesh federation with mesh gateways, you will need to configure the `global.federation.k8sAuthMethodHost` in secondary datacenters to point to the address of the Kubernetes API server of the secondary datacenter.  This address must be reachable from the Consul servers in the primary datacenter.
  * **General Note on old ACL Tokens**:  As of this release, ACL tokens no longer need to be stored as Kubernetes secrets. They will transparently be provisioned by the Kubernetes Auth Method when client and component pods are provisioned and will also be destroyed when client and component pods are destroyed.  Old ACL tokens, however, will still exist as Kubernetes secrets and in Consul and will need to be identified and manually deleted.

FEATURES:
* ACLs: Enable issuing ACL tokens via Consul login with a Kubernetes Auth Method and replace the need for storing ACL tokens as Kubernetes secrets.
  * Support CRD controller obtaining an ACL token via using a k8s auth method. [[GH-995](https://github.com/hashicorp/consul-k8s/pull/995)]
  * Support Connect Inject obtaining an ACL token via using a k8s auth method. [[GH-1076](https://github.com/hashicorp/consul-k8s/pull/1076)]
  * Support Sync Catalog obtaining an ACL token via using a k8s auth method. [[GH-1081](https://github.com/hashicorp/consul-k8s/pull/1081)], [[GHT-1077](https://github.com/hashicorp/consul-k8s/pull/1077)]
  * Support API Gateway controller obtaining an ACL token via using a k8s auth method. [[GH-1083](https://github.com/hashicorp/consul-k8s/pull/1083)]
  * Support Snapshot Agent obtaining an ACL token via using a k8s auth method. [[GH-1084](https://github.com/hashicorp/consul-k8s/pull/1084)]
  * Support Mesh Gateway obtaining an ACL token via using a k8s auth method. [[GH-1085](https://github.com/hashicorp/consul-k8s/pull/1085)]
  * Support Ingress Gateway obtaining an ACL token via using a k8s auth method. [[GH-1118](https://github.com/hashicorp/consul-k8s/pull/1118)]
  * Support Terminating Gateway obtaining an ACL token via using a k8s auth method. [[GH-1102](https://github.com/hashicorp/consul-k8s/pull/1102)]
  * Support Consul Client obtaining an ACL token via using a k8s auth method. [[GH-1093](https://github.com/hashicorp/consul-k8s/pull/1093)]
  * Support issuing global ACL tokens via k8s auth method. [[GH-1075](https://github.com/hashicorp/consul-k8s/pull/1075)]
  
  
IMPROVEMENTS:
* Control Plane
  * Upgrade Docker image Alpine version from 3.14 to 3.15. [[GH-1058](https://github.com/hashicorp/consul-k8s/pull/1058)
* Helm
  * API Gateway: Allow controller to read Kubernetes namespaces in order to determine if route is allowed for gateway. [[GH-1092](https://github.com/hashicorp/consul-k8s/pull/1092)]
  * Support a pre-configured bootstrap ACL token. [[GH-1125](https://github.com/hashicorp/consul-k8s/pull/1125)]
* Vault
  * Enable snapshot agent configuration to be retrieved from vault. [[GH-1113](https://github.com/hashicorp/consul-k8s/pull/1113)]
* CLI
  * Enable users to set up secondary clusters with existing federation secrets. [[GH-1126](https://github.com/hashicorp/consul-k8s/pull/1126)] 

BUG FIXES:
* Helm
  * Don't set TTL for server certificates when using Vault as the secrets backend. [[GH-1104](https://github.com/hashicorp/consul-k8s/pull/1104)]
  * Fix PodSecurityPolicies for clients/mesh gateways when hostNetwork is used. [[GH-1090](https://github.com/hashicorp/consul-k8s/pull/1090)]
* CLI
  * Fix `install` and `upgrade` commands for Windows. [[GH-1139](https://github.com/hashicorp/consul-k8s/pull/1139)]

## 0.41.1 (February 24, 2022)

BUG FIXES:
* Helm
  * Support Envoy 1.20.2. [[GH-1051](https://github.com/hashicorp/consul-k8s/pull/1051)]

## 0.41.0 (February 23, 2022)

FEATURES:
* Support WAN federation via Mesh Gateways with Vault as the secrets backend. [[GH-1016](https://github.com/hashicorp/consul-k8s/pull/1016),[GH-1025](https://github.com/hashicorp/consul-k8s/pull/1025),[GH-1029](https://github.com/hashicorp/consul-k8s/pull/1029),[GH-1038](https://github.com/hashicorp/consul-k8s/pull/1038)]
  * **Note**: To use WAN federation with ACLs and Vault, you will need to create a KV secret in Vault that will serve as the replication token with
    a random UUID: `vault kv put secret/consul/replication key="$(uuidgen)"`. 
  * You will need to then provide this secret to both the primary
    and the secondary datacenters with `global.acls.replicationToken` values and allow the `global.secretsBackend.vault.manageSystemACLsRole` Vault role to read it.
    In the primary datacenter, the Helm chart will create the replication token in Consul using the UUID as the secret ID of the token.
* Connect: Support workaround for pods with multiple ports, by registering a Consul service and injecting an Envoy sidecar and init container per port. [[GH-1012](https://github.com/hashicorp/consul-k8s/pull/1012)]
  * Transparent proxying, metrics, and metrics merging are not supported for multi-port pods.
  * Multi-port pods should specify annotations in the format, such that the service names and port names correspond with each other in the specified order, i.e. `web` service is listening on `8080`, `web-admin` service is listening on `9090`.
    * `consul.hashicorp.com/connect-service': 'web,web-admin`
    * `consul.hashicorp.com/connect-service-port': '8080,9090`

IMPROVEMENTS:
* Helm
  * Vault: Allow passing arbitrary annotations to the vault agent. [[GH-1015](https://github.com/hashicorp/consul-k8s/pull/1015)]
  * Vault: Add support for customized IP and DNS SANs for server cert in Vault. [[GH-1020](https://github.com/hashicorp/consul-k8s/pull/1020)]
  * Vault: Add support for Enterprise License to be configured in Vault. [[GH-1032](https://github.com/hashicorp/consul-k8s/pull/1032)]
  * API Gateway: Allow Kubernetes namespace to Consul enterprise namespace mapping for deployed gateways and mesh services. [[GH-1024](https://github.com/hashicorp/consul-k8s/pull/1024)]

BUG FIXES:
* API Gateway
  * Fix issue where if the API gateway controller pods restarted, gateway pods would become disconnected from the secret discovery service. [[GH-1007](https://github.com/hashicorp/consul-k8s/pull/1007)]
  * Fix issue where the API gateway controller could not update existing Deployments or Services. [[GH-1014](https://github.com/hashicorp/consul-k8s/pull/1014)]
  * Fix issue where the API gateway controller lacked sufficient permissions to bind routes when ACLs were enabled. [[GH-1018](https://github.com/hashicorp/consul-k8s/pull/1018)]

BREAKING CHANGES:
* Helm
  * Rename fields of IngressGateway CRD to fix incorrect names (`gatewayTLSConfig` => `tls`, `gatewayServiceTLSConfig` => `tls`, `gatewayTLSSDSConfig` => `sds`). [[GH-1017](https://github.com/hashicorp/consul-k8s/pull/1017)]

## 0.40.0 (January 27, 2022)

BREAKING CHANGES:
* Helm
  * Some Consul components from the Helm chart have been renamed to ensure consistency in naming across the components.
  This will not be a breaking change if Consul components are not referred to by name externally. Check the PR for the list of renamed components. [[GH-993](https://github.com/hashicorp/consul-k8s/pull/993)][[GH-1000](https://github.com/hashicorp/consul-k8s/pull/1000)]

FEATURES:
* Helm
  * Support Envoy 1.20.1. [[GH-958](https://github.com/hashicorp/consul-k8s/pull/958)]
  * Support Consul 1.11.2. [[GH-976](https://github.com/hashicorp/consul-k8s/pull/976)]
  * Support [Consul API Gateway](https://github.com/hashicorp/consul-api-gateway) Controller deployment through the Helm chart and provision an ACL token to for API Gateway via server-acl-init [[GH-925](https://github.com/hashicorp/consul-k8s/pull/925)]

IMPROVEMENTS:
* Helm
  * Allow customization of `terminationGracePeriodSeconds` on the ingress gateways. [[GH-947](https://github.com/hashicorp/consul-k8s/pull/947)]
  * Support `ui.dashboardURLTemplates.service` value for setting [dashboard URL templates](https://www.consul.io/docs/agent/options#ui_config_dashboard_url_templates_service). [[GH-937](https://github.com/hashicorp/consul-k8s/pull/937)]
  * Allow using dash-separated names for config entries when using `kubectl`. [[GH-965](https://github.com/hashicorp/consul-k8s/pull/965)]
  * Support Pod Security Policies with Vault integration. [[GH-985](https://github.com/hashicorp/consul-k8s/pull/985)]
  * Rename Consul resources to remove resource kind suffixes from the resource names to standardize resource names across the Helm chart. [[GH-993](https://github.com/hashicorp/consul-k8s/pull/993)]
  * Append `-client` to the Consul Daemonset name to standardize resource names across the Helm chart. [[GH-1000](https://github.com/hashicorp/consul-k8s/pull/1000)]
* CLI
  * Show a diff when upgrading a Consul installation on Kubernetes [[GH-934](https://github.com/hashicorp/consul-k8s/pull/934)]
* Control Plane
  * Support the value `$POD_NAME` for the annotation `consul.hashicorp.com/service-meta-*` that will now be interpolated and set to the pod's name in the service's metadata. [[GH-982](https://github.com/hashicorp/consul-k8s/pull/982)]
  * Allow managing Consul sidecar resources via annotations. [[GH-956](https://github.com/hashicorp/consul-k8s/pull/956)]
  * Support using a backslash to escape commas in `consul.hashicorp.com/service-tags` annotation. [[GH-983](https://github.com/hashicorp/consul-k8s/pull/983)]
  * Avoid making unnecessary calls to Consul in the endpoints controller to improve application startup time when Consul is down. [[GH-779](https://github.com/hashicorp/consul-k8s/issues/779)]

BUG FIXES:
* Helm
  * Add `PodDisruptionBudget` Kind when checking for existing versions so that `helm template` can generate the right version. [[GH-923](https://github.com/hashicorp/consul-k8s/pull/923)]
* Control Plane
  * Admin Partitions **(Consul Enterprise only)**: Attach anonymous-policy to the anonymous token from non-default partitions to support DNS queries when the default partition is on a VM. [[GH-966](https://github.com/hashicorp/consul-k8s/pull/966)]

## 0.39.0 (December 15, 2021)

FEATURES:
* Helm
  * Support Consul 1.11.1. [[GH-935](https://github.com/hashicorp/consul-k8s/pull/935)]
  * Support Envoy 1.20.0. [[GH-935](https://github.com/hashicorp/consul-k8s/pull/935)]
  * Minimum Kubernetes versions supported is 1.18+. [[GH-935](https://github.com/hashicorp/consul-k8s/pull/935)]
* CLI
   * **BETA** Add `upgrade` command to modify Consul installation on Kubernetes. [[GH-898](https://github.com/hashicorp/consul-k8s/pull/898)]

IMPROVEMENTS:
* Control Plane
  * Bump `consul-k8s-control-plane` UBI images for OpenShift to use base image `ubi-minimal:8.5`. [[GH-922](https://github.com/hashicorp/consul-k8s/pull/922)]
  * Support the value `$POD_NAME` for the annotation `consul.hashicorp.com/service-tags` that will now be interpolated and set to the pod name. [[GH-931](https://github.com/hashicorp/consul-k8s/pull/931)]
 

## 0.38.0 (December 08, 2021)

BREAKING CHANGES:
* Control Plane
  * Update minimum go version for project to 1.17 [[GH-878](https://github.com/hashicorp/consul-k8s/pull/878)]
  * Add boolean metric to merged metrics response `consul_merged_service_metrics_success` to indicate if service metrics
    were scraped successfully. [[GH-551](https://github.com/hashicorp/consul-k8s/pull/551)]

FEATURES:
* Vault as a Secrets Backend: Add support for Vault as a secrets backend for Gossip Encryption, Server TLS certs and Service Mesh TLS certificates,
    removing the existing usage of Kubernetes Secrets for the respective secrets. [[GH-904](https://github.com/hashicorp/consul-k8s/pull/904/)]

  See the [Consul Kubernetes and Vault documentation](https://www.consul.io/docs/k8s/installation/vault)
  for full install instructions.
  
  Requirements: 
  * Consul 1.11+
  * Vault 1.9+ and Vault-K8s 0.14+ must be installed with the Vault Agent Injector enabled (`injector.enabled=true`)
    into the Kubernetes cluster that Consul is installed into.
  * `global.tls.enableAutoEncryption=true` is required for TLS support.
  * If TLS is enabled in Vault, `global.secretsBackend.vault.ca` must be provided and should reference a Kube secret
    which holds a copy of the Vault CA cert.
  * Add boolean metric to merged metrics response `consul_merged_service_metrics_success` to indicate if service metrics were
    scraped successfully. [[GH-551](https://github.com/hashicorp/consul-k8s/pull/551)]
* Helm
  * Rename `PartitionExports` CRD to `ExportedServices`. [[GH-902](https://github.com/hashicorp/consul-k8s/pull/902)]

IMPROVEMENTS:
* CLI
  * Pre-check in the `install` command to verify the correct license secret exists when using an enterprise Consul image. [[GH-875](https://github.com/hashicorp/consul-k8s/pull/875)]
* Control Plane
  * Add a label "managed-by" to every secret the control-plane creates. Only delete said secrets on an uninstall. [[GH-835](https://github.com/hashicorp/consul-k8s/pull/835)]
  * Add support for labeling a Kubernetes service with `consul.hashicorp.com/service-ignore` to prevent services from being registered in Consul. [[GH-858](https://github.com/hashicorp/consul-k8s/pull/858)]
* Helm Chart
  * Fail an installation/upgrade if WAN federation and Admin Partitions are both enabled. [[GH-892](https://github.com/hashicorp/consul-k8s/issues/892)]
  * Add support for setting `ingressClassName` for UI. [[GH-909](https://github.com/hashicorp/consul-k8s/pull/909)]
  * Add partition support to Service Resolver, Service Router and Service Splitter CRDs. [[GH-908](https://github.com/hashicorp/consul-k8s/issues/908)]

BUG FIXES:
* Control Plane:
  * Add a workaround to check that the ACL token is replicated to other Consul servers. [[GH-862](https://github.com/hashicorp/consul-k8s/issues/862)]
  * Return 500 on prometheus response if unable to get metrics from Envoy. [[GH-551](https://github.com/hashicorp/consul-k8s/pull/551)]
  * Don't include body of failed service metrics calls in merged metrics response. [[GH-551](https://github.com/hashicorp/consul-k8s/pull/551)]
* Helm Chart
  * Admin Partitions **(Consul Enterprise only)**: Do not mount Consul CA certs to partition-init job if `externalServers.useSystemRoots` is `true`. [[GH-885](https://github.com/hashicorp/consul-k8s/pull/885)]

## 0.37.0 (November 18, 2021)

BREAKING CHANGES:
* Previously [UI metrics](https://www.consul.io/docs/connect/observability/ui-visualization) would be enabled when
  `global.metrics=false` and `ui.metrics.enabled=-`. If you are no longer seeing UI metrics,
  set `global.metrics=true` or `ui.metrics.enabled=true`. [[GH-841](https://github.com/hashicorp/consul-k8s/pull/841)]
* The `enterpriseLicense` section of the values file has been migrated from being under the `server` stanza to being
  under the `global` stanza. Migrating the contents of `server.enterpriseLicense` to `global.enterpriseLicense` will
  ensure the license job works. [[GH-856](https://github.com/hashicorp/consul-k8s/pull/856)]
* Consul [streaming](https://www.consul.io/docs/agent/options#use_streaming_backend) is re-enabled by default.
  Streaming is broken when using multi-DC federation and Consul versions 1.10.0, 1.10.1, 1.10.2.
  If you are using those versions and multi-DC federation, you must upgrade to Consul >= 1.10.3 or set:

  ```yaml
  client:
    extraConfig: |
      {"use_streaming_backend": false}
  ```
  
  [[GH-851](https://github.com/hashicorp/consul-k8s/pull/851)]

FEATURES:
* Helm Chart
  * Add support for Consul services to utilize Consul DNS for service discovery. Set `dns.enableRedirection` to allow services to
    use Consul DNS via the Consul DNS Service. [[GH-833](https://github.com/hashicorp/consul-k8s/pull/833)]
* Control Plane
  * Connect: Allow services using Connect to utilize Consul DNS to perform service discovery. [[GH-833](https://github.com/hashicorp/consul-k8s/pull/833)]

IMPROVEMENTS:
* Control Plane
  * TLS: Support PKCS1 and PKCS8 private keys for Consul certificate authority. [[GH-843](https://github.com/hashicorp/consul-k8s/pull/843)]
  * Connect: Log a warning when ACLs are enabled and the default service account is used. [[GH-842](https://github.com/hashicorp/consul-k8s/pull/842)]
  * Update Service Router, Service Splitter and Ingress Gateway CRD with support for RequestHeaders and ResponseHeaders. [[GH-863](https://github.com/hashicorp/consul-k8s/pull/863)]
  * Update Ingress Gateway CRD with partition support for the IngressService and TLS Config. [[GH-863](https://github.com/hashicorp/consul-k8s/pull/863)]
* CLI
  * Delete jobs, cluster roles, and cluster role bindings on `uninstall`. [[GH-820](https://github.com/hashicorp/consul-k8s/pull/820)]
* Helm Chart
  * Add `component` labels to all resources. [[GH-840](https://github.com/hashicorp/consul-k8s/pull/840)]
  * Update Consul version to 1.10.4. [[GH-861](https://github.com/hashicorp/consul-k8s/pull/861)]
  * Update Service Router, Service Splitter and Ingress Gateway CRD with support for RequestHeaders and ResponseHeaders. [[GH-863](https://github.com/hashicorp/consul-k8s/pull/863)]
  * Update Ingress Gateway CRD with partition support for the IngressService and TLS Config. [[GH-863](https://github.com/hashicorp/consul-k8s/pull/863)]
  * Re-enable streaming for Consul clients. [[GH-851](https://github.com/hashicorp/consul-k8s/pull/851)]

BUG FIXES:
* Control Plane
  * ACLs: Fix issue where if one or more servers fail to have their ACL tokens set on the initial run of server-acl-init
    then on subsequent re-runs of server-acl-init the tokens are never set. [[GH-825](https://github.com/hashicorp/consul-k8s/issues/825)]
  * ACLs: Fix issue where if the number of Consul servers is increased, the new servers are never provisioned
    an ACL token. [[GH-677](https://github.com/hashicorp/consul-k8s/issues/677)]
  * Fix issue where after a `helm upgrade`, users would see `x509: certificate signed by unknown authority.`
    errors when modifying config entry resources. [[GH-837](https://github.com/hashicorp/consul-k8s/pull/837)]
* Helm Chart
  * **(Consul Enterprise only)** Error on Helm install if a reserved name is used for the admin partition name or a
    Consul destination namespace for connect or catalog sync. [[GH-846](https://github.com/hashicorp/consul-k8s/pull/846)]
  * Truncate Persistent Volume Claim names when namespace names are too long. [[GH-799](https://github.com/hashicorp/consul-k8s/pull/799)]
  * Fix issue where UI metrics would be enabled when `global.metrics=false` and `ui.metrics.enabled=-`. [[GH-841](https://github.com/hashicorp/consul-k8s/pull/841)]
  * Populate the federation secret with the generated Gossip key when `global.gossipEncryption.autoGenerate` is set to true. [[GH-854](https://github.com/hashicorp/consul-k8s/pull/854)]

## 0.36.0 (November 02, 2021)

BREAKING CHANGES:
* Helm Chart
  * The `kube-system` and `local-path-storage` namespaces are now _excluded_ from connect injection by default on Kubernetes versions >= 1.21. If you wish to enable injection on those namespaces, set `connectInject.namespaceSelector` to `null`. [[GH-726](https://github.com/hashicorp/consul-k8s/pull/726)]

IMPROVEMENTS:
* Helm Chart
  * Automatic retry for `gossip-encryption-autogenerate-job` on failure [[GH-789](https://github.com/hashicorp/consul-k8s/pull/789)]
  * `kube-system` and `local-path-storage` namespaces are now excluded from connect injection by default on Kubernetes versions >= 1.21. This prevents deadlock issues when `kube-system` components go down and allows Kind to work without changing the failure policy of the mutating webhook. [[GH-726](https://github.com/hashicorp/consul-k8s/pull/726)]
  * Add support for services across Admin Partitions to communicate using mesh gateways. [[GH-807](https://github.com/hashicorp/consul-k8s/pull/807)]
    * Documentation for the installation can be found [here](https://github.com/hashicorp/consul-k8s/blob/main/docs/admin-partitions-with-acls.md).
  * Add support for PartitionExports CRD to enable cross-partition networking. [[GH-802](https://github.com/hashicorp/consul-k8s/pull/802)]
* CLI
  * Add `status` command. [[GH-768](https://github.com/hashicorp/consul-k8s/pull/768)]
  * Add `-verbose`, `-v` flag to the `consul-k8s install` command, which outputs all logs emitted from the installation. By default, verbose is set to `false` to hide logs that show resources are not ready. [[GH-810](https://github.com/hashicorp/consul-k8s/pull/810)]
  * Set `prometheus.enabled` to true and enable all metrics for Consul K8s when installing via the `demo` preset. [[GH-809](https://github.com/hashicorp/consul-k8s/pull/809)]
  * Set `controller.enabled` to `true` when installing via the `demo` preset. [[GH818](https://github.com/hashicorp/consul-k8s/pull/818)]
  * Set `global.gossipEncryption.autoGenerate` to `true` and `global.tls.enableAutoEncrypt` to `true` when installing via the `secure` preset. [[GH818](https://github.com/hashicorp/consul-k8s/pull/818)]
* Control Plane
  * Add support for partition-exports config entry as a Custom Resource Definition to help manage cross-partition networking. [[GH-802](https://github.com/hashicorp/consul-k8s/pull/802)]

## 0.35.0 (October 19, 2021)

FEATURES:
* Control Plane
  * Add `gossip-encryption-autogenerate` subcommand to generate a random 32 byte Kubernetes secret to be used as a gossip encryption key. [[GH-772](https://github.com/hashicorp/consul-k8s/pull/772)]
  * Add support for `partition-exports` config entry. [[GH-802](https://github.com/hashicorp/consul-k8s/pull/802)], [[GH-803](https://github.com/hashicorp/consul-k8s/pull/803)]
* Helm Chart
  * Add automatic generation of gossip encryption with `global.gossipEncryption.autoGenerate=true`. [[GH-738](https://github.com/hashicorp/consul-k8s/pull/738)]
  * Add support for configuring resources for mesh gateway `service-init` container. [[GH-758](https://github.com/hashicorp/consul-k8s/pull/758)]
  * Add support for `PartitionExports` CRD. [[GH-802](https://github.com/hashicorp/consul-k8s/pull/802)], [[GH-803](https://github.com/hashicorp/consul-k8s/pull/803)]

IMPROVEMENTS:
* Control Plane
  * Upgrade Docker image Alpine version from 3.13 to 3.14. [[GH-737](https://github.com/hashicorp/consul-k8s/pull/737)]
  * CRDs: tune failure backoff so invalid config entries are re-synced more quickly. [[GH-788](https://github.com/hashicorp/consul-k8s/pull/788)]
* Helm Chart
  * Enable adding extra containers to server and client Pods. [[GH-749](https://github.com/hashicorp/consul-k8s/pull/749)]
  * ACL support for Admin Partitions. **(Consul Enterprise only)**
  **BETA** [[GH-766](https://github.com/hashicorp/consul-k8s/pull/766)]
    * This feature now enabled ACL support for Admin Partitions. The server-acl-init job now creates a Partition token. This token
      can be used to bootstrap new partitions as well as manage ACLs in the non-default partitions.
    * Partition to partition networking is disabled if ACLs are enabled.
    * Documentation for the installation can be found [here](https://github.com/hashicorp/consul-k8s/blob/main/docs/admin-partitions-with-acls.md).
* CLI
  * Add `version` command. [[GH-741](https://github.com/hashicorp/consul-k8s/pull/741)]
  * Add `uninstall` command. [[GH-725](https://github.com/hashicorp/consul-k8s/pull/725)]

## 0.34.1 (September 17, 2021)

BUG FIXES:
* Helm
  * Fix consul-k8s image version in values file. [[GH-732](https://github.com/hashicorp/consul-k8s/pull/732)]

## 0.34.0 (September 17, 2021)

FEATURES:
* CLI
  * The `consul-k8s` CLI enables users to deploy and operate Consul on Kubernetes.
    * Support `consul-k8s install` command. [[GH-713](https://github.com/hashicorp/consul-k8s/pull/713)]
* Helm Chart
  * Add support for Admin Partitions. **(Consul Enterprise only)**
  **ALPHA** [[GH-729](https://github.com/hashicorp/consul-k8s/pull/729)]
    * This feature allows Consul to be deployed across multiple Kubernetes clusters while sharing a single set of Consul
servers. The services on each cluster can be independently managed. This feature is an alpha feature. It requires:
      * a flat pod and node network in order for inter-partition networking to work.
      * TLS to be enabled.
      * Consul Namespaces enabled.

      Transparent Proxy is unsupported for cross partition communication.

To enable Admin Partitions on the server cluster use the following config.

```yaml
global:
  enableConsulNamespaces: true
  tls:
    enabled: true
  image: hashicorp/consul-enterprise:1.11.0-ent-alpha
  adminPartitions:
    enabled: true
server:
  exposeGossipAndRPCPorts: true
  enterpriseLicense:
    secretName: license
    secretKey: key
connectInject:
  enabled: true
  transparentProxy:
    defaultEnabled: false
  consulNamespaces:
    mirroringK8S: true
controller:
  enabled: true
```

Identify the LoadBalancer External IP of the `partition-service`

```bash
kubectl get svc consul-consul-partition-service -o json | jq -r '.status.loadBalancer.ingress[0].ip'
```

Migrate the TLS CA credentials from the server cluster to the workload clusters

```bash
kubectl get secret consul-consul-ca-key --context "server-context" -o yaml | kubectl apply --context "workload-context" -f -
kubectl get secret consul-consul-ca-cert --context "server-context" -o yaml | kubectl apply --context "workload-context" -f -
```

Configure the workload cluster using the following config.

```yaml
global:
  enabled: false
  enableConsulNamespaces: true
  image: hashicorp/consul-enterprise:1.11.0-ent-alpha
  adminPartitions:
    enabled: true
    name: "alpha" # Name of Admin Partition
  tls:
    enabled: true
    caCert:
      secretName: consul-consul-ca-cert
      secretKey: tls.crt
    caKey:
      secretName: consul-consul-ca-key
      secretKey: tls.key
server:
  enterpriseLicense:
    secretName: license
    secretKey: key
externalServers:
  enabled: true
  hosts: [ "loadbalancer IP" ] # external IP of partition service LB
  tlsServerName: server.dc1.consul
client:
  enabled: true
  exposeGossipPorts: true
  join: [ "loadbalancer IP" ] # external IP of partition service LB
connectInject:
  enabled: true
  consulNamespaces:
    mirroringK8S: true
controller:
  enabled: true
```

This should lead to the workload cluster having only Consul agents that connect with the Consul server. Services in this
cluster behave like independent services. They can be configured to communicate with services in other partitions by
configuring the upstream configuration on the individual services.

* Control Plane
  * Add support for Admin Partitions. **(Consul Enterprise only)** **
    ALPHA** [[GH-729](https://github.com/hashicorp/consul-k8s/pull/729)]
    * Add Partition-Init job that runs in Kubernetes clusters that do not have servers running to provision Admin
      Partitions.
    * Update endpoints-controller, config-entry controller and config entries to add partition config to them.

IMPROVEMENTS:
* Helm Chart
  * Add ability to specify port for ui service. [[GH-604](https://github.com/hashicorp/consul-k8s/pull/604)]
  * Use `policy/v1` for Consul server `PodDisruptionBudget` if supported. [[GH-606](https://github.com/hashicorp/consul-k8s/pull/606)]
  * Add readiness, liveness and startup probes to the connect inject deployment. [[GH-626](https://github.com/hashicorp/consul-k8s/pull/626)][[GH-701](https://github.com/hashicorp/consul-k8s/pull/701)]
  * Add support for setting container security contexts on client and server Pods. [[GH-620](https://github.com/hashicorp/consul-k8s/pull/620)]
  * Update Envoy image to 1.18.4 [[GH-699](https://github.com/hashicorp/consul-k8s/pull/699)]
  * Add configuration for webhook-cert-manager tolerations [[GH-712](https://github.com/hashicorp/consul-k8s/pull/712)]
  * Update default Consul version to 1.10.2 [[GH-718](https://github.com/hashicorp/consul-k8s/pull/718)]
* Control Plane
  * Add health endpoint to the connect inject webhook that will be healthy when webhook certs are present and not empty. [[GH-626](https://github.com/hashicorp/consul-k8s/pull/626)]
  * Catalog Sync: Fix issue registering NodePort services with wrong IPs when a node has multiple IP addresses. [[GH-619](https://github.com/hashicorp/consul-k8s/pull/619)]
  * Allow registering the same service in multiple namespaces. [[GH-697](https://github.com/hashicorp/consul-k8s/pull/697)]

BUG FIXES:
* Helm Chart
  * Disable [streaming](https://www.consul.io/docs/agent/options#use_streaming_backend) on Consul clients because it is currently not supported when
    doing mesh gateway federation. If you wish to enable it, override the setting using `client.extraConfig`:

    ```yaml
    client:
      extraConfig: |
        {"use_streaming_backend": true}
    ```
    [[GH-718](https://github.com/hashicorp/consul-k8s/pull/718)]

## 0.33.0 (August 12, 2021)

BREAKING CHANGES:
* The consul-k8s repository has been merged with consul-helm and now contains the `consul-k8s-control-plane` binary (previously named `consul-k8s`) and the Helm chart to deploy Consul on Kubernetes. The docker image previously named `hashicorp/consul-k8s` has been renamed to `hashicorp/consul-k8s-control-plane`. The binary and Helm chart will be released together with the same version. **NOTE: If you install Consul through the Helm chart and are not customizing the `global.imageK8S` value then this will not be a breaking change.** [[GH-589](https://github.com/hashicorp/consul-k8s/pull/589)]
  * Helm chart v0.33.0+ will support the corresponding `consul-k8s-control-plane` image with the same version only. For example Helm chart 0.33.0 will only be supported to work with the default value `global.imageK8S`: `hashicorp/consul-k8s-control-plane:0.33.0`.
  * The control-plane binary has been renamed from `consul-k8s` to `consul-k8s-control-plane` and is now invoked as `consul-k8s-control-plane` in the Helm chart. The first version of this newly renamed binary will be 0.33.0.
  * The Go module `github.com/hashicorp/consul-k8s` has been named to `github.com/hashicorp/consul-k8s/control-plane`.
  * The Helm chart is located under `consul-k8s/charts/consul`.
  * The control-plane source code is located under `consul-k8s/control-plane`.
* Minimum Kubernetes versions supported is 1.17+ and now matches what is stated in the `README.md` file.  [[GH-1053](https://github.com/hashicorp/consul-helm/pull/1053)]

IMPROVEMENTS:
* Control Plane
  * Add flags `-log-level`, `-log-json` to all subcommands to control log level and json formatting. [[GH-523](https://github.com/hashicorp/consul-k8s/pull/523)]
  * Execute Consul clients and servers using the Docker entrypoint for consistency. [[GH-590](https://github.com/hashicorp/consul-k8s/pull/590)]
* Helm Chart
  * Substitute `HOST_IP/POD_IP/HOSTNAME` variables in `server.extraConfig` and `client.extraConfig` so they are passed in to server/client config already evaluated at runtime. [[GH-1042](https://github.com/hashicorp/consul-helm/pull/1042)]
  * Set failurePolicy to Fail for connectInject mutating webhook so that pods fail to schedule when the webhook is offline. This can be controlled via `connectInject.failurePolicy`. [[GH-1024](https://github.com/hashicorp/consul-helm/pull/1024)]
  * Allow setting global.logLevel and global.logJSON and propogate this to all consul-k8s commands. [[GH-980](https://github.com/hashicorp/consul-helm/pull/980)]
  * Allow setting `connectInject.replicas` to control number of replicas of webhook injector. [[GH-1029](https://github.com/hashicorp/consul-helm/pull/1029)]
  * Add the ability to manually specify a k8s secret containing server-cert via the value `server.serverCert.secretName`. [[GH-1024](https://github.com/hashicorp/consul-helm/pull/1046)]
  * Allow setting `ui.pathType` for providers that do not support the default pathType "Prefix". [[GH-1012](https://github.com/hashicorp/consul-helm/pull/1012)]
  * Allow setting `client.nodeMeta` to specify arbitrary key-value pairs to associate with the node. [[GH-728](https://github.com/hashicorp/consul-helm/pull/728)]

BUG FIXES:
* Control Plane
  * Connect: Use `AdmissionregistrationV1` instead of `AdmissionregistrationV1beta1` API as it was deprecated in k8s 1.16. [[GH-558](https://github.com/hashicorp/consul-k8s/pull/558)]
  * Connect: Fix bug where environment variables `<NAME>_CONNECT_SERVICE_HOST` and
  `<NAME>_CONNECT_SERVICE_PORT` weren't being set when the upstream annotation was used. [[GH-549](https://github.com/hashicorp/consul-k8s/issues/549)]
  * Connect: Fix a bug with leaving around ACL tokens after a service has been deregistered. Note that this will not clean up existing leftover ACL tokens. [[GH-540](https://github.com/hashicorp/consul-k8s/issues/540)][[GH-599](https://github.com/hashicorp/consul-k8s/issues/599)]
  * CRDs: Fix ProxyDefaults and ServiceDefaults resources not syncing with Consul < 1.10.0 [[GH-1023](https://github.com/hashicorp/consul-helm/issues/1023)]
  * Connect: Skip service registration for duplicate services only on Kubernetes. [[GH-581](https://github.com/hashicorp/consul-k8s/pull/581)]
  * Connect: redirect-traffic command passes ACL token when ACLs are enabled. [[GH-576](https://github.com/hashicorp/consul-k8s/pull/576)]

## 0.26.0 (June 22, 2021)

FEATURES:
* Connect: Support Transparent Proxy. [[GH-481](https://github.com/hashicorp/consul-k8s/pull/481)]
  This feature enables users to use KubeDNS to reach other services within the Consul Service Mesh,
  as well as enforces the inbound and outbound traffic to go through the Envoy proxy.

  Using transparent proxy for your service mesh applications means:
  - Proxy service registrations will set `mode` to `transparent` in the proxy configuration
    so that Consul can configure the Envoy proxy to have an inbound and outbound listener.
  - Both proxy and service registrations will include the cluster IP and service port of the Kubernetes service
    as tagged addresses so that Consul can configure Envoy to route traffic based on that IP and port.
  - The `consul-connect-inject-init` container will run `consul connect redirect-traffic` [command](https://www.consul.io/commands/connect/redirect-traffic),
    which will apply rules (via iptables) to redirect inbound and outbound traffic to the proxy.
    To run this command the `consul-connect-inject-init` requires running as root with capability `NET_ADMIN`.

  This feature includes the following changes:
  * Add new `-enable-transparent-proxy` flag to the `inject-connect` command.
    When `true`, transparent proxy will be used for all services on the Consul Service Mesh
    within a Kubernetes cluster. This flag defaults to `true`.
  * Add new `consul.hashicorp.com/transparent-proxy` pod annotation to allow enabling and disabling transparent
    proxy for individual services.
* CRDs: Add CRD for MeshConfigEntry. Supported in Consul 1.10+ [[GH-513](https://github.com/hashicorp/consul-k8s/pull/513)]
* Connect: Overwrite Kubernetes HTTP readiness and/or liveness probes to point to Envoy proxy when
  transparent proxy is enabled. [[GH-517](https://github.com/hashicorp/consul-k8s/pull/517)]
* Connect: Allow exclusion of inbound ports, outbound ports and CIDRs, and additional user IDs when
  Transparent Proxy is enabled. [[GH-506](https://github.com/hashicorp/consul-k8s/pull/506)]

  The following annotations are supported:
  * `consul.hashicorp.com/transparent-proxy-exclude-inbound-ports` - Comma-separated list of inbound ports to exclude.
  * `consul.hashicorp.com/transparent-proxy-exclude-outbound-ports` - Comma-separated list of outbound ports to exclude.
  * `consul.hashicorp.com/transparent-proxy-exclude-outbound-cidrs` - Comma-separated list of IPs or CIDRs to exclude.
  * `consul.hashicorp.com/transparent-proxy-exclude-uids` - Comma-separated list of Linux user IDs to exclude.
* Connect: Add the ability to set default tproxy mode at namespace level via label. [[GH-501](https://github.com/hashicorp/consul-k8s/pull/510)]
  * Setting the annotation `consul.hashicorp.com/transparent-proxy` to `true/false` will define whether tproxy is enabled/disabled for the pod.
  * Setting the label `consul.hashicorp.com/transparent-proxy` to `true/false` on a namespace will define the default behavior for pods in that namespace, which do not also have the annotation set.
  * The default tproxy behavior will be defined by the value of `-enable-transparent-proxy` flag to the `consul-k8s inject-connect` command. It can be overridden in a namespace by the the label on the namespace or for a pod using the annotation on the pod.
* Connect: support upgrades for services deployed before endpoints controller to
  upgrade to a version of consul-k8s with endpoints controller. [[GH-509](https://github.com/hashicorp/consul-k8s/pull/509)]
* Connect: A new command `consul-k8s connect-init` has been added.
  It replaces the existing init-container logic for ACL login and Envoy bootstrapping and introduces a polling wait for service registration,
  see `Endpoints Controller` for more information.
  [[GH-446](https://github.com/hashicorp/consul-k8s/pull/446)], [[GH-452](https://github.com/hashicorp/consul-k8s/pull/452)], [[GH-459](https://github.com/hashicorp/consul-k8s/pull/459)]
* Connect: A new controller `Endpoints Controller` has been added which is responsible for managing service endpoints and service registration.
  When a Kubernetes service references a deployed connect-injected pod, the endpoints controller will be responsible for managing the lifecycle of the connect-injected deployment. [[GH-455](https://github.com/hashicorp/consul-k8s/pull/455)], [[GH-467](https://github.com/hashicorp/consul-k8s/pull/467)], [[GH-470](https://github.com/hashicorp/consul-k8s/pull/470)], [[GH-475](https://github.com/hashicorp/consul-k8s/pull/475)]
  - This includes:
    - service registration and deregistration, formerly managed by the `consul-connect-inject-init`.
    - monitoring health checks, formerly managed by `healthchecks-controller`.
    - re-registering services in the events of consul agent failures, formerly managed by `consul-sidecar`.
  - The endpoints controller replaces the health checks controller while preserving existing functionality. [[GH-472](https://github.com/hashicorp/consul-k8s/pull/472)]
  - The endpoints controller replaces the cleanup controller while preserving existing functionality.
    [[GH-476](https://github.com/hashicorp/consul-k8s/pull/476)], [[GH-454](https://github.com/hashicorp/consul-k8s/pull/454)]
  - Merged metrics configuration support is now partially managed by the endpoints controller.
    [[GH-469](https://github.com/hashicorp/consul-k8s/pull/469)]

IMPROVEMENTS:
* Connect: skip service registration when a service with the same name but in a different Kubernetes namespace is found
  and Consul namespaces are not enabled. [[GH-527](https://github.com/hashicorp/consul-k8s/pull/527)]
* Connect: Leader election support for connect-inject deployment. [[GH-479](https://github.com/hashicorp/consul-k8s/pull/479)]
* Connect: the `consul-connect-inject-init` container has been split into two init containers. [[GH-441](https://github.com/hashicorp/consul-k8s/pull/441)]
 Connect: Connect webhook no longer generates its own certificates and relies on them being provided as files on the disk.
  [[GH-454](https://github.com/hashicorp/consul-k8s/pull/454)]]
* CRDs: Update `ServiceDefaults` with `Mode`, `TransparentProxy`, `DialedDirectly` and `UpstreamConfigs` fields. Note: `Mode` and `TransparentProxy` should not be set
  using this CRD but via annotations. [[GH-502](https://github.com/hashicorp/consul-k8s/pull/502)], [[GH-485](https://github.com/hashicorp/consul-k8s/pull/485)], [[GH-533](https://github.com/hashicorp/consul-k8s/pull/533)]
* CRDs: Update `ProxyDefaults` with `Mode`, `DialedDirectly` and `TransparentProxy` fields. Note: `Mode` and `TransparentProxy` should not be set
  using the CRD but via annotations. [[GH-505](https://github.com/hashicorp/consul-k8s/pull/505)], [[GH-485](https://github.com/hashicorp/consul-k8s/pull/485)], [[GH-533](https://github.com/hashicorp/consul-k8s/pull/533)]
* CRDs: update the CRD versions from v1beta1 to v1. [[GH-464](https://github.com/hashicorp/consul-k8s/pull/464)]
* Delete secrets created by webhook-cert-manager when the deployment is deleted. [[GH-530](https://github.com/hashicorp/consul-k8s/pull/530)]

BUG FIXES:
* CRDs: Update the type of connectTimeout and TTL in ServiceResolver and ServiceRouter from time.Duration to metav1.Duration.
  This allows a user to set these values as a duration string on the resource. Existing resources that had set a specific integer
  duration will continue to function with a duration with 'n' nanoseconds, 'n' being the set value.
* CRDs: Fix a bug where the `config` field in `ProxyDefaults` CR failed syncing to Consul because `apiextensions.k8s.io/v1` requires CRD spec to have structured schema. [[GH-495](https://github.com/hashicorp/consul-k8s/pull/495)]
* CRDs: make `lastSyncedTime` a pointer to prevent setting last synced time Reconcile errors. [[GH-466](https://github.com/hashicorp/consul-k8s/pull/466)]

BREAKING CHANGES:
* Connect: Add a security context to the init copy container and the envoy sidecar and ensure they
  do not run as root. If a pod container shares the same `runAsUser` (5995) as Envoy an error is returned.
  [[GH-493](https://github.com/hashicorp/consul-k8s/pull/493)]
* Connect: Kubernetes Services are required for all Consul Service Mesh applications.
  The Kubernetes service name will be used as the service name to register with Consul
  unless the annotation `consul.hashicorp.com/connect-service` is provided to the deployment/pod to override this.
  If using ACLs, the ServiceAccountName must match the service name used with Consul.

  *Note*: if you're already using a Kubernetes service, no changes required.

  Example Service:
  ```yaml
  ---
  apiVersion: v1
  kind: Service
  metadata:
    name: sample-app
  spec:
    selector:
      app: sample-app
    ports:
      - port: 80
        targetPort: 9090
  ---
  apiVersion: apps/v1
  kind: Deployment
  metadata:
    labels:
      app: sample-app
    name: sample-app
  spec:
    replicas: 1
    selector:
       matchLabels:
         app: sample-app
    template:
      metadata:
        annotations:
          'consul.hashicorp.com/connect-inject': 'true'
        labels:
          app: sample-app
      spec:
        containers:
        - name: sample-app
          image: sample-app:0.1.0
          ports:
          - containerPort: 9090
  ```
* Connect: `consul.hashicorp.com/connect-sync-period` annotation is no longer supported.
  This annotation used to configure the sync period of the `consul-sidecar` (aka `lifecycle-sidecar`).
  Since we no longer inject the `consul-sidecar` to keep services registered in Consul, this annotation has
  been removed. [[GH-467](https://github.com/hashicorp/consul-k8s/pull/467)]
* Connect: transparent proxy feature enabled by default. This may break existing deployments.
  Please see details of the feature.

## 0.26.0-beta3 (May 27, 2021)

IMPROVEMENTS:
* Connect: Overwrite Kubernetes HTTP readiness and/or liveness probes to point to Envoy proxy when
  transparent proxy is enabled. [[GH-517](https://github.com/hashicorp/consul-k8s/pull/517)]
* Connect: Don't set security context for the Envoy proxy when on OpenShift and transparent proxy is disabled.
  [[GH-521](https://github.com/hashicorp/consul-k8s/pull/521)]
* Connect: `consul-connect-inject-init` run with `privileged: true` when transparent proxy is enabled.
  [[GH-524](https://github.com/hashicorp/consul-k8s/pull/524)]

BUG FIXES:
* Connect: Process every Address in an Endpoints object before returning an error. This ensures an address that isn't reconciled successfully doesn't prevent the remaining addresses from getting reconciled. [[GH-519](https://github.com/hashicorp/consul-k8s/pull/519)]

## 0.26.0-beta2 (May 06, 2021)

BREAKING CHANGES:
* Connect: Add a security context to the init copy container and the envoy sidecar and ensure they
  do not run as root. If a pod container shares the same `runAsUser` (5995) as Envoy an error is returned
  on scheduling. [[GH-493](https://github.com/hashicorp/consul-k8s/pull/493)]

IMPROVEMENTS:
* CRDs: Update ServiceDefaults with Mode, TransparentProxy and UpstreamConfigs fields. Note: Mode and TransparentProxy should not be set
  using this CRD but via annotations. [[GH-502](https://github.com/hashicorp/consul-k8s/pull/502)], [[GH-485](https://github.com/hashicorp/consul-k8s/pull/485)]
* CRDs: Update ProxyDefaults with Mode and TransparentProxy fields. Note: Mode and TransparentProxy should not be set
  using the CRD but via annotations. [[GH-505](https://github.com/hashicorp/consul-k8s/pull/505)], [[GH-485](https://github.com/hashicorp/consul-k8s/pull/485)]
* CRDs: Add CRD for MeshConfigEntry. Supported in Consul 1.10+ [[GH-513](https://github.com/hashicorp/consul-k8s/pull/513)]
* Connect: No longer set multiple tagged addresses in Consul when k8s service has multiple ports and Transparent Proxy is enabled.
  [[GH-511](https://github.com/hashicorp/consul-k8s/pull/511)]
* Connect: Allow exclusion of inbound ports, outbound ports and CIDRs, and additional user IDs when
  Transparent Proxy is enabled. [[GH-506](https://github.com/hashicorp/consul-k8s/pull/506)]

  The following annotations are supported:

  * `consul.hashicorp.com/transparent-proxy-exclude-inbound-ports` - Comma-separated list of inbound ports to exclude.
  * `consul.hashicorp.com/transparent-proxy-exclude-outbound-ports` - Comma-separated list of outbound ports to exclude.
  * `consul.hashicorp.com/transparent-proxy-exclude-outbound-cidrs` - Comma-separated list of IPs or CIDRs to exclude.
  * `consul.hashicorp.com/transparent-proxy-exclude-uids` - Comma-separated list of Linux user IDs to exclude.

* Connect: Add the ability to set default tproxy mode at namespace level via label. [[GH-501](https://github.com/hashicorp/consul-k8s/pull/510)]

  * Setting the annotation `consul.hashicorp.com/transparent-proxy` to `true/false` will define whether tproxy is enabled/disabled for the pod.
  * Setting the label `consul.hashicorp.com/transparent-proxy` to `true/false` on a namespace will define the default behavior for pods in that namespace, which do not also have the annotation set.
  * The default tproxy behavior will be defined by the value of `-enable-transparent-proxy` flag to the `consul-k8s inject-connect` command. It can be overridden in a namespace by the the label on the namespace or for a pod using the annotation on the pod.

* Connect: support upgrades for services deployed before endpoints controller to
  upgrade to a version of consul-k8s with endpoints controller. [[GH-509](https://github.com/hashicorp/consul-k8s/pull/509)]

* Connect: add additional logging to the endpoints controller and connect-init command to help
  the user debug if pods arent starting right away. [[GH-514](https://github.com/hashicorp/consul-k8s/pull/514/)]

BUG FIXES:
* Connect: Use `runAsNonRoot: false` for connect-init's container when tproxy is enabled. [[GH-493](https://github.com/hashicorp/consul-k8s/pull/493)]
* CRDs: Fix a bug where the `config` field in `ProxyDefaults` CR was not synced to Consul because
  `apiextensions.k8s.io/v1` requires CRD spec to have structured schema. [[GH-495](https://github.com/hashicorp/consul-k8s/pull/495)]
* Connect: Fix a bug where health status in Consul is updated incorrectly due to stale pod information in cache.
  [[GH-503](https://github.com/hashicorp/consul-k8s/pull/503)]

## 0.26.0-beta1 (April 16, 2021)

BREAKING CHANGES:
* Connect: Kubernetes Services are now required for all Consul Service Mesh applications.
  The Kubernetes service name will be used as the service name to register with Consul
  unless the annotation `consul.hashicorp.com/connect-service` is provided to the deployment/pod to override this.
  If using ACLs, the ServiceAccountName must match the service name used with Consul.
  
  *Note*: if you're already using a Kubernetes service, no changes are required.

  Example Service:
  ```yaml
  ---
  apiVersion: v1
  kind: Service
  metadata:
    name: sample-app
  spec:
    selector:
      app: sample-app
    ports:
      - port: 80
        targetPort: 9090
  ---
  apiVersion: apps/v1
  kind: Deployment
  metadata:
    labels:
      app: sample-app
    name: sample-app
  spec:
    replicas: 1
    selector:
       matchLabels:
         app: sample-app
    template:
      metadata:
        annotations:
          'consul.hashicorp.com/connect-inject': 'true'
        labels:
          app: sample-app
      spec:
        containers:
        - name: sample-app
          image: sample-app:0.1.0
          ports:
          - containerPort: 9090
    ```
* Connect: `consul.hashicorp.com/connect-sync-period` annotation is no longer supported.
  This annotation was used to configure the sync period of the `consul-sidecar` (aka `lifecycle-sidecar`).
  Since we no longer inject the `consul-sidecar` to keep services registered in Consul, this annotation is
  now meaningless. [[GH-467](https://github.com/hashicorp/consul-k8s/pull/467)]
* Connect: transparent proxy feature is enabled by default. This may break existing deployments.
  Please see details of the feature below.

FEATURES:
* Connect: Support Transparent Proxy. [[GH-481](https://github.com/hashicorp/consul-k8s/pull/481)]
  This feature enables users to use KubeDNS to reach other services within the Consul Service Mesh,
  as well as enforces the inbound and outbound traffic to go through the Envoy proxy.
  Using transparent proxy for your service mesh applications means:
  - Proxy service registrations will set `mode` to `transparent` in the proxy configuration
    so that Consul can configure the Envoy proxy to have an inbound and outbound listener.
  - Both proxy and service registrations will include the cluster IP and service port of the Kubernetes service
    as tagged addresses so that Consul can configure Envoy to route traffic based on that IP and port.
  - The `consul-connect-inject-init` container will run `consul connect redirect-traffic` [command](https://www.consul.io/commands/connect/redirect-traffic),
    which will apply rules (via iptables) to redirect inbound and outbound traffic to the proxy.
    To run this command the `consul-connect-inject-init` requires running as root with capability `NET_ADMIN`.
  
  **Note: this feature is currently in beta.** 
  
  This feature includes the following changes:
  * Add new `-enable-transparent-proxy` flag to the `inject-connect` command.
    When `true`, transparent proxy will be used for all services on the Consul Service Mesh
    within a Kubernetes cluster. This flag defaults to `true`.
  * Add new `consul.hashicorp.com/transparent-proxy` pod annotation to allow enabling and disabling transparent
    proxy for individual services.

IMPROVEMENTS:
* CRDs: update the CRD versions from v1beta1 to v1. [[GH-464](https://github.com/hashicorp/consul-k8s/pull/464)]
* Connect: the `consul-connect-inject-init` container has been split into two init containers. [[GH-441](https://github.com/hashicorp/consul-k8s/pull/441)]
* Connect: A new internal command `consul-k8s connect-init` has been added.
  It replaces the existing init container logic for ACL login and Envoy bootstrapping and introduces a polling wait for service registration,
  see `Endpoints Controller` for more information.
  [[GH-446](https://github.com/hashicorp/consul-k8s/pull/446)], [[GH-452](https://github.com/hashicorp/consul-k8s/pull/452)], [[GH-459](https://github.com/hashicorp/consul-k8s/pull/459)]
* Connect: A new controller `Endpoints Controller` has been added which is responsible for managing service endpoints and service registration.
  When a Kubernetes service referencing a connect-injected pod is deployed, the endpoints controller will be responsible for managing the lifecycle of the connect-injected deployment. [[GH-455](https://github.com/hashicorp/consul-k8s/pull/455)], [[GH-467](https://github.com/hashicorp/consul-k8s/pull/467)], [[GH-470](https://github.com/hashicorp/consul-k8s/pull/470)], [[GH-475](https://github.com/hashicorp/consul-k8s/pull/475)]
  - This includes:
      - service registration and deregistration, formerly managed by the `consul-connect-inject-init`.
      - monitoring health checks, formerly managed by `healthchecks-controller`.
      - re-registering services in the events of consul agent failures, formerly managed by `consul-sidecar`.

  - The endpoints controller replaces the health checks controller while preserving existing functionality. [[GH-472](https://github.com/hashicorp/consul-k8s/pull/472)]

  - The endpoints controller replaces the cleanup controller while preserving existing functionality.
    [[GH-476](https://github.com/hashicorp/consul-k8s/pull/476)], [[GH-454](https://github.com/hashicorp/consul-k8s/pull/454)]

  - Merged metrics configuration support is now partially managed by the endpoints controller.
    [[GH-469](https://github.com/hashicorp/consul-k8s/pull/469)]
* Connect: Leader election support for connect webhook and controller deployment. [[GH-479](https://github.com/hashicorp/consul-k8s/pull/479)]
* Connect: Connect webhook no longer generates its own certificates and relies on them being provided as files on the disk.
  [[GH-454](https://github.com/hashicorp/consul-k8s/pull/454)]] 
* Connect: Connect pods and their Envoy sidecars no longer have a preStop hook as service deregistration is managed by the endpoints controller.
  [[GH-467](https://github.com/hashicorp/consul-k8s/pull/467)]

BUG FIXES:
* CRDs: make `lastSyncedTime` a pointer to prevent setting last synced time Reconcile errors. [[GH-466](https://github.com/hashicorp/consul-k8s/pull/466)]

## 0.25.0 (March 18, 2021)

FEATURES:
* Metrics: add metrics configuration to inject-connect and metrics-merging capability to consul-sidecar. When metrics and metrics merging are enabled, the consul-sidecar will expose an endpoint that merges the app and proxy metrics.

  The flags `-merged-metrics-port`, `-service-metrics-port` and `-service-metrics-path` can be used to configure the merged metrics server, and the application service metrics endpoint on the consul sidecar.

  The flags `-default-enable-metrics`, `-default-enable-metrics-merging`, `-default-merged-metrics-port`, `-default-prometheus-scrape-port` and `-default-prometheus-scrape-path` configure the inject-connect command.

IMPROVEMENTS:
* CRDs: add field Last Synced Time to CRD status and add printer column on CRD to display time since when the
  resource was last successfully synced with Consul. [[GH-448](https://github.com/hashicorp/consul-k8s/pull/448)]
  
BUG FIXES:
* CRDs: fix incorrect validation for `ServiceResolver`. [[GH-456](https://github.com/hashicorp/consul-k8s/pull/456)]

## 0.24.0 (February 16, 2021)

BREAKING CHANGES:
* Connect: the `lifecycle-sidecar` command has been renamed to `consul-sidecar`. [[GH-428](https://github.com/hashicorp/consul-k8s/pull/428)]
* Connect: the `consul-connect-lifecycle-sidecar` container name has been changed to `consul-sidecar` and the `consul-connect-envoy-sidecar` container name has been changed to `envoy-sidecar`. 
[[GH-428](https://github.com/hashicorp/consul-k8s/pull/428)]
* Connect: the `-default-protocol` and `-enable-central-config` flags are no longer supported.
  The `consul.hashicorp.com/connect-service-protocol` annotation on Connect pods is also
  no longer supported. [[GH-418](https://github.com/hashicorp/consul-k8s/pull/418)]

  Current deployments that have the annotation should remove it, otherwise they
  will get an error if a pod from that deployment is rescheduled.

  Removing the annotation will not change their protocol
  since the config entry was already written to Consul. If you wish to change
  the protocol you must migrate the config entry to be managed by a
  [`ServiceDefaults`](https://www.consul.io/docs/agent/config-entries/service-defaults) resource.
  See [Upgrade to CRDs](https://www.consul.io/docs/k8s/crds/upgrade-to-crds) for more
  information.

  To set the protocol for __new__ services, you must use the
  [`ServiceDefaults`](https://www.consul.io/docs/agent/config-entries/service-defaults) resource,
  e.g.

  ```yaml
  apiVersion: consul.hashicorp.com/v1alpha1
  kind: ServiceDefaults
  metadata:
    name: my-service-name
  spec:
    protocol: "http"
  ```
* Connect: pods using an upstream that references a datacenter, e.g.
  `consul.hashicorp.com/connect-service-upstreams: service:8080:dc2` will
  error during injection if Consul does not have a `proxy-defaults` config entry
  with a [mesh gateway mode](https://www.consul.io/docs/connect/config-entries/proxy-defaults#mode)
  set to `local` or `remote`. [[GH-421](https://github.com/hashicorp/consul-k8s/pull/421)]

  In practice, this would have already been causing issues since without that
  config setting, traffic wouldn't have been routed through mesh gateways and
  so would not be actually making it to the other service.

FEATURES:
* CRDs: support annotation `consul.hashicorp.com/migrate-entry` on custom resources
  that will allow an existing config entry to be migrated onto a Kubernetes custom resource. [[GH-419](https://github.com/hashicorp/consul-k8s/pull/419)]
* Connect: add new cleanup controller that runs in the connect-inject deployment. This
  controller cleans up Consul service instances that remain registered despite their
  pods being deleted. This could happen if the pod's `preStop` hook failed to execute
  for some reason. [[GH-433](https://github.com/hashicorp/consul-k8s/pull/433)]

IMPROVEMENTS:
* CRDs: give a more descriptive error when a config entry already exists in Consul. [[GH-420](https://github.com/hashicorp/consul-k8s/pull/420)]
* Set `User-Agent: consul-k8s/<version>` header on calls to Consul where `<version>` is the current
  version of `consul-k8s`. [[GH-434](https://github.com/hashicorp/consul-k8s/pull/434)]

## 0.23.0 (January 22, 2021)

BUG FIXES:
* CRDs: Fix issue where a `ServiceIntentions` resource could be continually resynced with Consul
  because Consul's internal representation had a different order for an array than the Kubernetes resource. [[GH-416](https://github.com/hashicorp/consul-k8s/pull/416)] 
* CRDs: **(Consul Enterprise only)** default the `namespace` fields on resources where Consul performs namespace defaulting to prevent constant re-syncing.
  [[GH-413](https://github.com/hashicorp/consul-k8s/pull/413)]

IMPROVEMENTS:
* ACLs: give better error if policy that consul-k8s tries to update was created manually by user. [[GH-412](https://github.com/hashicorp/consul-k8s/pull/412)]

FEATURES:
* TLS: add `tls-init` command that is responsible for creating and updating Server TLS certificates. [[GH-410](https://github.com/hashicorp/consul-k8s/pull/410)]

## 0.22.0 (December 21, 2020)

BUG FIXES:
* Connect: on termination of a connect injected pod the lifecycle-sidecar sometimes re-registered the application resulting in
  stale service entries for applications which no longer existed. [[GH-409](https://github.com/hashicorp/consul-k8s/pull/409)]

BREAKING CHANGES:
* Connect: the flags `-envoy-image` and `-consul-image` for command `inject-connect` are now required. [[GH-405](https://github.com/hashicorp/consul-k8s/pull/405)]

FEATURES:
* CRDs: add new CRD `IngressGateway` for configuring Consul's [ingress-gateway](https://www.consul.io/docs/agent/config-entries/ingress-gateway) config entry. [[GH-407](https://github.com/hashicorp/consul-k8s/pull/407)]
* CRDs: add new CRD `TerminatingGateway` for configuring Consul's [terminating-gateway](https://www.consul.io/docs/agent/config-entries/terminating-gateway) config entry. [[GH-408](https://github.com/hashicorp/consul-k8s/pull/408)]

## 0.21.0 (November 25, 2020)

IMPROVEMENTS:
* Connect: Add `-log-level` flag to `inject-connect` command. [[GH-400](https://github.com/hashicorp/consul-k8s/pull/400)]
* Connect: Ensure `consul-connect-lifecycle-sidecar` container shuts down gracefully upon receiving `SIGTERM`. [[GH-389](https://github.com/hashicorp/consul-k8s/pull/389)]
* Connect: **(Consul Enterprise only)** give more descriptive error message if using Consul namespaces with a Consul installation that doesn't support namespaces. [[GH-399](https://github.com/hashicorp/consul-k8s/pull/399)]

## 0.20.0 (November 12, 2020)

FEATURES:
* Connect: Support Kubernetes health probe synchronization with Consul for connect injected pods. [[GH-363](https://github.com/hashicorp/consul-k8s/pull/363)]
    * Adds a new controller to the connect-inject webhook which is responsible for synchronizing Kubernetes pod health checks with Consul service instance health checks.
      A Consul health check is registered for each connect-injected pod which mirrors the pod's Readiness status to Consul. This modifies connect routing to only
      pods which have passing Kubernetes health checks. See breaking changes for more information.
    * Adds a new label to connect-injected pods which mirrors the `consul.hashicorp.com/connect-inject-status` annotation.
    * **(Consul Enterprise only)** Adds a new annotation to connect-injected pods when namespaces are enabled: `consul.hashicorp.com/consul-namespace`. [[GH-376](https://github.com/hashicorp/consul-k8s/pull/376)]

BREAKING CHANGES:
* Connect: With the addition of the connect-inject health checks controller any connect services which have failing Kubernetes readiness
  probes will no longer be routable through connect until their Kubernetes health probes are passing.
  Previously, if any connect services were failing their Kubernetes readiness checks they were still routable through connect.
  Users should verify that their connect services are passing Kubernetes readiness probes prior to using health checks synchronization.

DEPRECATIONS:
* `create-inject-token` in the server-acl-init command has been un-deprecated.
  `-create-inject-auth-method` has been deprecated and replaced by `-create-inject-token`.
  
  `-create-inject-namespace-token` in the server-acl-init command has been deprecated. Please use `-create-inject-token` and `-enable-namespaces` flags
  to achieve the same functionality. [[GH-368](https://github.com/hashicorp/consul-k8s/pull/368)]

IMPROVEMENTS:
* Connect: support passing extra arguments to the envoy binary. [[GH-378](https://github.com/hashicorp/consul-k8s/pull/378)]
    
    Arguments can be passed in 2 ways:
    * via a flag to the consul-k8s inject-connect command,
      e.g. `consul-k8s inject-connect -envoy-extra-args="--log-level debug --disable-hot-restart"`
    * via pod annotations,
      e.g. `consul.hashicorp.com/envoy-extra-args: "--log-level debug --disable-hot-restart"`
      
* CRDs:
   * Add Age column to CRDs. [[GH-365](https://github.com/hashicorp/consul-k8s/pull/365)]
   * Add validations and field descriptions for ServiceIntentions CRD. [[GH-385](https://github.com/hashicorp/consul-k8s/pull/385)]
   * Update CRD sync status if deletion in Consul fails. [[GH-365](https://github.com/hashicorp/consul-k8s/pull/365)]

BUG FIXES:
* Federation: **(Consul Enterprise only)** ensure replication ACL token can replicate policies and tokens in Consul namespaces other than `default`. [[GH-364](https://github.com/hashicorp/consul-k8s/issues/364)]
* CRDs: **(Consul Enterprise only)** validate custom resources can only set namespace fields if Consul namespaces are enabled. [[GH-375](https://github.com/hashicorp/consul-k8s/pull/375)]
* CRDs: Ensure ACL token is global so that secondary DCs can manage custom resources.
  Without this fix, controllers running in secondary datacenters would get ACL errors. [[GH-369](https://github.com/hashicorp/consul-k8s/pull/369)]
* CRDs: **(Consul Enterprise only)** Do not attempt to create a `*` namespace when service intentions specify `*` as `destination.namespace`. [[GH-382](https://github.com/hashicorp/consul-k8s/pull/382)]
* CRDs: **(Consul Enterprise only)** Fix namespace support for ServiceIntentions CRD. [[GH-362](https://github.com/hashicorp/consul-k8s/pull/362)]
* CRDs: Rename field namespaces -> namespace in ServiceResolver CRD. [[GH-365](https://github.com/hashicorp/consul-k8s/pull/365)]

## 0.19.0 (October 12, 2020)

FEATURES:
* Add beta support for new commands `consul-k8s controller` and `consul-k8s webhook-cert-manager`. [[GH-353](https://github.com/hashicorp/consul-k8s/pull/353)]

  `controller` will start a Kubernetes controller that acts on Consul
  Custom Resource Definitions. The currently supported CRDs are:
    * `ProxyDefaults` - https://www.consul.io/docs/agent/config-entries/proxy-defaults
    * `ServiceDefaults` - https://www.consul.io/docs/agent/config-entries/service-defaults
    * `ServiceSplitter` - https://www.consul.io/docs/agent/config-entries/service-splitter
    * `ServiceRouter` - https://www.consul.io/docs/agent/config-entries/service-router
    * `ServiceResolver` - https://www.consul.io/docs/agent/config-entries/service-resolver
    * `ServiceIntentions` (requires Consul >= 1.9.0) - https://www.consul.io/docs/agent/config-entries/service-intentions
   
   See [https://www.consul.io/docs/k8s/crds](https://www.consul.io/docs/k8s/crds)
   for more information on the CRD schemas. **Requires Consul >= 1.8.4**.
   
   `webhook-cert-manager` manages certificates for Kubernetes webhooks. It will
   refresh expiring certificates and update corresponding secrets and mutating
   webhook configurations.

BREAKING CHANGES:
* Connect: No longer set `--max-obj-name-len` flag when executing `envoy`. This flag
  was [deprecated](https://www.envoyproxy.io/docs/envoy/latest/version_history/v1.11.0#deprecated)
  in Envoy 1.11.0 and had no effect from then onwards. With Envoy >= 1.15.0 setting
  this flag will result in an error, hence why we're removing it. [[GH-350](https://github.com/hashicorp/consul-k8s/pull/350)]

  If you are running any Envoy version >= 1.11.0 this change will have no effect. If you
  are running an Envoy version < 1.11.0 then you must upgrade Envoy to a newer
  version. This can be done by setting the `global.imageEnvoy` key in the
  Consul Helm chart.

IMPROVEMENTS:

* Add an ability to configure the synthetic Consul node name where catalog sync registers services. [[GH-312](https://github.com/hashicorp/consul-k8s/pull/312)]
  * Sync: Add `-consul-node-name` flag to the `sync-catalog` command to configure the Consul node name for syncing services to Consul.
  * ACLs: Add `-sync-consul-node-name` flag to the server-acl-init command so that it can create correct policy for the sync catalog.

BUG FIXES:
* Connect: use the first secret of type `kubernetes.io/service-account-token` when creating/updating auth method. [[GH-350](https://github.com/hashicorp/consul-k8s/pull/321)]
  
## 0.18.1 (August 10, 2020)

BUG FIXES:

* Connect: Reduce downtime caused by an alias health check of the sidecar proxy not being healthy for up to 1 minute
  when a Connect-enabled service is restarted. Note that this fix reverts the behavior of Consul Connect to the behavior
  it had before consul-k8s `v0.16.0` and Consul `v1.8.x`, where Consul can route to potentially unhealthy instances of a service
  because we don't respect Kubernetes readiness/liveness checks yet. Please follow [GH-155](https://github.com/hashicorp/consul-k8s/issues/155)
  for updates on that feature. [[GH-305](https://github.com/hashicorp/consul-k8s/pull/305)]

## 0.18.0 (July 30, 2020)

IMPROVEMENTS:

* Connect: Add resource request and limit flags for the injected init and lifecycle sidecar containers. These flags replace the hardcoded values previously included. As part of this change, the default value for the lifecycle sidecar container memory limit has increased from `25Mi` to `50Mi`. [[GH-298](https://github.com/hashicorp/consul-k8s/pull/298)], [[GH-300](https://github.com/hashicorp/consul-k8s/pull/300)]

BUG FIXES:

* Connect: Respect allow/deny list flags when namespaces are disabled. [[GH-296](https://github.com/hashicorp/consul-k8s/issues/296)]

## 0.17.0 (July 09, 2020)

BREAKING CHANGES:

* ACLs: Always update Kubernetes auth method created by the `server-acl-init` job. Previously, we would only update the auth method if Consul namespaces are enabled. With this change, we always update it to make sure that any configuration changes or updates to the `connect-injector-authmethod-svc-account` are propagated [[GH-282](https://github.com/hashicorp/consul-k8s/pull/282)].
* Connect: Connect pods have had the following resource settings changed: `consul-connect-inject-init` now has its memory limit set to `150M` up from `25M` and `consul-connect-lifecycle-sidecar` has its CPU request and limit set to `20m` up from `10m`. [[GH-291](https://github.com/hashicorp/consul-k8s/pull/291)]

IMPROVEMENTS:

* Extracted Consul's HTTP flags into our own package so we no longer depend on the internal Consul golang module. [[GH-259](https://github.com/hashicorp/consul-k8s/pull/259)]

BUG FIXES:

* Connect: Update resource settings to fix out of memory errors and CPU usage at 100% of limit. [[GH-283](https://github.com/hashicorp/consul-k8s/issues/283), [consul-helm GH-515](https://github.com/hashicorp/consul-helm/issues/515)]
* Connect: Creating a pod with a different service account name than its Consul service name will now result in an error when ACLs are enabled.
  Previously this would not result in an error, but the pod would not be able to send or receive traffic because its ACL token would be for a
  different service name. [[GH-237](https://github.com/hashicorp/consul-k8s/issues/237)]

## 0.16.0 (June 17, 2020)

FEATURES:

* ACLs: `server-acl-init` now supports creating tokens for ingress and terminating gateways [[GH-264](https://github.com/hashicorp/consul-k8s/pull/264)].
  * Add `-ingress-gateway-name` flag that takes the name of an ingress gateway that needs an acl token. May be specified multiple times. [Enterprise Only] If using Consul namespaces and registering the gateway outside of the default namespace, specify the value in the form `<GatewayName>.<ConsulNamespace>`.
  * Add `-terminating-gateway-name` flag that takes the name of a terminating gateway that needs an acl token. May be specified multiple times. [Enterprise Only] If using Consul namespaces and registering the gateway outside of the default namespace, specify the value in the form `<GatewayName>.<ConsulNamespace>`.
* Connect: Add support for configuring resource settings for memory and cpu limits/requests for sidecar proxies. [[GH-267](https://github.com/hashicorp/consul-k8s/pull/267)]

BREAKING CHANGES:

* Gateways: `service-address` command will now return hostnames if that is the address of the Kubernetes LB. Previously it would resolve the hostname to 1 IP. The `-resolve-hostnames` flag was added to preserve the IP resolution behavior. [[GH-271](https://github.com/hashicorp/consul-k8s/pull/271)]

IMPROVEMENTS:

* Sync: Add `-sync-lb-services-endpoints` flag to optionally sync load balancer endpoint IPs instead of load balancer ingress IP or hostname to Consul [[GH-257](https://github.com/hashicorp/consul-k8s/pull/257)].
* Connect: Add pod name to the consul connect metadata for connect injected pods. [[GH-231](https://github.com/hashicorp/consul-k8s/issues/231)]

BUG FIXES:

* Connect:
    * Fix bug where preStop hook was malformed. This caused Consul ACL tokens to never be deleted for connect services. [[GH-265](https://github.com/hashicorp/consul-k8s/issues/265)]
    * Fix bug where environment variable for upstream was not populated when using a different datacenter resulted. [[GH-246](https://github.com/hashicorp/consul-k8s/issues/246)]
    * Fix bug where the Connect health-check was defined with a service name instead of a service ID. This check was passing in consul version before 1.8, but will now fail with versions 1.8 and higher. [[GH-272](https://github.com/hashicorp/consul-k8s/pull/272)]

## 0.15.0 (May 13, 2020)

BREAKING CHANGES:

* The `service-address` command now resolves load balancer hostnames to the
  first IP. Previously it would use the hostname directly.
  This is a stop-gap measure because Consul currently only supports
  IP addresses for mesh gateways. [[GH-260](https://github.com/hashicorp/consul-k8s/pull/260)]

FEATURES:

* Add new `create-federation-secret` command that will create a Kubernetes secret
  containing data needed for secondary datacenters to federate. This command should be run only in the primary datacenter. [[GH-253](https://github.com/hashicorp/consul-k8s/pull/253)]

## 0.14.0 (April 23, 2020)

BREAKING CHANGES:

* ACLs: Remove `-expected-replicas`, `-release-name`, and `-server-label-selector` flags
  in favor of the new required `-server-address` flag [[GH-238](https://github.com/hashicorp/consul-k8s/pull/238)].

FEATURES:

* ACLs: The `server-acl-init` command can now run against Consul servers running outside of k8s [[GH-243](https://github.com/hashicorp/consul-k8s/pull/243)]:
  * Add `-bootstrap-token-file` flag to provide your own bootstrap token. If set, the command will
    skip ACL bootstrapping.
  * `-server-address` flag can also take a [cloud auto-join](https://www.consul.io/docs/agent/cloud-auto-join.html)
    string to discover server addresses.
  * Add `-inject-auth-method-host` flag to allow configuring the location of the Kubernetes API server
    for the Kubernetes auth method. This is useful because during the login workflow
    Consul servers are talking to the Kubernetes API to verify the service account token.
    When Consul servers are external to the Kubernetes cluster,
    we no longer know the address of the Kubernetes API server that is accessible
    from the external Consul servers.

IMPROVEMENTS:

* ACLs: Add `-server-address` and `-server-port` flags
  so that we don't need to discover server pod IPs and ports through the Kubernetes API [[GH-238](https://github.com/hashicorp/consul-k8s/pull/238)].

BUG FIXES:

* Connect: Fix upstream annotation parsing when multiple prepared queries are separated by spaces [[GH-224](https://github.com/hashicorp/consul-k8s/issues/224)]
* ACLs: Fix bug with `acl-init -token-sink-file` where running the command twice would fail [[GH-248](https://github.com/hashicorp/consul-k8s/pull/248)]

## 0.13.0 (April 06, 2020)

FEATURES:

* ACLs: Support new flag `server-acl-init -create-acl-replication-token` that creates
  an ACL token with permissions to perform ACL replication. [[GH-210](https://github.com/hashicorp/consul-k8s/pull/210)]
* ACLs: Support ACL replication from another datacenter. If `-acl-replication-token-file`
  is set, the `server-acl-init` command will skip ACL bootstrapping and instead
  will use the token in that file to create policies and tokens. This enables
  the `server-acl-init` command to be run in secondary datacenters. [[GH-226](https://github.com/hashicorp/consul-k8s/pull/226)]
* ACLs: Support new flag `acl-init -token-sink-file` that will write the token
  to the specified file. [[GH-232](https://github.com/hashicorp/consul-k8s/pull/232)]
* Commands: Add new command `service-address` that writes the address of the
  specified Kubernetes service to a file. If the service is of type `LoadBalancer`,
  the command will wait until the external address of the load balancer has
  been assigned. If the service is of type `ClusterIP` it will write the cluster
  IP. Services of type `NodePort` or `ExternalName` will result in an error.
  [[GH-234](https://github.com/hashicorp/consul-k8s/pull/234) and [GH-235](https://github.com/hashicorp/consul-k8s/pull/235)]
  
  Example usage:
  
      consul-k8s service-address \
        -k8s-namespace=default \
        -name=consul-mesh-gateway \
        -output-file=address.txt

* Commands: Add new `get-consul-client-ca` command that retrieves Consul clients' CA when auto-encrypt is enabled
  and writes it to a file [[GH-211](https://github.com/hashicorp/consul-k8s/pull/211)].

IMPROVEMENTS:

* ACLs: The following ACL tokens have been changed to local tokens rather than
  global tokens because they only need to be valid in their local datacenter:
  `client`, `enterprise-license`, `snapshot-agent`. In addition, if Consul
  Enterprise namespaces are not enabled, the `catalog-sync` token will be local. [[GH-226](https://github.com/hashicorp/consul-k8s/pull/226)]
* ACLs: If running with `-create-acl-replication-token=true` and `-create-inject-auth-method=true`,
  the anonymous policy will be configured to allow read access to all nodes and
  services. This is required for cross-datacenter Consul Connect requests to
  work. [[GH-230](https://github.com/hashicorp/consul-k8s/pull/230)].
* ACLs: The policy for the anonymous token has been renamed from `dns-policy` to `anonymous-token-policy`
  since it is used for more than DNS now (see above). [[GH-230](https://github.com/hashicorp/consul-k8s/pull/230)].

BUG FIXES:

* Sync: Fix a race condition where sync would delete services at initial startup [[GH-208](https://github.com/hashicorp/consul-k8s/pull/208)]

DEPRECATIONS:

* ACLs: The flag `-init-type=sync` for the command `acl-init` has been deprecated.
  Only the flag `-init-type=client` is supported. Previously, setting `-init-type=sync`
  had no effect so this is not a breaking change. [[GH-232](https://github.com/hashicorp/consul-k8s/pull/232)]
* Connect: deprecate the `-consul-ca-cert` flag in favor of `-ca-file` [[GH-217](https://github.com/hashicorp/consul-k8s/pull/217)]

## 0.12.0 (February 21, 2020)

BREAKING CHANGES:

* Connect Injector
  * Previously the injector would inject sidecars into pods in all namespaces. New flags `-allow-k8s-namespace` and `-deny-k8s-namespace` have been added. If no `-allow-k8s-namespace` flag is specified, the injector **will not inject sidecars into pods in any namespace**. To maintain the previous behavior, set `-allow-k8s-namespace='*'`.
* Catalog Sync
  * `kube-system` and `kube-public` namespaces are now synced from **unless** `-deny-k8s-namespace=kube-system -deny-k8s-namespace=kube-public` are passed to the `sync-catalog` command.
  * Previously, multiple sync processes could be run in the same Kubernetes cluster with different source Kubernetes namespaces and the same `-consul-k8s-tag`. This is no longer possible.
  The sync processes will now delete one-another's registrations. To continue running multiple sync processes, each process must be passed a different `-consul-k8s-tag` flag.
  * Previously, catalog sync would delete services tagged with `-consul-k8s-tag` (defaults to `k8s`) that were registered out-of-band, i.e. not by the sync process itself. It would delete services regardless of which node they were registered on.
  Now the sync process will only delete those services not registered by itself if they are on the `k8s-sync` node (the synthetic node created by the catalog sync process).

* Connect and Mesh Gateways: Consul 1.7+ now requires that we pass `-envoy-version` flag if using a version other than the default (1.13.0) so that it can generate correct bootstrap configuration. This is not yet supported in the Helm chart and consul-k8s, and as such, we require Envoy version 1.13.0.

IMPROVEMENTS:

* Support [**Consul namespaces [Enterprise feature]**](https://www.consul.io/docs/enterprise/namespaces/index.html) in all consul-k8s components [[GH-197](https://github.com/hashicorp/consul-k8s/pull/197)]
* Create allow and deny lists of k8s namespaces for catalog sync and Connect inject
* Connect Inject
  * Changes default Consul Docker image (`-consul-image`) to `consul:1.7.1`
  * Changes default Envoy Docker image (`-envoy-image`) to `envoyproxy/envoy-alpine:v1.13.0`

BUG FIXES:

* Bootstrap ACLs: Allow users to update their Connect ACL binding rule definition on upgrade
* Bootstrap ACLs: Fixes mesh gateway ACL policies to have the correct permissions
* Sync: Fixes a hot loop bug when getting an error from Consul when retrieving service information [[GH-204](https://github.com/hashicorp/consul-k8s/pull/204)]

DEPRECATIONS:
* `connect-inject` flag `-create-inject-token` is deprecated in favor of new flag `-create-inject-auth-method`

NOTES:

* Bootstrap ACLs: Previously, ACL policies were not updated after creation. Now, if namespaces are enabled, they are updated every time the ACL bootstrapper is run so that any namespace config changes can be adjusted. This change is only an issue if you are updating ACL policies after creation.

* Connect: Adds additional parsing of the upstream annotation to support namespaces. The format of the annotation becomes:

    `service_name.optional_namespace:port:optional_datacenter`

  The `service_name.namespace` is only parsed if namespaces are enabled. If they are not enabled and someone has added a `.namespace`, the upstream will not work correctly, as is the case when someone has put in an incorrect service name, port or datacenter. If namespaces are enabled and the `.namespace` is not defined, Consul will automatically fallback to assuming the service is in the same namespace as the service defining the upstream.


## 0.11.0 (January 10, 2020)

Improvements:

* Connect: Add TLS support [[GH-181](https://github.com/hashicorp/consul-k8s/pull/181)].
* Bootstrap ACLs: Add TLS support [[GH-183](https://github.com/hashicorp/consul-k8s/pull/183)].

Notes:

* Build: Our darwin releases for this version and up will be signed and notarized according to Apple's requirements.
Prior to this release, MacOS 10.15+ users attempting to run our software may see the error: "'consul-k8s' cannot be opened because the developer cannot be verified." This error affected all MacOS 10.15+ users who downloaded our software directly via web browsers, and was caused by changes to Apple's third-party software requirements.

  MacOS 10.15+ users should plan to upgrade to 0.11.0+.
* Build: ARM release binaries: Starting with 0.11.0, `consul-k8s` will ship three separate versions of ARM builds. The previous ARM binaries of Consul could potentially crash due to the way the Go runtime manages internal pointers to its Go routine management constructs and how it keeps track of them especially during signal handling (https://github.com/golang/go/issues/32912). From 0.11.0 forward, it is recommended to use:

  consul-k8s\_{version}\_linux_armelv5.zip for all 32-bit armel systems
  consul-k8s\_{version}\_linux_armhfv6.zip for all armhf systems with v6+ architecture
  consul-k8s\_{version}\_linux_arm64.zip for all v8 64-bit architectures
* Build: The `freebsd_arm` variant has been removed.


## 0.10.1 (December 17, 2019)

Bug Fixes:

* Connect: Fix bug where the new lifecycle sidecar didn't have permissions to
  read the ACL token file. [[GH-182](https://github.com/hashicorp/consul-k8s/pull/182)]

## 0.10.0 (December 17, 2019)

Bug Fixes:

* Connect: Fix critical bug where Connect-registered services instances would be deregistered
  when the Consul client on the same node was restarted. This fix adds a new
  sidecar that ensures the service instance is always registered. [[GH-161](https://github.com/hashicorp/consul-k8s/issues/161)]

* Connect: Fix bug where UI links between sidecar and service didn't work because
  the wrong service ID was being used. [[GH-163](https://github.com/hashicorp/consul-k8s/issues/163)]

* Bootstrap ACLs: Support bootstrapACLs for users setting the `nameOverride` config. [[GH-165](https://github.com/hashicorp/consul-k8s/issues/165)]

## 0.9.5 (December 5, 2019)

Bug Fixes:

* Sync: Add Kubernetes namespace as a suffix
  to the service names via `-add-k8s-namespace-suffix` flag.
  This prevents service name collisions in Consul when there
  are two services with the same name in different
  namespaces in Kubernetes [[GH-139](https://github.com/hashicorp/consul-k8s/issues/139)]

* Connect: Only write a `service-defaults` config during Connect injection if
  the protocol is set explicitly [[GH-169](https://github.com/hashicorp/consul-k8s/pull/169)]

## 0.9.4 (October 28, 2019)

Bug Fixes:

* Sync: Now changing the annotation `consul.hashicorp.com/service-sync` to `false`
  or deleting the annotation will un-sync the service. [[GH-76](https://github.com/hashicorp/consul-k8s/issues/76)]

* Sync: Rewrite Consul services to lowercase so they're valid Kubernetes services.
  [[GH-110](https://github.com/hashicorp/consul-k8s/issues/110)]

## 0.9.3 (October 15, 2019)

Bug Fixes:

* Add new delete-completed-job command that is used to delete the
  server-acl-init Kubernetes Job once it's completed. [[GH-152](https://github.com/hashicorp/consul-k8s/pull/152)]

* Fixes a bug where even if the ACL Tokens for the other components existed
  (e.g. client or sync-catalog) we'd try to generate new tokens and update the secrets. [[GH-152](https://github.com/hashicorp/consul-k8s/pull/152)]

## 0.9.2 (October 4, 2019)

Improvements:

* Allow users to set annotations on their Kubernetes services that get synced into
  Consul meta when using the Connect Inject functionality.
  To use, set one or more `consul.hashicorp.com/service-meta-<key>: <value>` annotations
  which will result in Consul meta `<key>: <value>`
  [[GH-141](https://github.com/hashicorp/consul-k8s/pull/141)]

Bug Fixes:

* Fix bug during connect-inject where the `-default-protocol` flag was being
  ignored [[GH-141](https://github.com/hashicorp/consul-k8s/pull/141)]

* Fix bug during connect-inject where service-tag annotations were
  being ignored [[GH-141](https://github.com/hashicorp/consul-k8s/pull/141)]

* Fix bug during `server-acl-init` where if any step errored then the command
  would exit and subsequent commands would fail. Now this command runs until
  completion, i.e. it retries failed steps indefinitely and is idempotent
  [[GH-138](https://github.com/hashicorp/consul-k8s/issues/138)]

Deprecations:

* The `consul.hashicorp.com/connect-service-tags` annotation is deprecated.
  Use `consul.hashicorp.com/service-tags` instead.

## 0.9.1 (September 18, 2019)

Improvements:

* Allow users to set tags on their Kubernetes services that get synced into
  Consul service tags via the `consul.hashicorp.com/connect-service-tags`
  annotation [[GH-115](https://github.com/hashicorp/consul-k8s/pull/115)]

Bug fixes:

* Fix bootstrap acl issue when Consul was installed into a namespace other than `default`
  [[GH-106](https://github.com/hashicorp/consul-k8s/issues/106)]
* Fix sync bug where `ClusterIP` services had their `Service` port instead
  of their `Endpoint` port registered. If the `Service`'s `targetPort` was different
  then `port` then the wrong port would be registered [[GH-132](https://github.com/hashicorp/consul-k8s/issues/132)]


## 0.9.0 (July 8, 2019)

Improvements:

* Allow creation of ACL token for Snapshot Agents
* Allow creation of ACL token for Mesh Gateways
* Allows client ACL token creation to be optional

## 0.8.1 (May 9, 2019)

Bug fixes:

* Fix central configuration write command to handle the case where the service already exists

## 0.8.0 (May 8, 2019)

Improvements:

* Use the endpoint IP address when generating a service id for NodePort services to prevent possible overlap of what are supposed to be unique ids
* Support adding a prefix for Kubernetes -> Consul service sync [[GH 140](https://github.com/hashicorp/consul-helm/issues/140)]
* Support automatic bootstrapping of ACLs in a Consul cluster that is run fully in Kubernetes.
* Support automatic registration of a Kubernetes AuthMethod for use with Connect (available in Consul 1.5+).
* Support central configuration for services, including proxy defaults (available in Consul 1.5+).

Bug fixes:

* Exclude Kubernetes system namespaces from Connect injection

## 0.7.0 (March 21, 2019)

Improvements:

* Use service's namespace when registering endpoints
* Update the Coalesce method to pass go vet tests
* Register Connect services along with the proxy. This allows the services to appear in the intention dropdown in the UI.[[GH 77](https://github.com/hashicorp/consul-helm/issues/77)]
* Add `-log-level` CLI flag for catalog sync

## 0.6.0 (February 22, 2019)

Improvements:

* Add support for prepared queries in the Connect upstream annotation
* Add a health endpoint to the catalog sync process that can be used for Kubernetes health and readiness checks

## 0.5.0 (February 8, 2019)

Improvements:

* Clarify the format of the `consul-write-interval` flag for `consul-k8s` [[GH 61](https://github.com/hashicorp/consul-k8s/issues/61)]
* Add datacenter support to inject annotation
* Update connect injector logging to remove healthcheck log spam and make important messages more visible

Bug fixes:

* Fix service registration naming when using Connect [[GH 36](https://github.com/hashicorp/consul-k8s/issues/36)]
* Fix catalog sync so that agents don't incorrectly deregister Kubernetes services [[GH 40](https://github.com/hashicorp/consul-k8s/issues/40)][[GH 59](https://github.com/hashicorp/consul-k8s/issues/59)]
* Fix performance issue for the k8s -> Consul catalog sync [[GH 60](https://github.com/hashicorp/consul-k8s/issues/60)]

## 0.4.0 (January 11, 2019)
Improvements:

* Supports a configurable tag for the k8s -> Consul sync [[GH 42](https://github.com/hashicorp/consul-k8s/issues/42)]

Bug fixes:

* Register NodePort services with the node's ip address [[GH 8](https://github.com/hashicorp/consul-k8s/issues/8)]
* Add the metadata/annotations field if needed before patching annotations [[GH 20](https://github.com/hashicorp/consul-k8s/issues/20)]

## 0.3.0 (December 7, 2018)
Improvements:

* Support syncing ClusterIP services [[GH 4](https://github.com/hashicorp/consul-k8s/issues/4)]

Bug fixes:

* Allow unnamed container ports to be used in connect-inject default
  annotations.

## 0.2.1 (October 26, 2018)

Bug fixes:

* Fix single direction catalog sync [[GH 7](https://github.com/hashicorp/consul-k8s/issues/7)]

## 0.2.0 (October 10, 2018)

Features:

* **New subcommand: `inject-connect`** runs a mutating admission webhook for
  automatic Connect sidecar injection in Kubernetes. While this can be setup
  manually, we recommend using the Consul helm chart.

## 0.1.0 (September 26, 2018)

* Initial release
