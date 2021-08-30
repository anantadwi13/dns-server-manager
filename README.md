# DNS Server Manager

Manage DNS Server (Bind9) via REST API

## Installation

Run container

```shell
docker run -it --name dns-server \
      -p 53:53/tcp \
      -p 53:53/udp \
      -p 127.0.0.1:953:953/tcp \
      -p 5555:5555 \
      -v $(pwd)/temp/data:/data \
      anantadwi13/dns-server-manager
```

## Usage

After running container, open API Specification on `http://{host}:5555/docs`