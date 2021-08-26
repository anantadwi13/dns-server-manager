FROM golang:1.17 AS builder

WORKDIR /go/src/bind9
COPY . .
RUN go mod tidy
RUN GOOS=linux go build -o service ./cmd/service/

FROM internetsystemsconsortium/bind9:9.16
WORKDIR /root
COPY --from=builder /go/src/bind9/service .

VOLUME ["/etc/bind", "/var/cache/bind", "/var/lib/bind", "/var/log", "/data"]

EXPOSE 53/udp 53/tcp 953/tcp 80/tcp

CMD ./service