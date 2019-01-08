## UNRELEASED
Improvements:

* Supports a configurable tag for the k8s -> Consul sync [GH 42]

Bug fixes:

* Register NodePort services with the node's ip address [GH 8]

## 0.3.0 (December 7, 2018)
Improvements:

* Support syncing ClusterIP services [GH 4]

Bug fixes:

* Allow unnamed container ports to be used in connect-inject default
  annotations.

## 0.2.1 (October 26, 2018)

Bug fixes:

* Fix single direction catalog sync [GH 7].

## 0.2.0 (October 10, 2018)

Features:

* **New subcommand: `inject-connect`** runs a mutating admission webhook for
  automatic Connect sidecar injection in Kubernetes. While this can be setup
  manually, we recommend using the Consul helm chart.

## 0.1.0 (September 26, 2018)

* Initial release
