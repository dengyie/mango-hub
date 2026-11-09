<!-- markdownlint-disable-file MD033 MD045 -->
<div align="center">

# MangoHub

**Enterprise Cloud & Edge Infrastructure Observability, Zero-Trust SSH Fleet & High-Throughput Metrics Engine**

[![License](https://img.shields.io/badge/license-MIT-green.svg?style=flat-square)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg?style=flat-square&logo=go&logoColor=white)](https://go.dev/)
[![Docker](https://img.shields.io/badge/Docker-ghcr.io-2496ED.svg?style=flat-square&logo=docker&logoColor=white)](https://github.com/dengyie/mango-hub/pkgs/container/mangohub)
[![TypeScript](https://img.shields.io/badge/TypeScript-Next.js-3178C6.svg?style=flat-square&logo=typescript&logoColor=white)](https://nextjs.org/)
[![Architecture](https://img.shields.io/badge/Architecture-Monorepo-orange.svg?style=flat-square)](#repository-architecture)
[![Tests](https://img.shields.io/badge/tests-passing-brightgreen.svg?style=flat-square)]()

[English](./README.md) · [简体中文](./README.zh-CN.md) · [Core Features](#core-features) · [Architecture](#repository-architecture) · [Quick Start](#quick-start) · [Zero-Trust SSH](#zero-trust-ssh-vault) · [API & Config](#configuration-reference)

<br>

<p align="center">
  <b>Terminates credential leakage hazards and performance bottlenecks across distributed edge server fleets.</b><br>
  Unifies sub-second telemetry sampling, cgroup v2 container quota awareness, and zero-trust dynamic SSH lease injection in a high-concurrency Go monorepo.<br>
  Features embedded 3D orthographic globe visualizations, integrated PTY web shells for Linux and Windows ConPTY, and sliding-window anti-brute-force defense.
</p>

</div>

---

## Motivation & Architecture Comparison

Traditional server monitoring tools either rely on bloated monitoring agents with heavy runtime dependencies, or force operators to manage static, long-lived SSH credentials that pose severe supply-chain risks. MangoHub eliminates both dilemmas by pairing low-overhead native Go sampling with hardware-hardened zero-trust SSH access:

| Evaluation Dimension | MangoHub Platform | Prometheus + Node Exporter | Netdata Cloud | Commercial APM SaaS |
| :--- | :--- | :--- | :--- | :--- |
| **Agent Footprint** | Ultralight native Go binary (<15MB RSS), zero external deps | Requires Prometheus scrape server + exporter | High memory consumption, cloud dependency | Heavy proprietary agents, vendor lock-in |
| **Fleet SSH Access** | Zero-trust ephemeral leases + dynamic `authorized_keys` injection | None (metrics collection only) | None | Closed-source browser bastions ($$$) |
| **Container Quota Awareness**| Native cgroup v2 traversal (Docker, Podman, LXC, Pterodactyl) | Manual container exporter orchestration | Partial container metrics | Additional container agent required |
| **Web UI Delivery** | Single-binary embedded Next.js Deer theme + 3D globe | Requires separate Grafana deployment | Embedded web UI | Cloud dashboard only |
| **Anti-Brute-Force Guard** | Sliding-window auth-fail rate limiting behind Cloudflare / proxies | Relies on external Nginx/Fail2ban | Basic web auth | Cloud WAF add-on |
| **Deployment Model** | 100% self-hosted, single binary, multi-database support | Complex multi-tier infrastructure | Cloud account required for full features | SaaS subscription required |

---

## Core Features

| Module | Feature Highlight | Technical Implementation & Advantage |
| :--- | :--- | :--- |
| **Sub-Second Telemetry** | Low-overhead system sampling | Sub-second metric streaming with minimal CPU footprint, `/proc/diskstats` block mapping, and multi-path network probing. |
| **Zero-Trust SSH Vault** | Ephemeral access with auto-revocation | Dynamic keypair generation with PBKDF2 encryption; temporary `authorized_keys` leases injected and purged automatically. |
| **Container Awareness** | cgroup v2 quota & clamp detection | Traverses hierarchical cgroup v2 limits across Docker, Podman, LXC, and Pterodactyl, preventing false CPU throttling alerts. |
| **Interactive PTY Shell** | Low-latency in-browser terminal | Native terminal emulation supporting both Unix PTY and Windows ConPTY with full ANSI escape sequence rendering. |
| **Edge Hardening** | Sliding-window brute-force mitigation | Adaptive rate limiting recognizing real client IPs behind reverse proxies and CDNs, protecting login endpoints. |
| **Workload Observability** | Computational hardware telemetry | Built-in polling for computational engines reporting hashrate, power consumption, core temperature, and process trees. |
| **3D Orthographic Globe** | Real-time global network visualization | Embedded Next.js 3D globe rendering geographic node locations, latency topology, and regional outage alarms. |
| **Monorepo Architecture** | Unified control plane & agent codebase | Go multi-module workspace (`go.work`) facilitating unified builds, shared protocols, and instant end-to-end tests. |

---

## Repository Architecture

This repository is organized as a unified Go Monorepo:

```text
mangohub/
├── agent/            # Node telemetry and zero-trust management daemon
│   ├── cmd/          # Cobra CLI commands (flags, startup, service)
│   ├── monitoring/   # Metrics collectors (CPU, Memory, Disk IO, Network, Ping)
│   ├── sshauth/      # Zero-trust authorized_keys lease manager & ACLs
│   ├── terminal/     # PTY interactive shell bridge
│   └── update/       # Self-update pipeline
├── hub/              # Central control plane & API server
│   ├── cmd/          # Server startup and CLI utilities
│   ├── database/     # SQLite / MySQL / PostgreSQL GORM models & migrations
│   ├── internal/     # Server core, SSH vault, rate limiters, auth middleware
│   ├── pkg/          # RPC protocol, JS runtime engine, metrics pipeline
│   └── web/          # Static file serving and embedded Deer theme
├── go.work           # Go multi-module workspace
├── Makefile          # Unified build and test targets
└── LICENSE           # MIT License
```

---

## Quick Start

### Prerequisites
- Go 1.24+ (Go 1.25+ recommended for Hub)
- Node.js 20+ (optional, only needed if rebuilding web UI)
- GCC / Clang / Zig (for CGO SQLite builds)

### 1. Build from Source

```bash
# Clone the repository
git clone https://github.com/dengyie/mango-hub.git
cd mango-hub

# Build both Agent and Hub
make all

# Or build individually:
make hub      # Output: ./bin/mangohub
make agent    # Output: ./bin/mangohub-agent
```

### 2. Run MangoHub Server

```bash
# Start server with default SQLite storage
./bin/mangohub server

# Or customize listen address
MANGOHUB_LISTEN="0.0.0.0:25774" ./bin/mangohub server
```

Access the dashboard at `http://localhost:25774`.

### 3. Deploy MangoHub Agent

```bash
./bin/mangohub-agent --endpoint "http://your-hub-ip:25774" --token "your-agent-token"
```

---

## Zero-Trust SSH Vault

The Fleet SSH Vault eliminates shared passwords and permanent SSH keys:

```text
Operator Browser / Terminal
  │
  ▼  1. Request temporary lease with target node ID
MangoHub Central Server (Hub)
  │  2. Generate ephemeral Ed25519 keypair in memory
  │  3. Sign dynamic lease token with time-to-live (TTL)
  ▼
MangoHub Agent (Target Node)
  │  4. Atomically inject public key into authorized_keys (StrictModes / ACLs verified)
  │  5. Spawn PTY session over encrypted WebSocket channel
  ▼
Session Expiration / Disconnection
  │  6. Auto-purge injected public key from authorized_keys
  └─► Audit trail logged to database with operator identity
```

---

## Configuration Reference

| Environment Variable | Default | Purpose |
| :--- | :--- | :--- |
| `MANGOHUB_LISTEN` | `0.0.0.0:25774` | Central control plane listening address and port. |
| `MANGOHUB_DB_TYPE` | `sqlite` | Database driver (`sqlite`, `mysql`, `postgres`). |
| `MANGOHUB_DB_DSN` | `data/mangohub.db` | Database connection string or local database file path. |
| `MANGOHUB_JWT_SECRET` | Random (Generated) | Secret key used for signing administrative session tokens. |
| `MANGOHUB_LOG_LEVEL` | `info` | Logging verbosity (`debug`, `info`, `warn`, `error`). |
| `MANGOHUB_AGENT_TOKEN` | Required | Pre-shared token authorizing node agent enrollment. |

---

## Contributing & License

Contributions are welcome. Please ensure all Go test suites pass before submitting changes:

```bash
go test ./agent/... ./hub/...
```

Licensed under the [MIT License](LICENSE).
