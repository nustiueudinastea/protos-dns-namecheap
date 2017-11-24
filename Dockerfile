FROM golang:1.8.3
LABEL protos="0.0.1" \
      protos.installer.metadata.description="This applications provides the capability to interact with the Namecheap API" \
      protos.installer.metadata.params="api_user,api_token,username,domain" \
      protos.installer.metadata.provides="dns"

ADD . "/go/src/namecheap-dns/"
WORKDIR "/go/src/namecheap-dns"
RUN curl https://glide.sh/get | sh
RUN glide update --strip-vendor
RUN go build namecheap-dns.go
RUN chmod +x /go/src/namecheap-dns/start.sh

ENTRYPOINT ["/go/src/namecheap-dns/start.sh"]
