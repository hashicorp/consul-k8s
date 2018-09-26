# Consul + Kubernetes (consul-k8s)

The `consul-k8s` binary includes first-class integrations between Consul and
Kubernetes. The project encapsulates multiple use cases such as syncing
services, injecting Connect sidecars, and more.
The Kubernetes integrations with Consul are
[documented directly on the Consul website](https://www.consul.io/docs/platform/k8s/index.html).
This README will present a basic overview of each use case, but for full
documentation please reference the Consul website.

This project is versioned separately from Consul. Supported Consul versions
for each feature will be noted below. By versioning this project separately,
we can iterate on Kubernetes integrations more quickly and release new versions
without forcing Consul users to do a full Consul upgrade.

## Features

  * [**Catalog Sync**](https://www.consul.io/docs/platform/k8s/service-sync.html):
    Sync Consul services into first-class Kubernetes services and vice versa.
    This enables Kubernetes to easily access external services and for
    non-Kubernetes nodes to easily discover and access Kubernetes services.
    _(Requires Consul 1.1+)_

## Installation

`consul-k8s` is distributed in multiple forms:

  * The recommended installation method is the official
    [Consul Helm chart](https://github.com/hashicorp/consul-helm). This will
    automatically configure the Consul and Kubernetes integration to run within
    an existing Kubernetes cluster.

  * A [Docker image `hashicorp/consul-k8s`](https://hub.docker.com/r/hashicorp/consul-k8s) is available. This can be used to manually run `consul-k8s` within a scheduled environment.

  * Raw binaries are available in the [HashiCorp releases directory](https://releases.hashicorp.com/consul-k8s/).
    These can be used to run `consul-k8s` directly or build custom packages.

## Contributing

To build and install Consul ESM locally, you will need to install the
Docker engine:

- [Docker for Mac](https://docs.docker.com/engine/installation/mac/)
- [Docker for Windows](https://docs.docker.com/engine/installation/windows/)
- [Docker for Linux](https://docs.docker.com/engine/installation/linux/ubuntulinux/)

Clone the repository:

```shell
$ git clone https://github.com/hashicorp/consul-k8s.git
```

To compile the `consul-k8s` binary for your local machine:

```shell
$ make dev
```

This will compile the `consul-k8s` binary into `bin/consul-k8s` as
well as your `$GOPATH` and run the test suite.

Or run the following to generate all binaries:

```shell
$ make dist
```

If you just want to run the tests:

```shell
$ make test
```

Or to run a specific test in the suite:

```shell
go test ./... -run SomeTestFunction_name
```
