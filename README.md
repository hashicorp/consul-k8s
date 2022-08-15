<h1>
  <img src="./assets/logo.svg" align="left" height="46px" alt="Consul logo"/>
  <span>Consul on Kubernetes</span>
</h1>

 **We're looking for feedback on how folks are using Consul on Kubernetes. Please fill out our brief [survey](https://hashicorp.sjc1.qualtrics.com/jfe/form/SV_4MANbw1BUku7YhL)!** 

## Overview

The `consul-k8s-control-plane` binary includes first-class integrations between Consul and
Kubernetes. The project encapsulates multiple use cases such as syncing
services, injecting Connect sidecars, and more.
The Kubernetes integrations with Consul are
[documented directly on the Consul website](https://www.consul.io/docs/platform/k8s/index.html).

This README will present a basic overview of use cases and installing the Helm charts, but for full documentation please reference the Consul website.

This project is versioned separately from Consul. Supported Consul versions
for each feature will be noted below. By versioning this project separately,
we can iterate on Kubernetes integrations more quickly and release new versions
without forcing Consul users to do a full Consul upgrade.

> **Note**  
> We take Consul's security and our users' trust very seriously. If
you believe you have found a security issue in Consul K8s, _please responsibly disclose_
by contacting us at [security@hashicorp.com](mailto:security@hashicorp.com).

## Features
    
  * [**Consul Service Mesh (Connect)**](https://www.consul.io/docs/k8s/connect):
    Run Consul Service Mesh (aka Consul Connect) on Kubernetes. This feature
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

  * Raw binaries are available in the [HashiCorp releases directory](https://releases.hashicorp.com/consul-k8s/).
    These can be used to run `consul-k8s` directly or build custom packages.

## Helm

Within the ['charts/consul'](charts/consul) directory is the official HashiCorp Helm chart for installing
and configuring Consul on Kubernetes. This chart supports multiple use
cases of Consul on Kubernetes, depending on the values provided.

For full documentation on this Helm chart along with all the ways you can
use Consul with Kubernetes, please see the
[Consul and Kubernetes documentation](https://www.consul.io/docs/platform/k8s/index.html).

### Prerequisites
  * **Helm 3.2+** (Helm 2 is not supported)
  * **Kubernetes 1.19+** - This is the earliest version of Kubernetes tested.
    It is possible that this chart works with earlier versions, but it is
    untested.

### Usage

Detailed installation instructions for Consul on Kubernetes are found [here](https://www.consul.io/docs/k8s/installation/overview). 

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

# Tutorials

You can find examples and complete tutorials on how to deploy Consul on 
Kubernetes using Helm on the [HashiCorp Learn website](https://learn.hashicorp.com/collections/consul/kubernetes).
