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

## Routing

Use `OPENCONNECT,COMPANY` to match the VPN server's split routes and split DNS domains. Add the following to the proxy configuration above:

```yaml
proxy-groups:
  - name: COMPANY
    type: select
    proxies:
      - OPENCONNECT

dns:
  enable: true
  nameserver:
    - 1.1.1.1
  # Resolve the VPN gateway independently of the VPN connection.
  proxy-server-nameserver:
    - 1.1.1.1

rules:
  - OPENCONNECT,COMPANY
  - DOMAIN,ad.com,REJECT
  - MATCH,DIRECT
```

The rule matches IPv4/IPv6 split include routes and split DNS domains, including their subdomains. Split exclude routes take precedence; excluded destinations continue to the following rules. Rules are evaluated in their configured order. The keyword is supported only in top-level `rules`.

With `dns.enable: true`, split DNS queries automatically use the server-supplied DNS addresses through the VPN. No manual `nameserver-policy` is required; explicit entries take precedence if configured. Other domains use the normal DNS configuration. If `proxy-server-nameserver` is omitted, VPN gateway resolution uses `default-nameserver` independently of the split DNS policy.

`COMPANY` must be an OpenConnect node or a proxy group containing exactly one OpenConnect node. Mixed groups and multiple VPN nodes are not supported by this rule, including multi-node `load-balance` groups. To target the node above directly, use `OPENCONNECT,OPENCONNECT` and omit the `COMPANY` group. Existing load balancing for other rules remains available.

Default routes (`0.0.0.0/0` and `::/0`), DNS search domains and `TunnelAllDNS` are not imported as split policy. Keep explicit company rules for destinations the server does not supply. The first match or automatic DNS lookup starts the VPN and waits for its configuration. If the first configuration cannot be obtained, requests reaching the rule are rejected and automatic DNS lookups fail. Reconnects and invalid updates preserve the last valid policy; valid updates replace routes and DNS together. No OS routes are installed.

See [the complete example](docs/openconnect/config.example.yaml) and [split route/DNS details](docs/openconnect/usage.md#server-supplied-split-routes-and-dns).

## Build

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
