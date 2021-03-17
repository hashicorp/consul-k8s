---
name: Bug Report
about: You're experiencing an issue with the Consul Helm chart that is different than the documented behavior.
labels: bug

---

When filing a bug, please include the following headings if possible. Any example text in this template can be deleted.

### Overview of the Issue

A paragraph or two about the issue you're experiencing.

### Reproduction Steps

Steps to reproduce this issue, eg:

1. When running helm install with the following `values.yml`:
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

### Logs

Include any relevant logs.

<details>
  <summary>Logs</summary>

```
output from 'kubectl logs' in relevant components
```

</details>

### Expected behavior

What was the expected result?

### Environment details

If not already included, please provide the following:
- `consul-k8s` version:
- `consul-helm` version:
- `values.yaml` used to deploy the helm chart:

Additionally, please provide details regarding the Kubernetes Infrastructure, if known:
- Cloud Provider (If self-hosted, the Kubernetes provider utilized):
- Networking CNI plugin in use:

Any other information you can provide about the environment/deployment.

### Additional Context

Additional context on the problem.
