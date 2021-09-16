# Consul Kubernetes CLI
This repository contains a CLI tool for installing and operating [Consul](https://www.consul.io/) on Kubernetes. 
**Warning** this tool is currently experimental. Do not use it on Consul clusters you care about.

## Installation & Setup
Currently the tool is not available on any releases page. Instead clone the repository and run `go build -o bin/consul-k8s`
and proceed to run the binary.

## Commands
* [consul-k8s install](#consul-k8s-install)

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

Flags:


  -auto-approve
 	Skip confirmation prompt.

  -dry-run
    Run pre-install checks and display summary of installation.

  -config-file,-f=<string>
 	Path to a file to customize the installation, such as Consul Helm chart values file. Can be specified multiple times.

  -namespace=<string>
 	Namespace for the Consul installation. Defaults to “consul”.

  -preset=<string>
 	Use an installation preset, one of demo, secure. Defaults to the default configuration of the Consul Helm chart.

  -set=<string>
 	Set a value to customize. Can be specified multiple times. Supports Consul Helm chart values.

  -set-file=<string>
      Set a value to customize via a file. The contents of the file will be set as the value. Can be specified multiple times. Supports Consul Helm chart values.

  -set-string=<string>
      Set a string value to customize. Can be specified multiple times. Supports Consul Helm chart values.


Global Flags:
-context=<string> 
	Kubernetes context to use

-kubeconfig, -c=<string>
	Path to kubeconfig file
```
