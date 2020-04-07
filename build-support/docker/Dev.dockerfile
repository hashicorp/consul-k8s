FROM hashicorp/consul-k8s:latest
COPY pkg/bin/linux_amd64/consul-k8s /bin
