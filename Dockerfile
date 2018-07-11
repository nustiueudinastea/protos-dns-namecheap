FROM golang:1.8.3 as builder
LABEL protos="0.0.1" \
      protos.installer.metadata.description="This applications provides the capability to interact with the Namecheap API" \
      protos.installer.metadata.params="api_user,api_token,username" \
      protos.installer.metadata.capabilities="ResourceProvider,InternetAccess,GetInformation" \
      protos.installer.metadata.provides="dns"

ADD . "/go/src/namecheap-dns/"
WORKDIR "/go/src/namecheap-dns"
RUN go get -u github.com/golang/dep/cmd/dep
RUN dep ensure
RUN go build namecheap-dns.go

FROM alpine:latest
COPY --from=builder /go/src/namecheap-dns/namecheap-dns /root/
COPY --from=builder /go/src/namecheap-dns/start.sh /root/
RUN chmod +x /root/start.sh

ENTRYPOINT ["/root/start.sh"]
