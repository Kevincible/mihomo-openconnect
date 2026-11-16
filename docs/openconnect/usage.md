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

Timeout options accept 0 for default and otherwise 1–86400 seconds. The global DNS policy handles application hostname resolution. Configure company DNS with `#OPENCONNECT` to carry queries through the VPN; use an independent `proxy-server-nameserver` for the VPN gateway. Do not point gateway DNS back through the VPN it is trying to establish. Mihomo's proxy cycle validation and the adapter's self-reference check reject cyclic proxy chains.

Configure a company group without `DIRECT` fallback. Keep all existing company domain and network rules, including exclusions. The adapter deliberately does not interpret normalized library routes as authoritative routing policy: the library does not expose all route provenance needed for lossless dynamic rules.

## Session ownership

Each proxy owns one library client and one IP stack. Construction does not authenticate or change OS routing. The first request starts the client once. Connection contexts only govern their own waits; `Close()` governs the shared session and joins the adapter's workers. Authentication and certificate errors remain associated with this outbound; reload the proxy with corrected credentials to start a new client after terminal failure.

The current tunnel address and MTU must match the active stack before a connection is returned. Configuration notifications are asynchronous, so queued notification payloads are never used to roll back a newer configuration. Packet reads that span a stack replacement are discarded; IP source/destination checks prevent packets for a previous address from entering the new stack. Packet memory is copied through the library's public API and its queue remains bounded.

Protocol keepalive, backoff, DTLS fallback and reconnection belong to sing-openconnect. Transport interruptions can lose UDP packets and TCP may retransmit. If addresses or MTU change, existing sockets are closed; this is not seamless migration. Rekeying with unchanged addresses/MTU retains the stack.

## Host-managed authentication

Create `outbound.OpenConnectOption` programmatically and set `AuthHandler` (excluded from YAML decoding). Implement:

```go
AuthenticateOpenConnect(ctx context.Context,
    challenge outbound.OpenConnectAuthChallenge) (map[string]string, error)
```

Return values keyed by each field's `Key`; use `Kind`, `Label` and `Choices` to present forms or retrieve credentials. The handler must return when `ctx` is canceled, otherwise host code can delay `Close()`. No keys, passwords, cookies, form values or TOTP secrets are logged by the adapter or serialized in proxy JSON. A host should protect its configuration and avoid logging returned challenge contents.

The handler supports multiple authentication rounds and reauthentication requested by the library. Keychain access and UI belong to the host, not this package. Browser authentication returns `ErrOpenConnectAuthenticationRequired`; obtain a supported cookie in the host or use a form-based gateway. The public Go host types do not expose SagerNet types to Swift or C.
