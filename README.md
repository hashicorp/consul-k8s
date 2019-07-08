# Consul + Kubernetes (consul-k8s)

Change!

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

To build and install `consul-k8s` locally, Go version 1.11.4+ is required because this repository uses go modules and go 1.11.4 introduced changes to checksumming of modules to correct a symlink problem.
You will also need to install the Docker engine:

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

To create a docker image with your local changes:

```shell
$ make dev-docker
```

### Rebasing contributions against master

PRs in this repo are merged using the [`rebase`](https://git-scm.com/docs/git-rebase) method. This keeps
the git history clean by adding the PR commits to the most recent end of the commit history. It also has
the benefit of keeping all the relevant commits for a given PR together, rather than spread throughout the
git history based on when the commits were first created.

If the changes in your PR do not conflict with any of the existing code in the project, then Github supports
automatic rebasing when the PR is accepted into the code. However, if there are conflicts (there will be
a warning on the PR that reads "This branch cannot be rebased due to conflicts"), you will need to manually
rebase the branch on master, fixing any conflicts along the way before the code can be merged.
