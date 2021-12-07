## Installing Admin Partitions with ACLs enabled

To enable ACLs on the server cluster use the following config:
```yaml
global:
  enableConsulNamespaces: true
  tls:
    enabled: true
  image: hashicorp/consul-enterprise:1.11.0-ent-beta1
  adminPartitions:
    enabled: true
  acls:
    manageSystemACLs: true
server:
  exposeGossipAndRPCPorts: true
  enterpriseLicense:
    secretName: license
    secretKey: key
  replicas: 1
connectInject:
  enabled: true
  transparentProxy:
    defaultEnabled: false
  consulNamespaces:
    mirroringK8S: true
controller:
  enabled: true
meshGateway:
  enabled: true
```

Identify the LoadBalancer External IP of the `partition-service`
```bash
kubectl get svc consul-consul-partition-service -o json | jq -r '.status.loadBalancer.ingress[0].ip'
```

Migrate the TLS CA credentials from the server cluster to the workload clusters
```bash
kubectl get secret consul-consul-ca-key --context "server-context" -o json | kubectl apply --context "workload-context" -f -
kubectl get secret consul-consul-ca-cert --context "server-context" -o json | kubectl apply --context "workload-context" -f -
```

Migrate the Partition token from the server cluster to the workload clusters
```bash
kubectl get secret consul-consul-partitions-acl-token --context "server-context" -o json | kubectl apply --context "workload-context" -f -
```

Identify the Kubernetes AuthMethod URL of the workload cluster to use as the `k8sAuthMethodHost`:
```bash
kubectl config view -o "jsonpath={.clusters[?(@.name=='workload-cluster-name')].cluster.server}"
```

Configure the workload cluster using the following:

```yaml
global:
  enabled: false
  enableConsulNamespaces: true
  image: hashicorp/consul-enterprise:1.11.0-ent-beta1
  adminPartitions:
    enabled: true
    name: "partition-name"
  tls:
    enabled: true
    caCert:
      secretName: consul-consul-ca-cert
      secretKey: tls.crt
    caKey:
      secretName: consul-consul-ca-key
      secretKey: tls.key
  acls:
    manageSystemACLs: true
    bootstrapToken:
      secretName: consul-consul-partitions-acl-token
      secretKey: token
server:
  enterpriseLicense:
    secretName: license
    secretKey: key
externalServers:
  enabled: true
  hosts: [ "loadbalancer IP" ]
  tlsServerName: server.dc1.consul
  k8sAuthMethodHost: "authmethod-host IP"
client:
  enabled: true
  exposeGossipPorts: true
  join: [ "loadbalancer IP" ]
connectInject:
  enabled: true
  consulNamespaces:
    mirroringK8S: true
controller:
  enabled: true
meshGateway:
  enabled: true
```
This should create clusters that have Admin Partitions deployed on them with ACLs enabled.
