FROM circleci/golang:1.13
COPY --from=consul /bin/consul /bin/consul
