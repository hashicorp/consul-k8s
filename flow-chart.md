                           ┌─────────────────────────────┐
                           │       OCP Upgrade Start     │
                           └──────────────┬──────────────┘
                                          ▼
                           ┌─────────────────────────────┐
                           │ isupgradefrom418to419 = true│
                           │                             │
                           │ TCPRoute GVK Present?       │
                           │ crds.consulapi.enabled ?    │
                           └──────────────┬──────────────┘
              ┌───────────────────────────┼───────────────────────────┐
              ▼                           ▼                           ▼
        ┌───────────────┐          ┌───────────────┐          ┌───────────────┐
        │   CASE 1A     │          │   CASE 1B     │          │    CASE 2     │
        │ TCP GVK = No  │          │ TCP GVK = No  │          │ TCP GVK = Yes │
        │ consulapi=F   │          │ consulapi=T   │          │ forced true   │
        └───────┬───────┘          └───────┬───────┘          └───────┬───────┘
                │                          │                          │
        ========================== PRE-UPGRADE HOOK ==================================
                ▼                          ▼                          ▼
      ┌───────────────────────┐  ┌────────────────────────────┐  ┌────────────────────────────┐
      │Generate manifests     │  │Generate Consul manifests   │  │Generate Consul manifests   │
      │from existing objects  │  │                            │  │                            │
      │                       │  │Gateway → consul API        │  │Gateway → consul API        │
      │Convert:               │  │GatewayClass → consul API   │  │GatewayClass → consul API   │
      │Gateway → v1           │  │HTTPRoute → consul API      │  │HTTPRoute → consul API      │
      │GatewayClass → v1      │  │GRPCRoute → consul API      │  │GRPCRoute → consul API      │
      │HTTPRoute → v1         │  │ReferenceGrant → consul API │  │TCPRoute → consul API       │
      │GRPCRoute → v1         │  │TCPRoute → consul API       │  │                            │
      │ReferenceGrant v1beta1 │  │                            │  │                            │
      │                       │  │Store → /data/consul        │  │Store → /data/consul        │
      │Store → /data/gw       │  │                            │  │                            │
      │Delete TCP CRD owned   │  │Generate Gateway manifests  │  │Generate Gateway manifests  │
      │                       │  │Store → /data/gw            │  │Store → /data/gw            │
      └───────────┬───────────┘  └───────────┬────────────────┘  └───────────┬────────────────┘
            =================== CONTROLLERS AFTER UPGRADE ==============================
                  ▼                          ▼                               ▼
     ┌───────────────────────┐   ┌─────────────────────────────┐   ┌─────────────────────────────┐
     │Gateway Controller     │   │Old Gateway Controller watch │   │Old Gateway Controller       │
     │gateway.networking API │   │gateway.networking API       │   │gateway.networking API       │
     │                       │   │                             │   │                             │
     │Does NOT watch         │   │New Controller watch         |   │New Controller watch         │
     │TCPRoute               │   │consul.hashicorp.com API     │   │consul.hashicorp.com API     │
     └───────────────┬───────┘   └───────────────┬─────────────┘   └───────────────┬─────────────┘
                     ▼                           ▼                                 ▼
            ==================== POST-UPGRADE HOOK ==================================
                  ▼                          ▼                          ▼
      ┌─────────────────────────┐   ┌─────────────────────────┐   ┌─────────────────────────┐
      │Apply manifests          │   │Apply manifests          │   │Apply manifests          │
      │/data/gatewayapi         │   │/data/gatewayapi         │   │/data/gatewayapi         │
      │                         │   │/data/consulapi          │   │/data/consulapi          │
      └─────────────┬───────────┘   └─────────────┬───────────┘   └─────────────┬───────────┘
                    ▼                             ▼                             ▼
        ┌──────────────────────┐      ┌───────────────────────────┐     ┌───────────────────────────┐
        │ Upgrade Complete     │      │ DNS Switch Required       │     │ DNS Switch Required       │
        │                      │      │                           │     │                           │
        │ Only Gateway API     │      │ After DNS switch          │     │ After DNS switch          │
        │ v1 resources remain  │      │ delete gateway API objs   │     │ delete gateway API objs   │
        └──────────────────────┘      └───────────────────────────┘     └───────────────────────────┘