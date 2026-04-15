# Third Party Licenses

This project includes third-party components that are licensed separately.

---

## Kubernetes Gateway API

Source: https://github.com/kubernetes-sigs/gateway-api  
License: Apache License 2.0  

A modified version of this project is included in:

charts/consul/templates

Modifications:
- Changed API group from gateway.networking.k8s.io to consul.hashicorp.com in the templates ( template version 0.6.x)

The original license (Apache License 2.0) preserved in the control-plane/gateway07/gateway-api-0.7.1-custom.