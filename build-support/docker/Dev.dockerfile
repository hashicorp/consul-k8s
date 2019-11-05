FROM consul:latest
COPY pkg/bin/linux_amd64/consul-k8s /bin
