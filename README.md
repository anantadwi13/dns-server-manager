# DNS Server Manager

Manage DNS Server (Bind9) via REST API

## Installation

```shell
docker run -it --name dns-server \
      -p 53:53/tcp \
      -p 53:53/udp \
      -p 953:953/tcp \
      -p 80:80 \
      -v /path/to/data:/data \
      anantadwi13/dns-server-manager
```