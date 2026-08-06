FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY main.go ./
RUN go build -o /out/http-decider-connect-proxy ./main.go

FROM alpine:3.20
RUN adduser -D -u 10001 app
USER app
COPY --from=build /out/http-decider-connect-proxy /bin/http-decider-connect-proxy
EXPOSE 9500
ENTRYPOINT ["/bin/http-decider-connect-proxy"]
