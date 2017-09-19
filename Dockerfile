FROM golang:1.8.3
LABEL protos=0.0.1

ADD . "/go/src/namecheap-dns/"
WORKDIR "/go/src/namecheap-dns"
RUN curl https://glide.sh/get | sh
RUN glide update --strip-vendor
RUN go build namecheap-dns.go
RUN chmod +x /go/src/namecheap-dns/start.sh

ENTRYPOINT ["/go/src/namecheap-dns/start.sh"]
