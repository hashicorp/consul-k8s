                         ┌──────────────────────────┐
                         │      Upgrade Start       │
                         └─────────────┬────────────┘
                                       ▼
                    ┌──────────────────────────────────┐
                    │ TCPRoute GVK Present ?           │
                    │ crds.enableTcpRoute ?            │
                    │ crds.consulapi.enabled ?         │
                    └───────────────┬──────────────────┘
                ┌───────────────────┼───────────────────────┐
                ▼                   ▼                       ▼
         ┌──────────────┐    ┌──────────────┐        ┌──────────────┐    
         │ CASE 1       │    │ CASE 2       │        │ CASE 3       │ 
         │ TCPRoute NO  │    │ TCPRoute NO  │        │ TCPRoute YES │
         │ enableTcp=F  │    │ enableTcp=T  │        │ enableTcp=T  │
         │ consulapi=F  │    │ consulapi=F  │        │ consulapi=F  │
         └───────┬──────┘    └───────┬──────┘        └───────┬──────┘
        ==================== PRE-UPGRADE HOOK ====================
                 ▼                   ▼                       ▼
   ┌────────────────────────┐  ┌────────────────────────┐  ┌────────────────────────┐
   │ Convert resources      │  │ Convert resources      │  │ Same as Case 2         │
   │                        │  │                        │  │                        │
   │ Gateway v1beta1 → v1   │  │ Gateway v1beta1 → v1   │  │ Gateway v1beta1 → v1   │
   │ GatewayClass → v1      │  │ GatewayClass → v1      │  │ GatewayClass → v1      │
   │ HTTPRoute → v1         │  │ HTTPRoute → v1         │  │ HTTPRoute → v1         │
   │ GRPCRoute → v1         │  │ GRPCRoute → v1         │  │ GRPCRoute → v1         │
   │ ReferenceGrant keep    │  │ ReferenceGrant keep    │  │ ReferenceGrant keep    │
   │ Delete TCP CRD if      │  │ TCPRoute → v1alpha2    │  │ Keep TCPRoute v1alpha2 │
   │ owned                  │  │                        │  │                        │
   │ Store manifests in PVC │  │ Store manifests in PVC │  │ Store manifests in PVC │
   └─────────────┬──────────┘  └─────────────┬──────────┘  └─────────────┬──────────┘
    ================================= CONTROLLERS ====================================
                 ▼                            ▼                            ▼
       ┌─────────────────────┐       ┌─────────────────────┐       ┌─────────────────────┐
       │ Gateway Controller  │       │ Gateway Controller  │       │ Gateway Controller  │
       │                     │       │                     │       │                     │
       │ Watches:            │       │ Watches:            │       │ Watches:            │
       │ Gateway             │       │ Gateway             │       │ Gateway             │
       │ HTTPRoute           │       │ HTTPRoute           │       │ HTTPRoute           │
       │ GRPCRoute           │       │ GRPCRoute           │       │ GRPCRoute           │
       │ TCPRoute disabled.  │       │ TCPRoute            │       │ TCPRoute            │
       └─────────────┬───────┘       └─────────────┬───────┘       └─────────────┬───────┘
       ================================ POST-UPGRADE HOOK ==================================
                 ▼                            ▼                            ▼
          ┌──────────────┐             ┌──────────────┐             ┌──────────────┐
          │ Apply        │             │ Apply        │             │ Apply        │
          │ manifests    │             │ manifests    │             │ manifests    │
          │ from PVC     │             │ from PVC     │             │ from PVC     │
          └──────┬───────┘             └──────┬───────┘             └──────┬───────┘
                 ▼                            ▼                            ▼
          ┌──────────────┐             ┌──────────────┐             ┌──────────────┐
          │ Upgrade Done │             │ Upgrade Done │             │ Upgrade Done │
          │ No action    │             │ No action    │             │ No action    │
          └──────────────┘             └──────────────┘             └──────────────┘



────────────────────────────────────────────────────────────
        ┌────────────────────────────┐
        │ CASE 4                     │
        │ TCPRoute YES               │
        │ enableTcpRoute = false     │
        │ consulapi = false          │
        └──────────────┬─────────────┘
                       ▼
        ┌───────────────────────────────┐
        │ INVALID CONFIGURATION         │
        | TCP resources exist but       │
        | controller disabled           │
        │ Upgrade should FAIL early     │
        | via conditional checks        │
        └───────────────────────────────┘
────────────────────────────────────────────────────────────
        ┌────────────────────────────┐
        │ CASE 5                     │
        │ consulapi.enabled = true   │
        └──────────────┬─────────────┘
                                  ▼
==================== PRE-UPGRADE HOOK ====================
        ┌──────────────────────────────────┐
        │ Generate manifests for both APIs │
        |                                  │
        │ gateway.networking → /data/gw    │
        │ consul.hashicorp → /data/consul  │
        └──────────────┬───────────────────┘
==================== CONTROLLERS ====================
                        ▼
        ┌─────────────────────────────────┐
        │ Dual Controller Mode            │
        │                                 │
        │ Old Controller                  │
        │ watches gateway.networking API  │
        │                                 │
        │ New Controller                  │
        │ watches consul.hashicorp API    │
        └──────────────┬──────────────────┘
==================== POST-UPGRADE ====================
                        ▼
           ┌───────────────────────────────┐
           │ Apply manifests               │
           │ /data/gatewayapi              │
           │ /data/consulapi               │
           └──────────────┬────────────────┘
                          ▼
      ┌───────────────────────────────────────────┐
      │ Customer Action                           │
      │ Switch DNS to Consul Gateway              │
      │ Then delete gateway.networking resources  │
      └───────────────────────────────────────────┘