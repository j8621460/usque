# usque — Code Overview and How It Works

This document explains the high-level architecture and the main components of the usque repository (Open-source reimplementation of the Cloudflare WARP client's MASQUE protocol). It focuses on the code found in the repository root and the key internal packages.

## Project purpose

usque implements MASQUE-like functionality and includes networking primitives such as:
- QUIC helpers and MASQUE-compatible configuration.
- A local SOCKS5 and HTTP proxy that can send traffic through a tunnel (a netstack-backed network).
- DNS resolution either via local system resolvers or through the tunnel.
- Utilities for key/certificate generation and port forwarding.

## Top-level entrypoint

- `main.go`
  - Minimal entry point that calls `cmd.Execute()` to start command-line handling (see `cmd/`).
  - Program startup and subcommand dispatch are implemented under the `cmd` package.

## Command layer (cmd/)

The `cmd` package implements CLI commands and wiring for different modes:
- Commands include creating/registering accounts, enrolling, starting proxies (HTTP/SOCKS), L4 helpers, native tunnel helpers, port forwarding, etc.
- The CLI constructs configuration, creates network stack objects (when needed), constructs resolvers, and starts servers (SOCKS5/HTTP/port forwarding) using types from `internal/`.

Key files:
- `cmd/root.go` — root command and global flags.
- `cmd/socks.go` / `cmd/httpproxy.go` — proxy server frontends (start/stop server logic).
- `cmd/nativetun*.go` — OS-specific native tunnel helpers.
- `cmd/portfw.go` — port forwarding CLI and wiring.

(Refer to the `cmd/` source for detailed subcommand behavior and flags.)

## Internal packages (internal/)

This repository's networking core lives under `internal/`. Important modules:

### internal/dns.go
- Provides `TunnelDNSResolver` which can:
  - Resolve names using the system `net.DefaultResolver` (when `UseOSResolver`).
  - Resolve names by sending DNS queries to one or more configured DNS servers, either over the system network or over a tunnel-backed netstack (`*netstack.Net`).
- Resolution approach:
  - For tunnel resolution, a custom net.Resolver with Dial that uses the tunnel `tunNet.DialContext("udp", dnsHost)` is created.
  - It queries multiple configured resolvers in parallel and returns the first successful result (with per-server timeouts).
- Helpers:
  - `NewNetstackResolver`, `NewStaticResolver`, and `GetProxyResolver` produce resolvers appropriate for proxy CONNECT handling and CLI flags.

Purpose: Ensure hostname -> IP lookups can be performed through the same tunnel used for proxied traffic (MASQUE semantics) or through the host OS when requested.

### internal/socks5.go
- Wraps and customizes `github.com/txthinking/socks5` to integrate with the tunnel netstack and the resolver.
- `SOCKS5Config` configures listening address, optional username/password, resolver, netstack (`TunNet`), custom Dial function, timeouts, and logger.
- `NewSOCKS5Server(cfg)`:
  - Sets global package-level DialTCP/DialUDP to point to the server's `dialTCP`/`dialUDP` functions, so SOCKS5 traffic can be dialed either via the tunnel or via a custom dial function.
  - Can run in `TCPOnly` mode (disable UDP ASSOCIATE).
- listen/serve behavior:
  - Mirrors the upstream `socks5.Server.ListenAndServe` but improves memory allocation behavior for UDP by using buffer pools and semaphores to limit concurrency.
  - Uses sync.Pool buffers to avoid allocating 64 KiB slices per UDP packet; uses semaphores to bound the number of concurrent client handlers and relays to keep heap usage bounded.
- Dial semantics:
  - `dialTCP`/`dialUDP` will:
    - If a custom `DialTCP` is set, use it.
    - Otherwise, when using tunnel DNS and netstack, resolve and dial through `TunNet`.
    - For domain names, run resolver lookups via `TunnelDNSResolver` then dial using `TunNet` with the resolved IP.
- TCP relay:
  - `relayTCP` performs copy loops between the client and remote connection using smaller pooled buffers and optional read timeouts.
- UDP relay:
  - `UDPHandle` sets up `UDPExchange` flows for client→destination and remote->client relays.
  - Uses pooled read buffers and a pooled wire buffer to build the encapsulated datagram to send back to the client, reducing per-packet heap allocations.
  - Graceful handling when UDP not associated with a TCP session (LimitUDP checks).
- Logging: helper function `logSOCKSError` normalizes several EOF/error conditions.

Purpose: Provide a memory/CPU efficient SOCKS5 server that integrates with the tunnel netstack, performs DNS resolution via the tunnel when appropriate, and supports both TCP and UDP relays with bounded resource usage.

### internal/logger.go
- Implements a `tzStampWriter` that prepends a timestamp with a timezone abbreviation to every log line (e.g., `YYYY/MM/DD HH:MM:SS TZ `).
- Exposes `InstallDefaultLogTZStamp()` to replace the stdlib logger output and also direct gVisor's logger to the same writer. This avoids ambiguous timestamps when running on systems with limited zoneinfo.

Purpose: Produce consistent, local-time-prefixed logs for tunnel-related subsystems and for the netstack debug output.

### internal/utils.go
- Misc utilities and helpers:
  - Key/certificate helpers:
    - `GenerateRandomAndroidSerial()` — random 8-byte hex serial.
    - `GenerateRandomWgPubkey()` — random 32-byte base64 string (WireGuard-like).
    - `GenerateEcKeyPair()` + `GenerateCert()` — ECDSA P-256 keypair and self-signed certificate generation (24h validity).
  - `DefaultQuicConfig(keepalivePeriod, initialPacketSize)` — returns a QUIC config with datagrams enabled, keep-alive, and optional fixed initial packet size (disables PMTU discovery if set).
  - Port mapping parsing:
    - `ParsePortMapping` parses strings of the form `[bind_address:]local_port:remote_host:remote_port`.
    - Handles IPv6 bracketed addresses, `*` -> 0.0.0.0, resolves bind addresses with `net.ResolveTCPAddr`.
    - Validation checks for port ranges and hostnames.
  - `LoginToBase64(username, password)` — base64-encode credentials for Basic auth.
  - `CheckIfname(name)` — validates network interface names with simple ASCII/length checks (warns on long or non-ASCII names).

Purpose: Provide small helpers used by the CLI and runtime for crypto, QUIC defaults, port forwarding, and validations.

## Runtime notes & resource controls

- UDP buffer pooling:
  - To avoid large heap pressure, UDP datagram read buffers and wire-frame buffers are pooled.
  - Concurrency on UDP handling is bounded with semaphores:
    - `udpClientHandleSem` bounds number of concurrent client handlers.
    - `udpRelaySem` bounds number of active remote relays.
- Timeouts:
  - TCP and UDP timeout behavior is configurable via `SOCKS5Config` fields; note integer-second truncation in the upstream library is worked around by using full durations internally.
- Integration:
  - The SOCKS5 server integrates tightly with a tunnel network represented by `golang.zx2c4.com/wireguard/tun/netstack.Net`. When provided, dialing and DNS can go via this netstack.

## Getting started (developer quick-run)
1. Build:
   - `go build ./...`
2. Run (example):
   - Start a SOCKS5 server configured to use the tunnel-resolver via the appropriate `cmd` subcommand; see `cmd/socks.go` flags for `--dns`, `--local-dns`, `--system-dns`, `--tun` options.
3. Logs:
   - `InstallDefaultLogTZStamp()` is used to ensure logs are timestamped with local TZ.

## Where to look in the source
- Entry/CLI: `cmd/`
- Tunnel integration, proxies, and helpers: `internal/`
  - `internal/dns.go`
  - `internal/socks5.go`
  - `internal/utils.go`
  - `internal/logger.go`
- Documentation and research notes: `README.md`, `RESEARCH.md`

## Suggestions for contributors
- Test UDP-heavy workloads to validate semaphore pool sizes and memory usage.
- Improve `isValidHostname` with a robust RFC-compliant check (current implementation is simplistic).
- Add unit tests for `parsePortMapping` edge cases (IPv6, wildcard bind, hostnames).
