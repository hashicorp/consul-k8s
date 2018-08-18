# Consul + Kubernetes (consul-k8s)

This repository contains the source for the `consul-k8s` binary which includes
first-class integrations of Consul with Kubernetes. The `consul-k8s` binary
is used for multiple use cases such as syncing catalog entries to and from
K8S services, Connecting admission webhooks, and more.

The Kubernetes integrations with Consul are
[documented directly on the Consul website](https://www.consul.io/docs/kubernetes/index.html).
This README will present a basic overview of each use case, but for full
documentation please reference the Consul website.

## Features

TODO

## Installation

`consul-k8s` is distributed in multiple forms:

  * The recommended installation method is the official
    [Consul Helm chart](https://github.com/hashicorp/consul-helm). This will
    automatically configure the Consul and Kubernetes integration to run within
    an existing Kubernetes cluster.

  * A [Docker image](#) is available. This can be used to manually run
    `consul-k8s` within a scheduled environment.

  * Raw binaries are available in the [HashiCorp releases directory](#).
    These can be used to run `consul-k8s` directly or build custom packages.
