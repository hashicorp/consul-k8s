---
name: Question
about: You'd like to clarify your understanding about a particular area within Consul K8s. We'd like to help and engage the community through Github!
labels: question

---

<!--
You've selected this issue type since you'd like to clarify your understanding about a particular area within Consul K8s. There are situations when an issue or feature request does not really classify the type of help you are requesting from the Consul K8s team. We'd like to help and engage the community through Github!
-->

#### Question

<!--

Provide a clear description of the question you would like answered with as much detail as you can provide (links to docs, gists of commands). If you are reporting a feature request or issue, please use the other issue types instead. If appropriate, please use the sections below for providing more details around your configuration, CLI commands, logs, and environment details!

Please search the existing issues for relevant questions, and use the reaction feature (https://blog.github.com/2016-03-10-add-reactions-to-pull-requests-issues-and-comments/) to add upvotes to pre-existing questions.

More details will help us answer questions more accurately and with less delay :) 
-->

### CLI Commands (consul-k8s, consul-k8s-control-plane, helm)

<!--

Provide any relevant CLI commands and output from those commands that could help understand what you've attempted so far.

```
consul-k8s install 
```

-->

### Helm Configuration

<!--- 

In order to effectively understand and answer your question, please provide exact steps that allow us the reproduce the problem. If no steps are provided, then it will likely take longer to get your question answered. An example that you can follow is provided below. 

Steps to reproduce this issue, eg:

1. When running helm install with the following `values.yaml`:
```
global:
  domain: consul
  datacenter: dc1
server:
  replicas: 1
  bootstrapExpect: 1
connectInject:
  enabled: true
controller:
  enabled: true
```
1. View error

  --->

### Logs

<!---

Provide log files from Consul Kubernetes components by providing output from `kubectl logs` from the pod and container that is surfacing the issue. 

<details>
  <summary>Logs</summary>

```
output from 'kubectl logs' in relevant components
```

</details>

--->

### Current understanding and Expected behavior

<!--- What is you current understanding of what is supposed to happen? What was the expected result after utilizing the commmands config you provided?  --->

### Environment details

<!---

If not already included, please provide the following:
- `consul-k8s` version:
- `values.yaml` used to deploy the helm chart:

Additionally, please provide details regarding the Kubernetes Infrastructure, as shown below:
- Kubernetes version: v1.22.x
- Cloud Provider (If self-hosted, the Kubernetes provider utilized): EKS, AKS, GKE, OpenShift (and version), Rancher (and version), TKGI (and version)
- Networking CNI plugin in use: Calico, Cilium, NSX-T 

Any other information you can provide about the environment/deployment.
--->

### Additional Context

<!---
Additional context on the problem. Docs, links to blogs, or other material that lead you to discover this issue or were helpful in troubleshooting the issue. 
--->
