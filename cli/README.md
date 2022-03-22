# Consul Kubernetes CLI
This repository contains a CLI tool for installing and operating [Consul](https://www.consul.io/) on Kubernetes. 
**Warning** this tool is currently experimental. Do not use it on Consul clusters you care about.

## Installation & Setup
Currently the tool is not available on any releases page. Instead clone the repository and run `go build -o bin/consul-k8s`
from this directory and proceed to run the binary.

## Commands
* [consul-k8s install](#consul-k8s-install)
* [consul-k8s uninstall](#consul-k8s-uninstall)

### consul-k8s install
This command installs Consul on a Kubernetes cluster. It allows `demo` and `secure` installations via preset configurations
using the `-preset` flag. The `demo` installation installs just a single replica server with sidecar injection enabled and
is useful to test out service mesh functionality. The `secure` installation is minimal like `demo` but also enables ACLs and TLS.

Get started with:
```bash
consul-k8s install -preset=demo
```

Note that when configuring an installation, the precedence order is as follows from lowest to highest precedence:
1. `-preset`
2. `-f`
3. `-set`
4. `-set-string`
5. `-set-file`

For example, `-set-file` will override a value provided via `-set`. Additionally, within each of these groups the
rightmost flag value has the highest precedence, i.e `-set foo=bar -set foo=baz` will result in `foo: baz` being set.

```
Usage: consul-k8s install [flags]
Install Consul onto a Kubernetes cluster.

Command Options:

  -auto-approve
      Skip confirmation prompt. The default is false.

  -config-file=<string>
      Path to a file to customize the installation, such as Consul Helm chart
      values file. Can be specified multiple times. This is aliased as "-f".

  -dry-run
      Run pre-install checks and display summary of installation. The default
      is false.

  -namespace=<string>
      Namespace for the Consul installation. The default is consul.

  -preset=<string>
      Use an installation preset, one of demo, secure. Defaults to none

  -set=<string>
      Set a value to customize. Can be specified multiple times. Supports
      Consul Helm chart values.

  -set-file=<string>
      Set a value to customize via a file. The contents of the file will be
      set as the value. Can be specified multiple times. Supports Consul Helm
      chart values.

  -set-string=<string>
      Set a string value to customize. Can be specified multiple times.
      Supports Consul Helm chart values.

  -timeout=<string>
      Timeout to wait for installation to be ready. The default is 10m.

  -wait
      Determines whether to wait for resources in installation to be ready
      before exiting command. The default is true.

Global Options:

  -context=<string>
      Kubernetes context to use.

  -kubeconfig=<string>
      Path to kubeconfig file. This is aliased as "-c".

```

### consul-k8s uninstall
This command uninstalls Consul on Kubernetes, while prompting whether to uninstall the release and whether to delete all
related resources such as PVCs, Secrets, and ServiceAccounts.

Get started with:
```bash
consul-k8s uninstall
```

```
Usage: consul-k8s uninstall [flags]
Uninstall Consul with options to delete data and resources associated with Consul installation.

Command Options:

  -auto-approve
      Skip approval prompt for uninstalling Consul. The default is false.

  -name=<string>
      Name of the installation. This can be used to uninstall and/or delete
      the resources of a specific Helm release.

  -namespace=<string>
      Namespace for the Consul installation.

  -timeout=<string>
      Timeout to wait for uninstall. The default is 10m.

  -wipe-data
      When used in combination with -auto-approve, all persisted data (PVCs
      and Secrets) from previous installations will be deleted. Only set this
      to true when data from previous installations is no longer necessary.
      The default is false.

Global Options:

  -context=<string>
      Kubernetes context to use.

  -kubeconfig=<string>
      Path to kubeconfig file. This is aliased as "-c".
```
