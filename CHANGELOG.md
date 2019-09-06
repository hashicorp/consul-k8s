## UNRELEASED

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
