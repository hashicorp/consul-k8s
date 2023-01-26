# Windows Support

## Index

- [About](#about)
- [Consul k8s Control Plane Changes](#consul-k8s-control-plane-changes)
  - [Enabling Pod OS detection](#enabling-pod-os-detection)
  - [Assigning Values Depending on the OS](#assigning-values-depending-on-the-os)
  - [Adding New Config Flags](#adding-new-config-flags)
  - [Unit Tests](#unit-tests)
- [Consul Helm Chart Changes](#consul-helm-chart-changes)
  - [Changes in values.yaml](#changes-in-valuesyaml)
  - [Changes in connect-injector-deployment.yaml Template](#changes-in-connect-injector-deploymentyaml-template)
- [How to Use this Chart](#how-to-use-this-chart)
  - [Using Custom Consul Windows Images](#using-custom-consul-windows-images)

## About

The purpose of this file is to document how we achieved automatic Consul injection into Windows workloads.

## Consul k8s Control Plane Changes

### Enabling Pod OS detection

To enable Consul injection into Windows workloads, the first thing we needed was to detect the pod's OS. Using Go's k8s.io/api/core/v1 package we can access the pod's **nodeSelector** field.
nodeSelector is the simplest recommended form of node selection constraint. You can add the nodeSelector field to your Pod specification and specify the node labels you want the target node to have. Kubernetes only schedules the Pod onto nodes that have each of the labels you specify (read more [here](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/)).  
When running mixed clusters (Linux and Windows nodes), you must use the nodeSelector field to make sure the pods are deployed in the correct nodes.  
nodeSelector example:

```yml
nodeSelector:
  kubernetes.io/os: "windows"
```

This is why we chose to use nodeSelector to detect the pods' OS. When running Linux-only clusters, you may not use nodeSelector, in these cases using k8s.io/api/core/v1 `pod.Spec.NodeSelector["kubernetes.io/os"]` will return the field's zero value (an empty string ""). Knowing this, we created the **isWindows** function, the function simply returns true if the pod's OS is Windows and false if it isn't.

```go
func isWindows(pod corev1.Pod) bool {
  podOS := pod.Spec.NodeSelector["kubernetes.io/os"]
  return podOS == "windows"
}
```

### Assigning Values Depending on the OS

After successfully detecting the pod's OS, we used the function we created to assign values depending on the OS the pod was running, so the dataplane sidecar and init containers could be injected with valid values. These values are the result of previous work we had done on the subject (read more [here](https://github.com/hashicorp-education/learn-consul-k8s-windows/blob/main/WindowsTroubleshooting.md#encountered-issues)).  
Assigning values example:  

```go
var dataplaneImage, connectInjectDir string

if isWindows(pod) {
  dataplaneImage = w.ImageConsulDataplaneWindows
  connectInjectDir = "C:\\consul\\connect-inject"
} else {
  dataplaneImage = w.ImageConsulDataplane
  connectInjectDir = "/consul/connect-inject"
}
```

As you can see in the example above, we also updated the **MeshWebhook struct** in [mesh_webhook.go](./control-plane/connect-inject/webhook/mesh_webhook.go), adding the Windows images fields we require.

### Adding New Config Flags

To be able to set the values for **ImageConsulDataplaneWindows** and **ImageConsulK8SWindows** through the Helm chart, we created 3 new flags for the **inject-connect** subcommand.  
The new flags that enable setting this values are:

- consul-image-windows
- consul-dataplane-image-windows
- consul-k8s-image-windows

> **Warning**  
> These flags require a default value, just like their Linux counterpart do, otherwise the `validateFlags()` function (line 750 in [command.go](./control-plane/subcommand/inject-connect/command.go)) will throw an error.

### Unit Tests  

#### command_test.go

After adding the new command flags to *inject-connect/command.go*, and updating the `validateFlags()` function, we updated the unit test in command_test.go to account for these changes.
The updates were focused on the `TestRun_FlagValidation` test.

#### container_init_test.go

We updated test coverage by creating new cases to account for the init container being deployed in a Windows pod. The new cases were added in the following tests:  

- TestHandlerContainerInit
- TestHandlerContainerInit_namespacesAndPartitionsEnabled
- TestHandlerContainerInit_Multiport

These new cases evaluate that the command and the env vars are set appropriately for a Windows pod.

## Consul Helm Chart Changes

### Changes in values.yaml

New fields were added as part of the **global** stanza:

- global.imageWindows: sets the default Windows Consul image.
- global.imageK8SWindows: sets the default Windows Consul K8s control-plane image.
- global.imageConsulDataplaneWindows: sets the default Windows Consul dataplane image.

### Changes in connect-injector-deployment.yaml Template

In order to pass the Windows image values set on the Helm chart to the connect-injector command flags, we modified the [connect-inject-deployment.yaml](./charts/consul/templates/connect-inject-deployment.yaml) template. We added the newly created config flags and used Helm's templating language to insert the required values.

> **Warning**  
> It is essential for these changes to work, to have Windows images available for: Consul, consul-k8s-control-plane, consul-dataplane. You can read more about this [here](https://github.com/hashicorp-education/learn-consul-k8s-windows/tree/main/k8s-v1.0.x/dockerfiles).

## How to Use this Chart

Using this chart is fairly simple, you can first package the chart using Helm:  
`helm package charts/consul`  
This command will create **consul-1.1.0-dev.tgz**, use this file to install the helm chart.

### Using Custom Consul Windows Images

In case you need to use your own Windows Consul images, you can override the default image values.  
Take the following values.yaml as an example:

```yml
# Contains values that affect multiple components of the chart.
global: 
  imageK8S:  # Build the binary in this repo and upload to a repository.
  imageK8SWindows: # Read here: to know how to build your Windows images: https://github.com/hashicorp-education/learn-consul-k8s-windows/tree/main/k8s-v1.0.x/dockerfiles
  imageConsulDataplaneWindows: # Read here: to know how to build your Windows images: https://github.com/hashicorp-education/learn-consul-k8s-windows/tree/main/k8s-v1.0.x/dockerfiles
  # The main enabled/disabled setting.
  # If true, servers, clients, Consul DNS and the Consul UI will be enabled.
  enabled: true
  # The prefix used for all resources created in the Helm chart.
  name: consul
  # The name of the datacenter that the agents should register as.
  datacenter: dc1
  # Enables TLS across the cluster to verify authenticity of the Consul servers and clients.
  tls:
    enabled: false
  # Enables ACLs across the cluster to secure access to data and APIs.
  acls:
    # If true, automatically manage ACL tokens and policies for all Consul components.
    manageSystemACLs: false
# Configures values that configure the Consul server cluster.
server:
  enabled: true
  # The number of server agents to run. This determines the fault tolerance of the cluster.
  replicas: 1
  # When running mixed clusters, nodeSelector MUST be specified.
  nodeSelector: |
    kubernetes.io/os: linux
    kubernetes.io/arch: amd64
# Contains values that configure the Consul UI.
ui:
  enabled: true
  # Registers a Kubernetes Service for the Consul UI as a NodePort.
  service:
    type: NodePort
# Configures and installs the automatic Consul Connect sidecar injector.
connectInject:
  enabled: true
  transparentProxy:
    defaultEnabled: false
  # When running mixed clusters, nodeSelector MUST be specified  
  nodeSelector: |
    kubernetes.io/os: linux
    kubernetes.io/arch: amd64

webhookCertManager:
  # When running mixed clusters, nodeSelector MUST be specified
  nodeSelector: |
      kubernetes.io/os: linux
      kubernetes.io/arch: amd64
```

To use a custom values YAML file use the following command:
`helm install consul <path to chart tgz file>/consul-1.1.0-dev.tgz --values <path to custom values file>/values.yaml`

> **Note**  
> To learn how to deploy an EKS cluster and enabling Windows nodes in EKS you can follow [this guide](https://github.com/hashicorp-education/learn-consul-k8s-windows/blob/main/WindowsLearningGuide.md)
