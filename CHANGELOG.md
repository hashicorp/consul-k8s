## UNRELEASED

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
