## UNRELEASED

IMPROVEMENTS:

  * RBAC support for `syncCatalog`. This is enabled by default if the catalog
    sync is enabled but can be controlled with `syncCatalog.rbac.enabled`.
    This will create the `ClusterRole` and `ClusterRoleBinding` necessary
    for the catalog sync. [GH-29]

BUG FIXES:

  * Updated images to point to latest versions for 0.3.0.
  * Add missing continuation characters to long commands [GH-26].

## 0.3.0 (October 11, 2018)

FEATURES:

  * `connectInject` can install the automatic Connect sidecar injector.

## 0.2.0 (September 26, 2018)

FEATURES:

  * `syncCatalog` can install the [service catalog sync](https://www.hashicorp.com/blog/consul-and-kubernetes-service-catalog-sync)
    functionality.

IMPROVEMENTS:

  * server: support `storageClass` [GH-7]

## 0.1.0

Initial release
