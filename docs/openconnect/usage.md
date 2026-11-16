# OpenConnect configuration and host API

The initial supported protocol is AnyConnect (including ocserv). Other VPN flavors and browser/SSO authentication are not exposed by this adapter. Authentication forms can be completed by a Go host through `OpenConnectAuthHandler`; a CLI configuration that needs an interactive form fails explicitly instead of waiting indefinitely.

## Configuration

All options live in a `type: openconnect` proxy. See [config.example.yaml](config.example.yaml).

| Option | Default | Meaning |
| --- | --- | --- |
| `server`, `port` | required, 443 | Gateway hostname, IP or HTTPS URL with an optional path. A URL port must agree with `port`. |
| `username`, `password` | empty | Non-interactive credentials. |
| `auth-group` | empty | Authentication group selected by the library. |
| `cookie` | empty | Previously obtained session cookie; host must supply a valid current cookie. |
| `totp-secret` | empty | Base32 software-token secret; TOTP calculation is handled by sing-openconnect. |
| `ca` | system trust | Additional inline PEM certificate authority. |
| `certificate`, `private-key` | empty | Inline client certificate and key PEM, supplied together. |
| `servername` | gateway hostname | TLS certificate name override. |
| `skip-cert-verify` | false | Explicitly disable certificate verification; use a CA for private gateways instead. |
| `udp` | true | Permit application UDP through the VPN. |
| `dtls` | true | Permit the library to negotiate DTLS. TLS remains the fallback. |
| `ipv6` | false | Accept negotiated IPv6 tunnel addresses. |
| `mtu` | negotiated | Requested tunnel MTU, 576–65535. The negotiated value configures the outbound stack. |
| `connect-timeout` | 30 | Maximum seconds for an individual TCP dial / UDP setup, including waiting for readiness. |
| `reconnect-timeout` | 300 | Library reconnection budget in seconds after an established session fails. |
| `dpd-interval` | server/library default | Dead-peer detection interval in seconds; nonzero values below 2 are raised to 2 by the library. |
| `user-agent`, `reported-os`, `local-hostname` | library defaults | Gateway compatibility fields. |
| `ip-stack.mode` | gvisor | Currently only `gvisor` is validated and accepted. Build with `with_gvisor`. |
| `dialer-proxy` | none | Route both gateway TLS and DTLS through a Mihomo proxy or group. |
| `interface-name`, `routing-mark`, `ip-version`, `tfo`, `mptcp` | Mihomo defaults | Existing underlying Mihomo dialer options. |

Timeout options accept 0 for default and otherwise 1–86400 seconds. The global DNS policy handles application hostname resolution. An explicit company DNS server can use `#OPENCONNECT` to carry queries through that outbound. Keep `proxy-server-nameserver` independent of the VPN it is trying to establish. Mihomo's proxy cycle validation and the adapter's self-reference check reject cyclic proxy chains.

## Server-supplied split routes and DNS

Add a top-level rule targeting an OpenConnect proxy or a group containing exactly one OpenConnect proxy:

```yaml
rules:
  - OPENCONNECT,COMPANY
  - DOMAIN,ad.com,REJECT
  - DOMAIN-SUFFIX,google.com,auto
  - MATCH,DIRECT
```

The rule matches the server's IPv4/IPv6 split include routes and split DNS domains, including their subdomains. Split exclude routes take precedence, including for resolved split DNS domains; excluded destinations continue to the following rules. Rules above `OPENCONNECT` retain their normal priority. The target may contain nested groups, but every path must lead to the same OpenConnect node; mixed groups and multiple VPN nodes are rejected. This keyword is not supported inside logical rules, rule providers or sub-rules.

With `dns.enable: true`, queries for split DNS domains automatically use the server-supplied DNS addresses through that OpenConnect connection. Explicit `nameserver-policy` entries take precedence. Public domains keep the normal DNS configuration. When `proxy-server-nameserver` is omitted, gateway resolution uses `default-nameserver` independently of the automatic policy. Disabling the rule also disables its automatic DNS policy. Fake-IP mode remains supported: the actual hostname lookup when opening the connection uses the same split DNS policy.

The first rule match or automatic DNS lookup starts the VPN and waits for its configuration, bounded by `connect-timeout`; DNS lookups also observe their request deadline. Before the first valid configuration, requests reaching this rule are rejected and automatic DNS lookups fail rather than falling through to public routing/DNS. Reconnects retain the last valid policy; valid updates replace routes and DNS together, even if the tunnel address and MTU do not change. VPN DNS answers have a separate cache which is replaced on policy changes. A VPN DNS failure does not fall back to public DNS. Policies are held in memory, not persisted across configuration reloads or process restarts.

`OPENCONNECT` imports only split routes: it ignores both `0.0.0.0/0` and `::/0`, because the library's public API cannot distinguish explicit default routes from automatically synthesized defaults. Default search domains and `TunnelAllDNS` do not become split DNS matches. Keep explicit rules for full-tunnel routing, unsupported server policy, and any company destinations the server does not supply. Without this keyword, the existing manually configured routing/DNS behavior is unchanged; no OS routes are installed.

## Session ownership

Each proxy owns one library client and one IP stack. Construction does not authenticate or change OS routing. The first request starts the client once. Connection contexts only govern their own waits; `Close()` governs the shared session and joins the adapter's workers. Authentication and certificate errors remain associated with this outbound; reload the proxy with corrected credentials to start a new client after terminal failure.

The current tunnel address and MTU must match the active stack before a connection is returned. Configuration notifications are asynchronous, so queued notification payloads are never used to roll back a newer configuration. The bounded receive backlog is drained when the stack is replaced. Packet reads that span a stack replacement are discarded; IP source/destination checks prevent packets for a previous address from entering the new stack. Packet memory is copied through the library's public API and its queue remains bounded.

Protocol keepalive, backoff, DTLS fallback and reconnection belong to sing-openconnect. Transport interruptions can lose UDP packets and TCP may retransmit. If addresses or MTU change, existing sockets are closed; this is not seamless migration. Rekeying with unchanged addresses/MTU retains the stack.

## Host-managed authentication

Create `outbound.OpenConnectOption` programmatically and set `AuthHandler` (excluded from YAML decoding). Implement:

```go
AuthenticateOpenConnect(ctx context.Context,
    challenge outbound.OpenConnectAuthChallenge) (map[string]string, error)
```

Return values keyed by each field's `Key`; use `Kind`, `Label` and `Choices` to present forms or retrieve credentials. The handler must return when `ctx` is canceled, otherwise host code can delay `Close()`. No keys, passwords, cookies, form values or TOTP secrets are logged by the adapter or serialized in proxy JSON. A host should protect its configuration and avoid logging returned challenge contents.

The handler supports multiple authentication rounds and reauthentication requested by the library. Keychain access and UI belong to the host, not this package. Browser authentication returns `ErrOpenConnectAuthenticationRequired`; obtain a supported cookie in the host or use a form-based gateway. The public Go host types do not expose SagerNet types to Swift or C.
