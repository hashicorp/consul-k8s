FROM golang:latest as builder
ARG GIT_COMMIT
ARG GIT_DIRTY
ARG GIT_DESCRIBE
WORKDIR /go/src/github.com/hashicorp/consul-k8s
ENV CONSUL_DEV=1
ENV COLORIZE=0
Add . /go/src/github.com/hashicorp/consul-k8s/
RUN make

FROM consul:latest

COPY --from=builder /go/src/github.com/hashicorp/consul-k8s/cmd/bin/consul-k8s /bin
