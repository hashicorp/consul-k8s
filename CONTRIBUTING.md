# Contributing

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
