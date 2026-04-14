# Third Party Licenses

This project includes third-party components that are licensed separately.

---

## Kubernetes Gateway API

Source: https://github.com/kubernetes-sigs/gateway-api  
License: Apache License 2.0  

A modified version of this project is included in:

control-plane/gateway07/gateway-api-0.7.1-custom

Modifications:
- Changed API group from gateway.networking.k8s.io to consul.hashicorp.com
- Integrated with Consul-specific controller logic

The original license (Apache License 2.0) is preserved within the module.