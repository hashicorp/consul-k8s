# Consul on Kubernetes Helm Chart

---

 **We're looking for feedback on how folks are using Consul on Kubernetes. Please fill out our brief [survey](https://hashicorp.sjc1.qualtrics.com/jfe/form/SV_4MANbw1BUku7YhL)!** 

## Overview

This is the Official HashiCorp Helm chart for installing and configuring Consul on Kubernetes. This chart supports multiple use cases of Consul on Kubernetes, depending on the values provided.

For full documentation on this Helm chart along with all the ways you can use Consul with Kubernetes, please see the Consul and Kubernetes documentation.

> :warning: **Please note**: We take Consul's security and our users' trust very seriously. If
you believe you have found a security issue in Consul K8s, _please responsibly disclose_
by contacting us at [security@hashicorp.com](mailto:security@hashicorp.com).

## Features
    
  * [**Consul Service Mesh**](https://www.consul.io/docs/k8s/connect):
    Run Consul Service Mesh on Kubernetes. This feature
    injects Envoy sidecars and registers your Pods with Consul.
    
  * [**Catalog Sync**](https://www.consul.io/docs/k8s/service-sync):
    Sync Consul services into first-class Kubernetes services and vice versa.
    This enables Kubernetes to easily access external services and for
    non-Kubernetes nodes to easily discover and access Kubernetes services.

## Installation

`consul-k8s` is distributed in multiple forms:

  * The recommended installation method is the official
    [Consul Helm chart](https://github.com/hashicorp/consul-k8s/tree/main/charts/consul). This will
    automatically configure the Consul and Kubernetes integration to run within
    an existing Kubernetes cluster.

  * A [Docker image `hashicorp/consul-k8s-control-plane`](https://hub.docker.com/r/hashicorp/consul-k8s-control-plane) is available. This can be used to manually run `consul-k8s-control-plane` within a scheduled environment.

  * Consul K8s CLI, distributed as `consul-k8s`, can be used to install and uninstall Consul Kubernetes. See the [Consul K8s CLI Reference](https://www.consul.io/docs/k8s/k8s-cli) for more details on usage. 

### Prerequisites

The following pre-requisites must be met before installing Consul on Kubernetes. 

  * **Kubernetes 1.23.x - 1.26.x** - This represents the earliest versions of Kubernetes tested.
    It is possible that this chart works with earlier versions, but it is
    untested.
  * Helm install
    * **Helm 3.6+** for Helm based installs. 
  * Consul K8s CLI based install
    * `kubectl` configured to authenticate to a Kubernetes cluster with a valid `kubeconfig` file.
    * `brew`, `yum`, or `apt` package manager on your local machine 

### CLI

The Consul K8s CLI is the easiest way to get up and running with Consul on Kubernetes. See [Install Consul on K8s CLI](https://developer.hashicorp.com/consul/docs/k8s/installation/install-cli#install-the-cli) for more details on installation, and refer to 
[Consul on Kubernetes CLI Reference](https://developer.hashicorp.com/consul/docs/k8s/k8s-cli) for more details on subcommands and a list of all available flags
for each subcommand. 


 1. Install the HashiCorp tap, which is a repository of all Homebrew packages for HashiCorp:
 
    ``` bash
    brew tap hashicorp/tap
    ```
  
2. Install the Consul K8s CLI with hashicorp/tap/consul formula.

    ``` bash
    brew install hashicorp/tap/consul-k8s
    ```
  
3. Issue the install subcommand to install Consul on Kubernetes:
   
    ``` bash 
    consul-k8s install 
    ```

### Helm

The Helm chart is ideal for those who prefer to use Helm for automation for either the installation or upgrade of Consul on Kubernetes. The chart supports multiple use cases of Consul on Kubernetes, depending on the values provided. Detailed installation instructions for Consul on Kubernetes are found [here](https://www.consul.io/docs/k8s/installation/overview). 

1. Add the HashiCorp Helm repository:
   
    ``` bash
    helm repo add hashicorp https://helm.releases.hashicorp.com
    ```
    
2. Ensure you have access to the Consul Helm chart and you see the latest chart version listed. If you have previously added the 
   HashiCorp Helm repository, run `helm repo update`. 

    ``` bash
    helm search repo hashicorp/consul
    ```

3. Now you're ready to install Consul! To install Consul with the default configuration using Helm 3.2 run the following command below.
   This will create a `consul` Kubernetes namespace if not already present, and install Consul on the dedicated namespace. 
 
   ``` bash
   helm install consul hashicorp/consul --set global.name=consul --create-namespace -n consul

Please see the many options supported in the `values.yaml`
file. These are also fully documented directly on the
[Consul website](https://www.consul.io/docs/platform/k8s/helm.html).

## Tutorials

You can find examples and complete tutorials on how to deploy Consul on 
Kubernetes using Helm on the [HashiCorp Learn website](https://learn.hashicorp.com/collections/consul/kubernetes).
