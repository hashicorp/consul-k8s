## Datadog Acceptance Testing

Tests helm chart override automation configurations for expected operation.


```yaml
global:
  metrics:
    enabled: true
    enableAgentMetrics: true
    disableAgentHostName: true
    enableHostMetrics: true
    datadog:
      enabled: true
      dogstatsd:
        enabled: true
        socketTransportType: "UDS"
        dogstatsdAddr: "/var/run/datadog/dsd.socket"
        dogstatsdTags: [ "source:consul","consul_service:consul-server" ]
```