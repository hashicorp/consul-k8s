# Consul on Kubernetes Helm Chart

---

 **We're looking for feedback on how folks are using Consul on Kubernetes. Please fill out our brief [survey](https://hashicorp.sjc1.qualtrics.com/jfe/form/SV_4MANbw1BUku7YhL)!** 
 

 > **Warning**  
 > Please read the following issue to learn more about upcoming breaking changes that will be implemented by Q4 2022 for the default deployment of Consul on Kubernetes: [Enabling of service mesh by default and disabling of node-level client agents from Consul Service Mesh on Kubernetes and Catalog Sync](https://github.com/hashicorp/consul-k8s/issues/1438)

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

### Prerequisites
  * **Helm 3.2+** (Helm 2 is not supported)
  * **Kubernetes 1.21-1.24** - This is the earliest version of Kubernetes tested.
    It is possible that this chart works with earlier versions but it is
    untested.

### Usage

Detailed installation instructions for Consul on Kubernetes are found [here](https://www.consul.io/docs/k8s/installation/overview). 

1. Add the HashiCorp Helm Repository:
    ``` bash
    $ helm repo add hashicorp https://helm.releases.hashicorp.com
    ```
    
2. Ensure you have access to the Consul Helm chart and you see the latest chart version listed. 
   If you have previously added the HashiCorp Helm repository, run `helm repo update`.

   ```bash
   $ helm search repo hashicorp/consul
   ```

3. Now you're ready to install Consul! To install Consul with the default configuration using Helm 3.2 run the following command below. 
   This will create a `consul` Kubernetes namespace if not already present, and install Consul on the dedicated namespace.

   ```bash
   $ helm install consul hashicorp/consul --set global.name=consul --create-namespace -n consul
   ```

Please see the many options supported in the `values.yaml`
file. These are also fully documented directly on the
[Consul website](https://www.consul.io/docs/platform/k8s/helm.html).

# Tutorials

You can find examples and complete tutorials on how to deploy Consul on 
Kubernetes using Helm on the [HashiCorp Learn website](https://learn.hashicorp.com/consul).
