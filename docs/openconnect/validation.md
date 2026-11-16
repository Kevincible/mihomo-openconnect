# Validation and integration scope

The release gates are defined in [Validate](../../.github/workflows/validate.yml). They verify the actual outbound against a local ocserv 1.3.0 server; credentials and certificates are generated per fixture. The image's Debian base is pinned by digest and the ocserv package version is pinned. A company VPN is never used by CI.

| Gate | Evidence |
| --- | --- |
| Parsing and safe proxy serialization | `TestOpenConnectConfig`; constructor is lazy and accepts `dtls: false` without disabling application UDP. |
| Username/password and host authentication | Real ocserv exchanges, rejection of wrong credentials, interactive password handler, TOTP against liboath, separate working session after failed auth. |
| TLS certificate verification | Locally generated CA trusted explicitly; the same gateway rejected without that CA. |
| TCP, UDP, DNS | Multi-packet TCP echo, UDP echo and DNS response transmitted to private tunnel addresses. |
| DTLS and fallback | Assert negotiated DTLS in normal operation; exercise disabled DTLS and blocked UDP transport while application UDP still works over TLS. |
| Gateway proxy chain | Real HTTP CONNECT proxy in front of the VPN; all application transports traverse that VPN. |
| Cancellation and concurrency | Eight concurrent dials after a canceled dial, one shared stack, concurrent repeated Close. |
| Reconnect and configuration | Session restart with unchanged stack, container pause/resume, server restart, address and MTU replacement, old connection closure and new traffic. |
| Data races | Integration suite runs with `-race`. |
| Existing Mihomo functions | Adapter/group/provider, configuration/cycle detection, DNS, rules and OpenVPN regression suites on Linux/macOS. |
| Targets | CLI builds for Linux/macOS/Windows amd64 and arm64, plus build without gVisor. |
| Update/release guards | Git-history tests cover old tags, branch updates, tag promotion, rewritten history, input idempotency, incomplete platform sets and mixed-source artifacts. |

Local execution on macOS arm64 also exercises the actual adapter through loopback-published Docker ocserv. Tests are limited to the relevant functionality; UI automation is not run. Workflow logs are the authoritative evidence for each published version.

## Host work outside this repository

This repository delivers the generic outbound, complete versioned Go source and CLI releases. The original plan places company policy updates, keychain/interactive UI, system service management and whale-nio's C ABI/XCFramework packaging in their existing host components.

For whale-nio, pin the source tag in `ThirdParty/MihomoCore/go.mod`, retain its protobuf replacement and build with `with_gvisor`. Use its existing c-archive → XCFramework flow so Mihomo and sing-openconnect share one Go runtime. The existing Swift/Go packet framing and packetFlow entry stack do not change. Manage the system Packet Tunnel MTU separately from the negotiated outbound VPN MTU.

Before deploying to a real company gateway or iOS, validate that environment's authentication, iOS arm64/simulator linking, company DNS/rules, ordinary proxies, sleep/wake, network transitions, resource release and peak extension memory. These checks require the host app and gateway; a CLI/ocserv result is not evidence that those environments have passed.

Dynamic routes remain a separate follow-up component: preserve route exclusions and protocol/port restrictions, use split-DNS only (not default search domains), distinguish explicit default routes from normalized defaults, validate before atomic replacement, and keep the last valid company rules after update failure or disconnect. The current library's normalized route API cannot prove every original route's provenance; this adapter therefore does not guess or install global policy. Start from the host's existing complete company rules, never an empty replacement.

CLI binary size and VPN stack memory vary with target and workload. The release manifests provide binary hashes and build inputs; iOS resource/performance claims require actual host measurements and are not inferred from CGO=0 or pure Go.
