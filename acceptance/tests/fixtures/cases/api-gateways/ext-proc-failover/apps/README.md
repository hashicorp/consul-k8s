# ext-proc failover test images

Source for the six HTTP ext-proc apps used by
`TestAPIGateway_ExtProc_MultiClusterFailover`
([../../../../../api-gateway/api_gateway_ext_proc_failover_test.go](../../../../../api-gateway/api_gateway_ext_proc_failover_test.go)).

These are small, dependency-free (Go stdlib only) HTTP services. They were
originally authored for the `103-envoy-ext-proc-kind-failover` demo and are
vendored here so the acceptance test is self-contained and reproducible without
any external repository or registry.

| App | Image tag | Role |
| --- | --- | --- |
| `ext-proc-http` | `local/ext-proc-http:0.1` | Envoy HTTP `ExternalProcessor` on the api-gateway inbound listener; consults `route-decider` and rewrites `x-route-target`. Used as the single gateway's only instance AND as the two-gateway `base` instance. The same image is deployed a second time as `ext-proc-http-path` (the two-gateway `path` instance) so its logs can be inspected in isolation. |
| `route-decider` | `local/route-decider:0.1` | HTTP decider for the gateway. Maps `:path` (suffix `/b`->service-b, `/c`->service-c, else service-a). |
| `service-d` | `local/service-d:0.1` | Pure forwarder; receives `/dd1` and proxies to `service-e1` over the mesh. |
| `service-e` | `local/service-e:0.1` | Reads `x-route-target` and proxies to `service-f`/`service-g` (falls back to `service-a`). |
| `ext-proc-http-connect-proxy` | `local/ext-proc-http-connect-proxy:0.1` | HTTP `ExternalProcessor` run as a sidecar on `service-e1`'s connect-proxy inbound; consults `http-decider-connect-proxy`. |
| `http-decider-connect-proxy` | `local/http-decider-connect-proxy:0.1` | HTTP decider for the mesh path; randomly returns `service-f` or `service-g`. |

The plain backends (`service-a/b/c/f/g`) use the public `hashicorp/http-echo`
image and need no build.

## Build & load

The test is opt-in (`EXT_PROC_LOCAL_DEV=true` + an enterprise license, since
`builtin/ext-proc` is Consul Enterprise only). Build these images and load them
into BOTH kind clusters before running it:

```bash
# Build all images, then load them into the two kind clusters used by the test.
./build-images.sh <server-cluster-name> <client-cluster-name>
```

Or build only (e.g. to push elsewhere):

```bash
./build-images.sh
```

Override the tag with `IMAGE_TAG=<tag>` (the fixtures under `../common`,
`../single` and `../two` reference `:0.1`).
