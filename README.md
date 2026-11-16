# mihomo-openconnect

Mihomo with a native AnyConnect/OpenConnect outbound, powered by [sing-openconnect](https://github.com/SagerNet/sing-openconnect) and Mihomo's existing gVisor IP stack. Rules, DNS policies, proxy groups, proxy chains and load balancing remain available.

[![Validate](https://github.com/Kevincible/mihomo-openconnect/actions/workflows/validate.yml/badge.svg)](https://github.com/Kevincible/mihomo-openconnect/actions/workflows/validate.yml)
[![Track upstream](https://github.com/Kevincible/mihomo-openconnect/actions/workflows/track-upstream.yml/badge.svg)](https://github.com/Kevincible/mihomo-openconnect/actions/workflows/track-upstream.yml)

Download CLI binaries for macOS, Linux and Windows (arm64/amd64) from [Releases](https://github.com/Kevincible/mihomo-openconnect/releases). Each release includes complete source, SHA256 checksums and build manifests. The module path remains `github.com/metacubex/mihomo`.

```yaml
proxies:
  - name: OPENCONNECT
    type: openconnect
    server: vpn.example.com
    port: 443
    username: your-username
    password: your-password
    udp: true
    dtls: true
    ip-stack:
      mode: gvisor
```

Use [the complete example](docs/openconnect/config.example.yaml) to route company DNS and traffic through this outbound. Keep your existing company rules: the adapter does not install server routes into global policy. A disconnected VPN does not fall back to a direct connection.

```sh
# Use the exact Go version recorded in upstream.lock.json.
go build -mod=readonly -tags with_gvisor -o bin/mihomo-openconnect .
./bin/mihomo-openconnect -t -f docs/openconnect/config.example.yaml
```

Configuration is validated without contacting the VPN. A session starts on first use and is shared by connections. Request cancellation does not cancel that session. Address/MTU changes close affected sockets and replace the IP stack; unchanged configurations preserve it. Disabling DTLS still permits TCP, UDP and DNS over TLS.

- [Configuration, authentication and lifecycle](docs/openconnect/usage.md)
- [Upstream tracking, reproducible builds and rollback](docs/openconnect/maintenance.md)
- [Validation and host integration scope](docs/openconnect/validation.md)
- [Original Mihomo README](docs/openconnect/README.upstream.md)

## Validation

```sh
docker build -t mihomo-openconnect-ocserv:test tests/openconnect/ocserv
OPENCONNECT_INTEGRATION=1 go test -race -tags with_gvisor ./adapter/outbound \
  -run TestOpenConnectIntegration -count=1 -v -timeout 12m
```

The fixture creates temporary credentials and certificates and only publishes its VPN port on loopback. It exercises real ocserv authentication, TCP/UDP/DNS, DTLS and TLS fallback, a gateway proxy chain, restart/outage recovery, address/MTU changes, request cancellation and concurrent close. No company VPN or UI automation is used.

The scheduled workflow checks both upstreams every six hours, pins exact commits and validates before publishing. Ordinary sing-openconnect branch commits produce GitHub prereleases; stable tags and Mihomo stable releases use the stable release channel. Conflicts and failed gates stop publication. See the maintenance guide for permissions and manual reproduction.

License: [GPL-3.0](LICENSE), inherited from Mihomo. This is an independent fork, not an official MetaCubeX or SagerNet release.
