## Unreleased

IMPROVEMENTS:
* Use `consul-k8s` subcommand to perform `tls-init` job. This allows for server certificates to get rotated on subsequent runs.
  Consul servers have to be restarted in order for them to update their server certificates [[GH-749](https://github.com/hashicorp/consul-helm/pull/721)]

## 0.28.0 (Dec 21, 2020)

BREAKING CHANGES:
* Setting `server.bootstrapExpect` to a value less than `server.replicas` will now
  give an error. This was a misconfiguration as the servers wouldn't wait
  until the proper number have started before electing a leader. [[GH-721](https://github.com/hashicorp/consul-helm/pull/721)]
* Clients and servers now run as non root. Users can also configure `server.securityContext` and `client.securityContext`
  if they wish to overwrite this behavior. Please see [Helm reference](https://www.consul.io/docs/k8s/helm) for more information.
  [[GH-748](https://github.com/hashicorp/consul-helm/pull/748)]

FEATURES:
* CRDs: add new CRD `IngressGateway` for configuring Consul's [ingress-gateway](https://www.consul.io/docs/agent/config-entries/ingress-gateway) config entry. [[GH-714](https://github.com/hashicorp/consul-helm/pull/714)]
* CRDs: add new CRD `TerminatingGateway` for configuring Consul's [terminating-gateway](https://www.consul.io/docs/agent/config-entries/terminating-gateway) config entry. [[GH-715](https://github.com/hashicorp/consul-helm/pull/715)]
* Enable client agents outside of the K8s cluster to join a consul datacenter
  without the Pod IPs of the consul servers and clients in K8s needing to be
  routeable. Adds new helm values `server.exposeGossipAndRPCPorts` and
  `server.ports.serflan.port`. To enable external client agents, enable
  `server.exposeGossipAndRPCPorts` and `client.exposeGossipAndPorts`, and set
  `server.ports.serflan.port` to a port not being used on the host, e.g 9301.
  The internal IP of the K8s nodes do need to be routeable from the external
  client agent and the external client agent's IP also needs to be routeable
  from the K8s nodes.
  [[GH-740](https://github.com/hashicorp/consul-helm/pull/740)]

IMPROVEMENTS:
* Updated the default consul-k8s image to `hashicorp/consul-k8s:0.22.0`.
  This release includes an important bug fix where the lifecycle-sidecar sometimes re-registered the application.
  Please see consul-k8s [v0.22.0](https://github.com/hashicorp/consul-k8s/releases/tag/v0.22.0) release for more info.
* Updated the default Consul image to `hashicorp/consul:1.9.1`.
* Make `server.bootstrapExpect` optional. If not set, will now default to `server.replicas`.
  If you're currently setting `server.replicas`, there is no effect. [[GH-721](https://github.com/hashicorp/consul-helm/pull/721)]

BUG FIXES:
* Fix pod security policy when running mesh gateways in `hostNetwork` mode. [[GH-605](https://github.com/hashicorp/consul-helm/issues/605)]
* CRDs: **(Consul Enterprise only)** change `ServiceResolver` field `failover[].namespaces` to `failover[].namespace`.
  This will not affect existing `ServiceResolver` resources and will only update the documentation for that field.
 
  If `failover[].namespaces` was used previously, it was ignored and after this change it will still be ignored.
  If `failover[].namespace` was used previously, it worked correctly and after this change it will still work correctly. [[GH-714](https://github.com/hashicorp/consul-helm/pull/714)]
* Recreate the Server/Client Pod when the Server/Client ConfigMap is updated via `helm upgrade`
  by using Server ConfigMap and Client ConfigMap values as hashes on Server StatefulSet and Client DaemonSet annotations respectively.
  This updates the previously hashed values of the extraConfig. [[GH-550](https://github.com/hashicorp/consul-helm/pull/550)]
* Remove unused ports `8302` and `8300` from the client daemonset pods. [[GH-737](https://github.com/hashicorp/consul-helm/pull/737)]

## 0.27.0 (Nov 25, 2020)

IMPROVEMENTS:
* Connect: support `connectInject.logLevel` setting. [[GH-699](https://github.com/hashicorp/consul-helm/pull/699)]
* Connect: **(Consul Enterprise only)** error out if `connectInject.consulNamespaces.mirroringK8S: true` but `global.enableConsulNamespaces: false`. [[GH-695](https://github.com/hashicorp/consul-helm/pull/695)]
* Updated the default Consul image to `hashicorp/consul:1.9.0`.
* Updated the default consul-k8s image to `hashicorp/consul-k8s:0.21.0`.
* Updated the default envoy image to `envoyproxy/envoy-alpine:v1.16.0`.

## 0.26.0 (Nov 12, 2020)

FEATURES:
* Kubernetes health check synchronization with Consul for connect injected pods via `connectInject.healthChecks` [[GH-651](https://github.com/hashicorp/consul-helm/pull/651)].
  The default behavior for this feature is `enabled: true`.
  See [https://www.consul.io/docs/k8s/connect/health](https://www.consul.io/docs/k8s/connect/health) for more information.
  In order to enable this feature for existing installations it is required to restart all connect injected deployments so that they are re-injected.
  Until this is done, health checks for these deployments will not be synced to Consul.

  **It is recommended to enable TLS with this setting enabled because it requires making calls to Consul clients across the cluster.
    Without TLS enabled, these calls could leak ACL tokens should the cluster network become compromised.**
* Support for custom resource definitions (CRDs) is now generally available.
  CRDs require Consul >= 1.8.4. If you wish to use `ServiceIntentions`
  custom resources then this requires Consul >= 1.9.0 (which is still in beta as of this release).

  To enable, set `controller.enabled: true` in your Helm configuration:

  ```yaml
  controller:
    enabled: true
  ```

  See [https://www.consul.io/docs/k8s/crds](https://www.consul.io/docs/k8s/crds)
  for more information. **NOTE:** Using CRDs with an existing cluster may require additional steps to migrate previously created
  config entries so they can be managed by CRDs. See [https://www.consul.io/docs/k8s/crds/upgrade-to-crds](https://www.consul.io/docs/k8s/crds/upgrade-to-crds)
  for full details.

BREAKING CHANGES:
* This helm release only supports consul-k8s versions 0.20+
* With the addition of the connect-inject health checks controller, any connect services which have failing Kubernetes readiness
  probes will no longer be routable through connect until their Kubernetes health probes are passing.
  Previously, if any connect services were failing their Kubernetes readiness checks they were still routable through connect.
  Users should verify that their connect services are passing Kubernetes readiness probes prior to using health checks synchronization.
* When health checks are enabled, Consul clients will have `check_update_interval` set to `0s`. Previously,
  it was set to its default of `5m`. This change ensures the output of the check will show up in the Consul UI immediately. [[GH-674](https://github.com/hashicorp/consul-helm/pull/674)]
* CRDs: controller default `limits.memory` increased from `30Mi` to `50Mi` and `requests.memory` increased from `20Mi` to `50Mi`
  based on observed usage. [[GH-649](https://github.com/hashicorp/consul-helm/pull/649)]

BUG FIXES:
* Fix issue where Consul enterprise license job would fail for Consul versions >= 1.8.1. [[GH-647](https://github.com/hashicorp/consul-helm/issues/647)]

IMPROVEMENTS:
* Connect: support passing extra arguments to the injected envoy sidecar. [[GH-675](https://github.com/hashicorp/consul-helm/pull/675)]

  To pass extra arguments to envoy, set `connectInject.envoyExtraArgs` in your
  Helm configuration:

  ```yaml
  connectInject:
    enabled: true
    envoyExtraArgs: "--log-level debug --disable-hot-restart"
  ```
* Connect: update MutatingWebhook resource version to `admissionregistration.k8s.io/v1` from `admissionregistration.k8s.io/v1beta1`
  for clusters where it is supported. [[GH-658](https://github.com/hashicorp/consul-helm/pull/658)]
* Updated the default Consul image to `consul:1.8.5`.
* Updated the default consul-k8s image to `hashicorp/consul-k8s:0.20.0`.

## 0.25.0 (Oct 12, 2020)

FEATURES:

* Support deploying this Helm chart to OpenShift 4.x. [[GH-600](https://github.com/hashicorp/consul-helm/pull/600)]

  To install on OpenShift, set `global.openshift.enabled` to `true`:

  ```sh
  helm install consul hashicorp/consul \
    --set global.name=consul \
    --set global.openshift.enabled=true
  ```

* Beta support for custom resource definitions. [[GH-636](https://github.com/hashicorp/consul-helm/pull/636)]

  **Requires Consul >= 1.8.4.**
  
  The currently supported CRDs can be used to manage Consul's [Configuration Entries](https://www.consul.io/docs/agent/config-entries),
  specifically:
    * `ProxyDefaults` - https://www.consul.io/docs/agent/config-entries/proxy-defaults
    * `ServiceDefaults` - https://www.consul.io/docs/agent/config-entries/service-defaults
    * `ServiceSplitter` - https://www.consul.io/docs/agent/config-entries/service-splitter
    * `ServiceRouter` - https://www.consul.io/docs/agent/config-entries/service-router
    * `ServiceResolver` - https://www.consul.io/docs/agent/config-entries/service-resolver
    * `ServiceIntentions` (requires Consul >= 1.9.0) - https://www.consul.io/docs/agent/config-entries/service-intentions

  An example use looks like:

  ```yaml
  apiVersion: consul.hashicorp.com/v1alpha1
  kind: ServiceDefaults
  metadata:
    name: defaults
  spec:
    protocol: "http"
  ```

  See [https://www.consul.io/docs/k8s/crds](https://www.consul.io/docs/k8s/crds)
  for more information on the CRD schemas.

  To enable, set `controller.enabled: true` in your Helm configuration:

  ```yaml
  controller:
    enabled: true
  ```

  This will install the CRDs, the controller that watches for CR creation, and
  a webhook certificate manager that manages the certificates for the controller's
  webhooks.

* Add acceptance test framework and automated acceptance tests to the Helm chart.
  Please see Contributing docs for more info on how to [run](https://github.com/hashicorp/consul-helm/blob/master/CONTRIBUTING.md#acceptance-tests)
  and [add](https://github.com/hashicorp/consul-helm/blob/master/CONTRIBUTING.md#writing-acceptance-tests) acceptance tests. [[GH-551](https://github.com/hashicorp/consul-helm/pull/551)]

IMPROVEMENTS:

* Add `dns.type` and `dns.additionalSpec` settings for changing the DNS service type and adding additional spec. [[GH-555](https://github.com/hashicorp/consul-helm/pull/555)]
* Catalog Sync: Can now be run when Consul clients are disabled. It will make API calls to the Consul servers instead. [[GH-570](https://github.com/hashicorp/consul-helm/pull/570)]
* Catalog Sync: Add support for changing the Consul node name where services are sync'd. [[GH-580](https://github.com/hashicorp/consul-helm/pull/580)]
* Support for setting `priorityClassName` for sync-catalog and connect-inject deployments. [[GH-609](https://github.com/hashicorp/consul-helm/pull/609)]
* Updated the default Consul image to `consul:1.8.4`.
* Updated the default Envoy image to `envoyproxy/envoy-alpine:v1.14.4`.

BREAKING CHANGES:
* `connectInject.imageEnvoy` and `meshGateway.imageEnvoy` have been removed and now inherit from `global.imageEnvoy`
  which is now standardized across terminating/ingress/mesh gateways and connectInject.
  `global.imageEnvoy` is now a required parameter. [GH-585](https://github.com/hashicorp/consul-helm/pull/585)

## 0.24.1 (Aug 10, 2020)

BUG FIXES:

* Bumps default Consul version to `1.8.2`. This version of Consul contains a fix
  for [https://github.com/hashicorp/consul/issues/8430](https://github.com/hashicorp/consul/issues/8430)
  which causes Consul clients running on the same node as a connect-injected pod
  to crash loop indefinitely when restarted.

* Bumps default consul-k8s version to `0.18.1`. This version contains a fix
  for an issue that caused all connect-injected pods to be unhealthy for 60s
  if they were restarted. To roll out this fix, all Connect deployments must
  be restarted so that they are re-injected.

## 0.24.0 (July 31, 2020)

IMPROVEMENTS:

* Add server.extraConfig and client.extraConfig values as hashes on Server
  StatefulSet and Client Daemonset annotations respectively. This recreates
  the server/client pod when the server/client extraConfig is updated via `helm upgrade` [[GH-550](https://github.com/hashicorp/consul-helm/pull/550)]

* Introduce field `server.extraLabels` to append additional labels to consul server pods. [[GH-553](https://github.com/hashicorp/consul-helm/pull/553)]

* Introduce field `server.disableFsGroupSecurityContext` which disables setting the fsGroup securityContext on the server statefulset.
  This enables deploying on platforms where the fsGroup is automatically set to an arbitrary gid. (eg OpenShift) [[GH-528](https://github.com/hashicorp/consul-helm/pull/528)]

* Connect: Resource settings for Connect, mesh, ingress and terminating gateway init containers and lifecycle sidecars have been made configurable. The default values correspond to the previously set limits, except that the lifecycle sidecar memory limit has been increased to `50Mi` [[GH-556](https://github.com/hashicorp/consul-helm/pull/556)]. These new fields are:
  * `global.lifecycleSidecarContainer.resources` - Configures the resource settings for all lifecycle sidecar containers used with Connect inject, mesh gateways, ingress gateways and terminating gateways.
  * `connectInject.initContainer.resources` - Configures resource settings for the Connect-injected init container.
  * `meshGateway.initCopyConsulContainer.resources` - Configures the resource settings for the `copy-consul-bin` init container for mesh gateways.
  * `ingressGateways.defaults.initCopyConsulContainer.resources` - Configures the resource settings for the `copy-consul-bin` init container for ingress gateways. Defaults can be overridden per ingress gateway.
  * `terminatingGateways.defaults.initCopyConsulContainer.resources` - Configures the resource settings for the `copy-consul-bin` init container for terminating gateways. Defaults can be overridden per terminating gateway.

* Updated the default consul version to 1.8.1.

BREAKING CHANGES:

* Updating either server.extraConfig or client.extraConfig and running `helm upgrade` will force a restart of the
  server or agent pods respectively.

## 0.23.1 (July 10, 2020)

BUG FIXES:

* TLS: Fixes bug introduced in 0.23.0 where the DNS subject alternative names
  for the server certs were invalid. This would cause the server-acl-init job
  to run forever without completing. [[GH-538](https://github.com/hashicorp/consul-helm/pull/538)]

## 0.23.0 (July 9, 2020)

BREAKING CHANGES:

* Connect: Resource limits have been set for ingress and terminating gateway containers and
  bumped up for mesh gateways. See deployment definitions for new resource settings. [[GH-533](https://github.com/hashicorp/consul-helm/pull/533), [GH-534](https://github.com/hashicorp/consul-helm/pull/534)]

IMPROVEMENTS:

* Default version of `consul-k8s` has been set to `hashicorp/consul-k8s:0.17.0`.
* ClusterRoles and ClusterRoleBindings have been converted to Roles and RoleBindings
  for the following components because they only required access within their namespace:
  * Enterprise License Job
  * Server ACL Init
  * Server Statefulset
  * Client Daemonset
  * Client Snapshot Agent

   [[GH-403](https://github.com/hashicorp/consul-helm/issues/403)]

* The volumes set by `client.extraVolumes` are now passed as the last `-config-dir` argument.
  This means any settings there will override previous settings. This allows users to override
  settings that Helm is setting automatically, for example the acl down policy. [[GH-531](https://github.com/hashicorp/consul-helm/pull/531)]

BUG FIXES:

* Connect: Resource settings for mesh, ingress and terminating gateway init containers
 lifecycle sidecar containers have been changed to avoid out of memory errors and hitting CPU limits. [[GH-515](https://github.com/hashicorp/consul-helm/issues/515)]
     * `copy-consul-bin` has its memory limit set to `150M` up from `25M`
     * `lifecycle-sidecar` has its CPU request and limit set to `20m` up from `10m`.

## 0.22.0 (June 18, 2020)

FEATURES:

* Supports deploying Consul [Ingress](https://www.consul.io/docs/connect/ingress_gateway)
  and [Terminating](https://www.consul.io/docs/connect/terminating_gateway) Gateways.
  Multiple different gateways of each type can be deployed with default values that can
  be overridden for specific gateways if desired. Full documentation of the configuration
  options can be found in the values file or in the Helm chart documentation
  ([Ingress](https://www.consul.io/docs/k8s/helm#v-ingressgateways),
  [Terminating](https://www.consul.io/docs/k8s/helm#v-terminatinggateways)).
  Requires Consul 1.8.0+.

  Ingress gateways: [[GH-456](https://github.com/hashicorp/consul-helm/pull/456)], 
  Terminating gateways: [[GH-503](https://github.com/hashicorp/consul-helm/pull/503)]

* Resources are now set on all containers. This enables the chart to be deployed
  in clusters that have resource quotas set. This also ensures that Consul
  server and client pods won't be evicted by Kubernetes when nodes reach their
  resource limits.
  
  Resource settings have been made configurable for sync catalog, connect inject
  and client snapshot deployments and sidecar proxies. [[GH-470](https://github.com/hashicorp/consul-helm/pull/470)]
  
  The default settings were chosen based on a cluster with a small workload.
  For production, we recommend monitoring resource usage and modifying the
  defaults according to your usage. [[GH-466](https://github.com/hashicorp/consul-helm/pull/466)]

BREAKING CHANGES:

* If upgrading to Consul 1.8.0 and using Consul Connect, you will need to upgrade consul-k8s to 0.16.0 (by setting `global.imageK8S: hashicorp/consul-k8s:0.16.0`) and re-roll your Connect pods so they get re-injected, before upgrading consul. This is required because we were previously setting a health check incorrectly that now fails on Consul 1.8.0. If you upgrade to 1.8.0 without upgrading to consul-k8s 0.16.0 and re-rolling your connect pods first, the connect pods will fail their health checks and no traffic will be routed to them.

* It is recommended to use the helm repository to install the helm chart instead of cloning this repo directly. Starting with this release
 the master branch may contain breaking changes.

  ```sh
    $ helm repo add hashicorp https://helm.releases.hashicorp.com
    $ helm install consul hashicorp/consul --set global.name=consul
  ```

* Mesh Gateway: `meshGateway.enableHealthChecks` is no longer supported. This config
  option was to work around an issue where mesh gateways would not listen on their
  bind ports until a Connect service was registered. This issue was fixed in Consul 1.6.2. ([GH-464](https://github.com/hashicorp/consul-helm/pull/464))

* Mesh Gateway: The default resource settings have been changed. To keep
  the previous settings, you must set `meshGateway.resources` in your own Helm config. ([GH-466](https://github.com/hashicorp/consul-helm/pull/466))

  Before:
  ```yaml
  meshGateway:
    resources:
      requests:
        memory: "128Mi"
        cpu: "250m"
      limits:
        memory: "256Mi"
        cpu: "500m"
  ```

  After:
  ```yaml
  meshGateway:
    resources:
      requests:
        memory: "100Mi"
        cpu: "100m"
      limits:
        memory: "100Mi"
        cpu: "100m"
  ```

* Clients and Servers: There are now default resource settings for Consul clients
   and servers. Previously, there were no default settings which meant the default
   was unlimited. This change was made because Kubernetes will prefer to evict
   pods that don't have resource settings and that resulted in the Consul client
   and servers being evicted. The default resource settings were chosen based
   on a low-usage cluster. If you are running a production cluster, use the
   `kubectl top` command to see how much CPU and memory your clients and servers
   are using and set the resources accordingly [[GH-466](https://github.com/hashicorp/consul-helm/pull/466)].
* `global.bootstrapACLs` has been removed, use `global.acls.manageSystemACLs` instead [[GH-501](https://github.com/hashicorp/consul-helm/pull/501)].

IMPROVEMENTS:

* Add component label to the server, DNS, and UI services [[GH-480](https://github.com/hashicorp/consul-helm/pull/480)].
* Provide the ability to set a custom CA Cert for consul snapshot agent [[GH-481](https://github.com/hashicorp/consul-helm/pull/481)].
* Add support for client host networking [[GH-496](https://github.com/hashicorp/consul-helm/pull/496)].

  To enable:
  ```yaml
  client:
    hostNetwork: true
    dnsPolicy: ClusterFirstWithHostNet
  ```
* Add ability to set Affinity and Tolerations to Connect Inject and Catalog Sync [[GH-335](https://github.com/hashicorp/consul-helm/pull/335)].
* Updated the default consul-k8s version to 0.16.0.
* Updated the default consul version to 1.8.0.
* Update default Envoy image version and OS to `envoyproxy/envoy-alpine:1.14.2` [[GH-502](https://github.com/hashicorp/consul-helm/pull/502)].

DEPRECATIONS

* Setting resources via YAML string is now deprecated. Instead, set directly as YAML.
  This affects `client.resources`, `server.resources` and `meshGateway.resources`.
  To set directly as YAML, simply remove the pipe (`|`) character that defines
  the YAML as a string [[GH-465](https://github.com/hashicorp/consul-helm/pull/465)]: 
  
  Before:
  ```yaml
  client:
    resources: |
      requests:
        memory: "128Mi"
        cpu: "250m"
      limits:
        memory: "256Mi"
        cpu: "500m"
  ```
  
  After:
  ```yaml
  client:
    resources:
      requests:
        memory: "128Mi"
        cpu: "250m"
      limits:
        memory: "256Mi"
        cpu: "500m"
  ```

## 0.21.0 (May 14, 2020)

FEATURES

* Add experimental support for multi-datacenter federation via

    ```yaml
    global:
      federation:
        enabled: true
    ```
  
  This requires Consul 1.8.0+ (which as of this release is only available as
  a beta. To use the beta, set `global.image: consul:1.8.0-beta1`)

* Add new Helm value `global.federation.createFederationSecret` that will
  create a Kubernetes secret in primary datacenters that can be exported to secondary
  datacenters to help bootstrap secondary clusters for federation ([GH-447](https://github.com/hashicorp/consul-helm/pull/447)).

IMPROVEMENTS

* Default Consul Docker image is now `consul:1.7.3`.
* Default consul-k8s Docker image is now `hashicorp/consul-k8s:0.15.0`.
* ACLs: Restrict permissions for the `server-acl-init` job [[GH-454](https://github.com/hashicorp/consul-helm/pull/454)].

BUG FIXES

* Fix missing `NODE_NAME` environment variable when setting `meshGateway.wanAddress.source=NodeName`
  [[GH-453](https://github.com/hashicorp/consul-helm/pull/453)].

## 0.20.1 (Apr 27, 2020)

BUG FIXES

* Fix a bug where `client.join` and `externalServers.hosts` values containing spaces are
  not quoted properly, for example, when providing [cloud auto-join](https://www.consul.io/docs/agent/cloud-auto-join.html) strings
  [[GH-435](https://github.com/hashicorp/consul-helm/pull/435)].

## 0.20.0 (Apr 24, 2020)

BREAKING CHANGES:

* External Servers [[GH-430](https://github.com/hashicorp/consul-helm/pull/430)]:
  * `externalServers.https.address` moved to `externalServers.hosts`
    and changed its type from `string` to `array`.
  * `externalServers.https.port` moved to `externalServers.httpsPort`
    and its default value changed from `443` to `8501`.
  * `externalServers.https.tlsServerName` moved to `externalServers.tlsServerName`.
  * `externalServers.https.useSystemRoots` moved to `externalServers.useSystemRoots`.

  For example, if previously setting `externalServers` like so:

    ```yaml
    externalServers:
      enabled: true
      https:
        address: "example.com"
        port: 443
        tlsServerName: null
        useSystemRoots: false
    ```

  Now you need to change it to the following:

    ```yaml
    externalServers:
      enabled: true
      hosts: ["example.com"]
      httpsPort: 443
      tlsServerName: null
      useSystemRoots: false
    ```

* Auto-encrypt: You can no longer re-use `client.join` property if using auto-encrypt
  with `externalServers.enabled` set to `true`. You must provide Consul server HTTPS address
  via `externalServers.hosts` and `externalServers.httpsPort`.

  For example, if previously setting:

    ```yaml
    tls:
      enabled: true
      enabledAutoEncrypt: true
    externalServers:
      enabled: true
    client:
      join: ["consul.example.com"]
    ``` 

  Now you need to change it to:

  ```yaml
    tls:
      enabled: true
      enabledAutoEncrypt: true
    externalServers:
      enabled: true
      hosts: ["consul.example.com"]
    client:
      join: ["consul.example.com"]
    ``` 

FEATURES:

* Support managing ACLs when running Consul servers externally to Kubernetes:

    * ACLs: Support providing your own bootstrap token [[GH-420](https://github.com/hashicorp/consul-helm/pull/420)].
      If provided, the `server-acl-init` job will skip server ACL bootstrapping.

      Example:

        ```yaml
        global:
          acls:
            manageSystemACLs: true
            bootstrapToken:
              secretName: bootstrap-token
              secretKey: token
        ```

    * External Servers: Add `externalServers.k8sAuthMethodHost` to allow configuring a custom location
      of the Kubernetes API server for the auth method created in Consul [[GH-420](https://github.com/hashicorp/consul-helm/pull/420)].
      The Kubernetes API server provided here must be reachable from the external Consul servers.

      Example:

        ```yaml
        externalServers:
          enabled: true
          k8sAuthMethodHost: https://kubernetes-api.example.com:443
        ```

IMPROVEMENTS:

* Default to the latest version of consul-k8s: hashicorp/consul-k8s:0.14.0

BUG FIXES:

* `tls-init-cleanup` can run even if pre-install fails [[GH-419](https://github.com/hashicorp/consul-helm/pull/419)].

## 0.19.0 (Apr 7, 2020)

BREAKING CHANGES:

* Mesh Gateways:
  * `meshGateway.wanAddress` - The following values are no longer supported:
  
       ```yaml
       meshGateway:
         wanAddress:
           useNodeIP: true
           useNodeName: false
           host: ""
       ```
    
    Instead, if previously setting `useNodeIP: true`, now you must set:
       ```yaml
       meshGateway:
         wanAddress:
           source: "NodeIP"
       ```
    
    If previously setting `useNodeName: true`, now you must set:
       ```yaml
       meshGateway:
         wanAddress:
           source: "NodeName"
       ```
    
    If previously setting `host: "example.com"`, now you must set:
       ```yaml
       meshGateway:
         wanAddress:
           source: "Static"
           static: "example.com"
       ```
    where `meshGateway.wanAddress.static` is set to the previous `host` value.
  
  * `meshGateway.service.enabled` now defaults to `true`. If
    previously you were enabling mesh gateways but not enabling the service,
    you must now explicitly set this to `false`:
    
    Previously:
    ```yaml
    meshGateway:
      enabled: true
    ```
    
    Now:
    ```yaml
    meshGateway:
      enabled: true
      service:
        enabled: false
    ```
    
  * `meshGateway.service.type` now defaults to `LoadBalancer` instead of `ClusterIP`.
    To set to `ClusterIP` use:
    ```yaml
    meshGateway:
      service:
        type: ClusterIP
    ```

  * `meshGateway.containerPort` now defaults to `8443` instead of `443`. This is
    to support running in Google Kubernetes Engine by default. This change should
    have no effect because the service's targetPort will change accordingly so
    you will still be able to route to the mesh gateway as before.
    If you wish to keep the port as `443` you must set:
    ```yaml
    meshGateway:
      containerPort: 443
    ```

FEATURES:

* Add `externalServers` configuration to support configuring the Helm chart with Consul servers
  running outside of a Kubernetes cluster [[GH-375](https://github.com/hashicorp/consul-helm/pull/375)]. At the moment, this configuration is only used together
  with auto-encrypt, but might be extended later for other use-cases.

  To use auto-encrypt with external servers, you can set:
  ```yaml
  externalServers:
    enabled: true
  ```
  This will tell all consul-k8s components to talk to the external servers to retrieve
  the clients' CA. Take a look at other properties you can set for `externalServers`
  [here](https://github.com/hashicorp/consul-helm/blob/e892588288c5c14197306cc714aabb2473f6f59e/values.yaml#L273-L305).

* ACLs: Support ACL replication. ACL replication allows two or more Consul clusters
  to be federated when ACLs are enabled. One cluster is designated the primary
  and the rest are secondaries. The primary cluster replicates its ACLs to
  the secondaries. [[GH-368](https://github.com/hashicorp/consul-helm/pull/368)]
  
  NOTE: This feature requires that the clusters are federated.
  
  Primary cluster:
  
  ```yaml
  global:
    acls:
      manageSystemACLs: true
      createReplicationToken: true
  ```

  The replication acl token Kubernetes secret is exported from the primary cluster
  into the secondaries and then referenced in their Helm config:
  
  ```yaml
  global:
    acls:
      manageSystemACLs: true
      replicationToken:
        secretName: name
        secretKey: key
  ```

* Mesh Gateways: Automatically set mesh gateway addresses when using a Kubernetes
  Load Balancer service. 
  To use, set:
  
  ```yaml
  meshGateway:
    enabled: true
    service:
      enabled: true
      type: "LoadBalancer"
    wanAddress:
      source: "Service"
  ```
  [[GH-388](https://github.com/hashicorp/consul-helm/pull/388)]

* Support setting image pull secrets via service accounts [[GH-411](https://github.com/hashicorp/consul-helm/pull/411)].

IMPROVEMENTS:

* Default to the latest version of consul-k8s: `hashicorp/consul-k8s:0.13.0`
* Default to the latest version of Consul: `consul:1.7.2`
* Allow setting specific secret keys in `server.extraVolumes` [[GH-395](https://github.com/hashicorp/consul-helm/pull/395)]
* Support auto-encrypt [[GH-375](https://github.com/hashicorp/consul-helm/pull/375)].
  Auto-encrypt is the feature of Consul that allows clients to bootstrap their own certs
  at startup. To enable it through the Helm Chart, set:
  ```yaml
  global:
    tls:
      enabled: true
      enableAutoEncrypt: true
  ```
* Run the enterprise license job on Helm upgrades, as well as installs [[GH-407](https://github.com/hashicorp/consul-helm/pull/407)].

BUGFIXES:

* Mesh Gateways: Mesh gateways are no longer de-registered when their node's Consul
  client restarts. [[GH-380](https://github.com/hashicorp/consul-helm/pull/380)]

DEPRECATIONS:

* `global.bootstrapACLs` is deprecated. Instead, set `global.acls.manageSystemACLs`.
   `global.bootstrapACLs` will be supported for the next three releases.

   Previously:
   ```yaml
   global:
     bootstrapACLs: true
   ```

   Now:
   ```yaml
   global:
     acls:
       manageSystemACLs: true
   ```

## 0.18.0 (Mar 18, 2020)

IMPROVEMENTS:

* Allow setting your own certificate authority for Consul to Consul communication
(i.e. not Connect service to service communication) [[GH-346](https://github.com/hashicorp/consul-helm/pull/346)].
  To use, set:
  ```yaml
  global:
    tls:
      caCert:
        secretName: null
        secretKey: null
      caKey:
        secretName: null
        secretKey: null
  ```
  See `values.yaml` for more details.
* Allow setting custom annotations for Consul server service [[GH-376](https://github.com/hashicorp/consul-helm/pull/376)]
  To use, set:
  ```yaml
  server:
    service:
      annotations: |
        "annotation-key": "annotation-value"
  ```

BUG FIXES:

* Fix incompatibility with Helm 3.1.2. [[GH-390](https://github.com/hashicorp/consul-helm/issues/390)]
* Ensure the Consul Enterprise license gets applied, even if servers take a long time to come up. [[GH-348](https://github.com/hashicorp/consul-helm/pull/348))

## 0.17.0 (Feb 21, 2020)

BREAKING CHANGES:

* `consul-k8s` `v0.12.0`+ is now required. The chart is passing new flags that are only available in this version.
  To use this version if not using the chart defaults, set
  ```yaml
  global:
    imageK8S: hashicorp/consul-k8s:0.12.0
  ```

IMPROVEMENTS:

* Catalog Sync
  * New Helm values have been added to configure which Kubernetes namespaces we will sync from. The defaults are shown below:
    ```yaml
    syncCatalog:
      toConsul: true
      k8sAllowNamespaces: ["*"]
      k8sDenyNamespaces: ["kube-system", "kube-public"]
    ```
  * If running Consul Enterprise 1.7.0+, Consul namespaces are supported. New Helm values have been added to allow configuring which
    Consul namespaces Kubernetes services are synced to. See [https://www.consul.io/docs/platform/k8s/service-sync.html#consul-enterprise-namespaces](https://www.consul.io/docs/platform/k8s/service-sync.html#consul-enterprise-namespaces) for more details.

    ```yaml
    global:
      enableConsulNamespaces: true
    syncCatalog:
      consulNamespaces:
        # consulDestinationNamespace is the name of the Consul namespace to register all
        # k8s services into. If the Consul namespace does not already exist,
        # it will be created. This will be ignored if `mirroringK8S` is true.
        consulDestinationNamespace: "default"

        # mirroringK8S causes k8s services to be registered into a Consul namespace
        # of the same name as their k8s namespace, optionally prefixed if
        # `mirroringK8SPrefix` is set below. If the Consul namespace does not
        # already exist, it will be created. Turning this on overrides the
        # `consulDestinationNamespace` setting.
        # `addK8SNamespaceSuffix` may no longer be needed if enabling this option.
        mirroringK8S: false

        # If `mirroringK8S` is set to true, `mirroringK8SPrefix` allows each Consul namespace
        # to be given a prefix. For example, if `mirroringK8SPrefix` is set to "k8s-", a
        # service in the k8s `staging` namespace will be registered into the
        # `k8s-staging` Consul namespace.
        mirroringK8SPrefix: ""
    ```

* Connect Inject
  * New Helm values have been added to configure which Kubernetes namespaces we will inject pods in. The defaults are shown below:
    ```yaml
    connectInject:
      k8sAllowNamespaces: ["*"]
      k8sDenyNamespaces: []
    ```
  * If running Consul Enterprise 1.7.0+, Consul namespaces are supported. New Helm values have been added to allow configuring which Consul namespaces Kubernetes pods
    are registered into. See [https://www.consul.io/docs/platform/k8s/connect.html#consul-enterprise-namespaces](https://www.consul.io/docs/platform/k8s/connect.html#consul-enterprise-namespaces) for more details.
    ```yaml
    global:
      enableConsulNamespaces: true

    connectInject:
      consulNamespaces:
        # consulDestinationNamespace is the name of the Consul namespace to register all
        # k8s pods into. If the Consul namespace does not already exist,
        # it will be created. This will be ignored if `mirroringK8S` is true.
        consulDestinationNamespace: "default"

        # mirroringK8S causes k8s pods to be registered into a Consul namespace
        # of the same name as their k8s namespace, optionally prefixed if
        # `mirroringK8SPrefix` is set below. If the Consul namespace does not
        # already exist, it will be created. Turning this on overrides the
        # `consulDestinationNamespace` setting.
        mirroringK8S: false

        # If `mirroringK8S` is set to true, `mirroringK8SPrefix` allows each Consul namespace
        # to be given a prefix. For example, if `mirroringK8SPrefix` is set to "k8s-", a
        # pod in the k8s `staging` namespace will be registered into the
        # `k8s-staging` Consul namespace.
        mirroringK8SPrefix: ""
    ```

BUG FIXES:

* Fix template rendering bug when setting `connectInject.overrideAuthMethodName` [[GH-342](https://github.com/hashicorp/consul-helm/pull/342)]
* Set `"consul.hashicorp.com/connect-inject": "false"` annotation on enterprise license job so it is not connect injected [[GH-343](https://github.com/hashicorp/consul-helm/pull/343)]

DEPRECATIONS:

* `.syncCatalog.k8sSourceNamespace` should no longer be used. Instead, use the new `.syncCatalog.k8sAllowNamespaces` and `.syncCatalog.k8sDenyNamespaces` features. For backward compatibility, if both this and the allow/deny lists are set, the allow/deny lists will be ignored.

NOTES:

* Bootstrap ACLs: Previously, ACL policies were not updated after creation. Now, if namespaces are enabled, they are updated every time the ACL bootstrapper is run so that any namespace config changes can be adjusted. This change is only an issue if you are updating ACL policies after creation.

## 0.16.2 (Jan 15, 2020)

BUG FIXES:

  * Fix Helm Chart version.

## 0.16.1 (Jan 14, 2020)

BUG FIXES:

  * Fix a bug with the `tls-init` job, in which it could not correctly detect CA file
    if Consul domain is provided [[GH-329](https://github.com/hashicorp/consul-helm/pull/329)].

## 0.16.0 (Jan 10, 2020)

IMPROVEMENTS:

  * Optionally allow enabling TLS for Consul communication [[GH-313](https://github.com/hashicorp/consul-helm/pull/313)].
    If `global.tls.enabled` is set to `true`, the Helm chart will generate a CA and necessary certificates and
    enable TLS for servers, clients, Connect injector, Mesh gateways, catalog sync, ACL bootstrapping, and snapshot agents.

    Note that this feature is only supported if both servers and clients are running
    on Kubernetes. We will have better support for other deployment architectures,
    as well as bringing your own CA, in the future.

    Also, note that simply turning on this feature and running `helm upgrade` will result in downtime if you are using
    Consul Connect or Sync Catalog features. We will be adding instructions on how to do this upgrade without downtime soon.
    Additionally, if you do decide to proceed with an upgrade despite downtime
    and you're using Consul Connect, all application pods need to be recreated after upgrade, so that the Connect injector
    can re-inject Envoy sidecars with TLS enabled.

  * Use the latest version of consul-k8s (0.11.0).

  * Add pod name as metadata to client nodes to help users map nodes in Consul to underlying client pods
    [[GH-315](https://github.com/hashicorp/consul-helm/pull/315)].

  * Rename `enterprise-licence.yaml` template to `enterprise-license-job.yaml` [[GH-321](https://github.com/hashicorp/consul-helm/pull/321)].

BUG FIXES:

  * Fix graceful termination for servers [[GH-313](https://github.com/hashicorp/consul-helm/pull/313)].
    `terminationGracePeriod` is now set to 30 seconds for the servers. The previous setting of 10 seconds
    wasn't always enough time for a graceful leave, and in those cases, servers leave the cluster
    in a "failed" state. Additionally, clients always set `leave_on_terminate` to `true`.
    This replaces the `preStop` hook that was calling `consul leave`. Note that `leave_on_terminate` defaults
    to true for clients as of Consul `0.7`, so this change only affects earlier versions.

  * Helm test runner now respects the provided namespace [[GH-320](https://github.com/hashicorp/consul-helm/pull/320)].

  * Add pod security policies for the `enterprise-license` [[GH-325](https://github.com/hashicorp/consul-helm/pull/325)]
    and the `server-acl-init` jobs [[GH-326](https://github.com/hashicorp/consul-helm/pull/325)].

## 0.15.0 (Dec 17, 2019)

BREAKING CHANGES:

  * `connectInject.centralConfig` defaults to `true` now instead of `false`. This is to make it
     easier to configure Connect via `service-defaults` and other routing
     config [[GH-302](https://github.com/hashicorp/consul-helm/pull/302)].
     See https://www.consul.io/docs/agent/options.html#enable_central_service_config.

     If you wish to disable central config, set `connectInject.centralConfig` to
     false in your local values file. NOTE: If `connectInject.enabled` is false,
     then central config is not enabled so this change will not affect you. 
  
  * Connect Inject: If using Connect Inject, you must also upgrade your `consul-k8s` version
    to a version >= 0.10.1. A new flag is being passed in to `consul-k8s` which is not
    supported in earlier versions.

BUG FIXES:
  * Fix bug with `fullnameOverride` and add new `global.name` setting for changing
    the default prefix for resources. [[GH-286](https://github.com/hashicorp/consul-helm/issues/286)]

  * Connect Inject: Fix critical bug where Connect-registered services instances would be de-registered
    when the Consul client on the same node was restarted. This fix adds a new
    sidecar that ensures the service instance is always registered. [[GH-314](https://github.com/hashicorp/consul-helm/pull/314)]

## 0.14.0 (Dec 10, 2019)

IMPROVEMENTS:

  * Consul client DaemonSet can now use a [hostPath mount](https://kubernetes.io/docs/concepts/storage/volumes/#hostpath)
    for its data directory by setting the `client.dataDirectoryHostPath` value.
    This setting is currently necessary to ensure that when a Consul client Pod is deleted,
    e.g. during a Consul version upgrade, it does not lose its Connect service
    registrations. In the next version, we plan to have services automatically
    re-register which will remove the need for this. [[GH-298](https://github.com/hashicorp/consul-helm/pull/298)]
    (**Update:** 0.15.0 uses a version of consul-k8s that fixes this bug and so hostPath is longer necessary)
    
    **Security Warning:** If using this setting, Pod Security Policies *must* be enabled on your cluster
     and in this Helm chart (via the `global.enablePodSecurityPolicies` setting)
     to prevent other Pods from mounting the same host path and gaining
     access to all of Consul's data. Consul's data is not encrypted at rest.

  * New configuration option `client.updateStrategy` allows setting the update
    strategy for the Client DaemonSet. [[GH-298](https://github.com/hashicorp/consul-helm/pull/298)]

  * New configuration option `client.dnsPolicy` allows setting the DNS
    policy for the Client DaemonSet. [[GH-298](https://github.com/hashicorp/consul-helm/pull/298)]

## 0.13.0 (Dec 5, 2019)

BREAKING CHANGES:

  * `client.grpc` defaults to `true` now instead of `false`. This is to make it
    harder to misconfigure Connect. [[GH-282](https://github.com/hashicorp/consul-helm/pull/282)]
     
    If you do not wish to enable gRPC for clients, set `client.grpc` to
    `false` in your local values file.

  * Add `syncCatalog.addK8SNamespaceSuffix` and default it to `true`. [[GH-280](https://github.com/hashicorp/consul-helm/pull/280)]
    Note: upgrading an existing installation will result in deregistering
    of existing synced services in Consul and registering them with a new name.
    If you would like to avoid this behavior set `syncCatalog.addK8SNamespaceSuffix`
    to `false`.
    
    This changes the default service names registered from Kubernetes into Consul. Previously, we would register all Kubernetes services, regardless of namespace, as the same service in Consul. After this change, the default behaviour is to append the Kubernetes namespace to the Consul service name. For example, given a Kubernetes service `foo` in the namespace `namespace`, it would be registered in Consul as `foo-namespace`. The name can also be controlled via the `consul.hashicorp.com/service-name` annotation.

IMPROVEMENTS:

  * Use the latest version of consul (1.6.2)
  * Use the latest version of consul-k8s (0.9.5)
  * Add `connectInject.overrideAuthMethodName` to allow setting the `-acl-auth-method flag` [[GH-278](https://github.com/hashicorp/consul-helm/pull/278)]
  * Support external to k8s Consul servers [[GH-289](https://github.com/hashicorp/consul-helm/pull/289)]

BUG FIXES:

  * Do not run `server-acl-init` during server rollout [[GH-292](https://github.com/hashicorp/consul-helm/pull/292)]

## 0.12.0 (Oct 28, 2019)

IMPROVEMENTS:

  * Use the latest version of consul-k8s (0.9.4)
  * Support `bootstrapACLs` when only servers are enabled (not clients) [[GH-250](https://github.com/hashicorp/consul-helm/pull/250)]
  * Use less privileges for catalog sync when not syncing to k8s [[GH-248](https://github.com/hashicorp/consul-helm/pull/248)]
  * Enable disabling tests for users using `helm template` [[GH-249](https://github.com/hashicorp/consul-helm/pull/249)]

BUG FIXES:

  * Fix `missing required field "caBundle"` bug [[GH-213](https://github.com/hashicorp/consul-helm/issues/213)]


## 0.11.0 (Oct 15, 2019)

IMPROVEMENTS:

  * Use the latest version of Consul (1.6.1)

BUG FIXES:

  * Use the latest version of `consul-k8s` (0.9.3) which fixes issues with upgrading between Helm chart
    versions when `bootstrapACLs` is enabled [[GH-246](https://github.com/hashicorp/consul-helm/pull/246)].
  * Add `server-acl-init-cleanup` job to clean up the `server-acl-init` job
    when it completes successfully [[GH-246](https://github.com/hashicorp/consul-helm/pull/246)].
  * Add the ability to specify Consul client daemonset affinity [[GH-165](https://github.com/hashicorp/consul-helm/pull/165)]

## 0.10.0 (Oct 4, 2019)

IMPROVEMENTS:

  * Use latest version of Consul (1.6.0) and consul-k8s (0.9.2)
  * Remove random value from `helm test` to enable helmfile use [[GH-143](https://github.com/hashicorp/consul-helm/pull/143)]
  
BUG FIXES:
  
  * The latest version of `consul-k8s` fixes issues with the `server-acl-init`
    job failing repeatedly.

## 0.9.0 (Sep 6, 2019)

IMPROVEMENTS:

  * Support running the consul snapshot agent
  * Support mesh gateways
  * Allow setting annotations for the DNS service
  * Allow setting `-consul-write-interval`, `-log-level` and `-k8s-source-namespace` flags for consul-k8s sync
  * Allow setting DNS service IP
  * Fix issues where acl-init job would fail repeatedly and ACLs would not be
    bootstrapped

BUG FIXES:

  * Fix enterprise license application when ACLs are turned off
  * `rules` key must always be set (fixes https://github.com/hashicorp/consul-helm/issues/178)

## 0.8.1 (May 9, 2019)

IMPROVEMENTS:

  * Update default consul-k8s version to 0.8.1 for a central config bug fix

## 0.8.0 (May 8, 2019)

IMPROVEMENTS:

  * Support adding a prefix to Kubernetes services registered in Consul [[GH 140](https://github.com/hashicorp/consul-helm/issues/140)]
  * Support an option for automatically bootstrapping ACLs in a Consul cluster that is run fully in Kubernetes. If connectInject is enabled with this option on, this also automatically configures a new Kubernetes AuthMethod so that injected services are automatically granted ACL tokens based on their Kubernetes service account.
  * Support central service configuration including proxy defaults in Connect (available in Consul 1.5+).
  * Remove the `gossipEncryption.enabled` option and instead have the implementation based on the existence of the secretName and secretKey.

## 0.7.0 (March 21, 2019)

BREAKING CHANGES:

  * If previously setting the release name to `consul`, you must now set `fullnameOverride: consul` in your config to prevent all resources being renamed.

IMPROVEMENTS:

  * Support pod PriorityClasses for Consul servers and clients
  * Add annotation and additional spec values for the UI service
  * Add liveness and readiness checks to the catalog sync pod [[consul-k8s GH 57](https://github.com/hashicorp/consul-k8s/issues/57)]
  * Support custom annotations for Consul clients and servers
  * Support PodSecurityPolicies for Consul components
  * Add service accounts and cluster roles/role bindings for each Consul component
  * Add the namespace to the metadata volume name
  * Support tolerations on Consul client and server pods
  * Support gossip protocol encryption
  * Allows custom environment variables for Consul client and server pods
  * Support nodeSelectors for all components
  
BUG FIXES:

  * Allow setting `extraConfig` variables using Helm's `--set` flag [[GH 74](https://github.com/hashicorp/consul-helm/issues/74)]
  * Fix a formatting bug in the enterprise license command

## 0.6.0 (February 8, 2019)

IMPROVEMENTS:

  * Supports applying a Consul Enterprise License to the cluster through the Helm chart
  * Support assigning an ACL token to the catalog sync process [[GH 26](https://github.com/hashicorp/consul-k8s/issues/26)]
  * Updates default `consul` version to `1.4.2` and `consul-k8s` version to `0.5.0`
  
BUG FIXES:

  * Switch the chart labels to a non-changing value to allow helm upgrades [[GH 86](https://github.com/hashicorp/consul-helm/issues/86)]
  
## 0.5.0 (January 11, 2019)

IMPROVEMENTS:

  * Supports new NodePort syncing style that uses the node ip address
  * Adds a configurable tab to the Kubernetes -> Consul sync

## 0.4.0 (December 7, 2018)

IMPROVEMENTS:

  * RBAC support for `syncCatalog`. This will create the `ClusterRole`, `ClusterRoleBinding`
    and `ServiceAccount` that is necessary for the catalog sync. [[GH-20](https://github.com/hashicorp/consul-helm/issues/20)]
  * client: agents now have the node name set to the actual K8S node name [[GH-14](https://github.com/hashicorp/consul-helm/issues/14)]
  * RBAC support for `connectInject`. This will create a `ClusterRole`, `ClusterRoleBinding`,
    and `ServiceAccount` that is necessary for the connect injector to automatically generate
    TLS certificates to interact with the Kubernetes API.
  * Server affinity is now configurable. This makes it easier to run an entire
    Consul cluster on Minikube. [[GH-13](https://github.com/hashicorp/consul-helm/issues/13)]
  * Liveness probes are now http calls, reducing errors in the logs.
  * All namespaced resources now specify the namespace metadata, making `helm template` usage in 
    a non-default namespace easier. [[GH-66](https://github.com/hashicorp/consul-helm/issues/66)]
  * Add support for ClusterIP service syncing.

BUG FIXES:

  * Add catalog sync default behavior flag to the chart [GH-28]
  * Updated images to point to latest versions for 0.3.0.
  * Add missing continuation characters to long commands [[GH-26](https://github.com/hashicorp/consul-helm/issues/26)].
  * connectInject: set the correct namespace for the MutatingWebhookConfiguration
    so that deployments work in non-default namespaces. [[GH-38](https://github.com/hashicorp/consul-helm/issues/38)]
  * Provide a valid `maxUnavailable` value when replicas=1. [[GH-58](https://github.com/hashicorp/consul-helm/issues/58)]
  * Correctly sets server resource requirements.
  * Update the `maxUnavailable` default calculation to allow rolling updates on 3 server clusters. [[GH-71](https://github.com/hashicorp/consul-helm/issues/71)]

## 0.3.0 (October 11, 2018)

FEATURES:

  * `connectInject` can install the automatic Connect sidecar injector.

## 0.2.0 (September 26, 2018)

FEATURES:

  * `syncCatalog` can install the [service catalog sync](https://www.hashicorp.com/blog/consul-and-kubernetes-service-catalog-sync)
    functionality.

IMPROVEMENTS:

  * server: support `storageClass` [[GH-7](https://github.com/hashicorp/consul-helm/issues/7)]

## 0.1.0

Initial release
