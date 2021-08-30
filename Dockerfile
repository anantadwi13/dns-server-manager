FROM golang:1.17 AS builder

WORKDIR /go/src/bind9
COPY go.* ./
RUN go mod download
COPY cmd cmd
COPY internal internal
RUN go mod tidy
RUN GOOS=linux go build -o service ./cmd/service/

FROM internetsystemsconsortium/bind9:9.16
WORKDIR /root
COPY --from=builder /go/src/bind9/service .

VOLUME ["/var/log", "/data"]

EXPOSE 53/udp 53/tcp 953/tcp 5555/tcp

CMD ./service