<div align="center">

# MangoHub

**Enterprise-Grade Cloud & Edge Infrastructure Observability, Zero-Trust SSH Fleet & Metrics Engine**

[![License](https://img.shields.io/badge/license-MIT-green.svg?style=flat-square)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.24%2B-00ADD8.svg?style=flat-square&logo=go&logoColor=white)](https://go.dev/)
[![Docker Image](https://img.shields.io/badge/docker-ghcr.io%2Fmangohub-blue.svg?style=flat-square&logo=docker&logoColor=white)](https://github.com/dengyie/mango-hub/pkgs/container/mangohub)
[![Architecture](https://img.shields.io/badge/architecture-Agent%20%2B%20Hub%20Monorepo-orange.svg?style=flat-square)](#-repository-architecture)
[![Tests](https://img.shields.io/badge/tests-passing-brightgreen.svg?style=flat-square)]()

[English](./README.md) • [简体中文](./README_zh-cn.md)

<p align="center">
  <img src="./docs/images/dashboard.png" alt="MangoHub Dashboard" width="880" style="border-radius: 8px; box-shadow: 0 4px 16px rgba(0,0,0,0.12);" />
</p>

</div>

---

## 🌟 Key Features

### 1. High-Performance Telemetry & Zero-Overhead Sampling
- **Sub-second precision**: Real-time metrics streaming with minimal CPU and memory footprint.
- **Deep Disk IO Metrics**: High-accuracy IOPS, read/write throughput counters (`/proc/diskstats`), and mountpoint-to-block device mapping.
- **Hierarchical cgroup v2 & Quotas**: Built-in support for containerized quota traversal (Docker, Podman, LXC, Pterodactyl) and CPU clamp detection.
- **Resilient Connectivity**: Multi-path pinging (ICMP, TCP, HTTP proxy aware) and adaptive custom DNS resolvers.

### 2. Fleet SSH Vault (Zero-Trust Remote Access)
- **PBKDF2 Hardware-Backed Encryption**: Ephemeral keypair generation and secure credential vaulting.
- **Dynamic Lease Injection**: Automatic temporary `authorized_keys` injection and self-revocation on client nodes.
- **StrictModes & Windows ACL Hardening**: Atomic key updates, ownership preservation, and Windows access control validation.
- **Web Terminal**: Built-in interactive PTY shell supporting both Unix and Windows ConPTY.

### 3. Edge Hardening & Zero-Trust Defense
- **Adaptive Auth-Fail Rate Limiting**: Intelligent sliding-window mitigation against credential stuffing and brute-force attacks behind Cloudflare / reverse proxies.
- **Granular Role-Based Access Control**: Strict separation between public status viewing, administrative node control, and ticket-based ticket dispatching.

### 4. Advanced Computational Workload & Miner Observability
- **Real-Time Telemetry Polling**: Integrated collector for computational workloads (e.g., SRBMiner) reporting hashrate, power consumption, and thermal data.
- **Process Supervisor & Lifetime Management**: Controlled startup, timeout-bounded shutdown, and process group process tree reaping.

### 5. Next-Gen 3D Interactive Visualization
- **Built-in Deer Theme**: Modern Next.js UI embedded directly into the binary with zero runtime dependencies.
- **3D Orthographic Globe**: Interactive rotatable globe depicting node locations, latencies, and partial network outages.
- **Responsive Dashboard**: Live resource curves, popover inspect panels, and customizable layouts.

---

## 🏗️ Repository Architecture

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

## 🚀 Quick Start

### Prerequisites
- Go 1.24+ (Go 1.25+ recommended for Hub)
- Node.js 20+ (optional, only needed if rebuilding web UI)
- GCC / Clang / Zig (for CGO SQLite builds)

### 1. Build from Source

```bash
# Clone the repository
git clone https://github.com/dengyie/mangohub.git
cd mangohub

# Build both Agent and Hub
make all

# Or build individually:
make hub      # Output: ./bin/mangohub
make agent    # Output: ./bin/mangohub-agent
```

### 2. Run Mangohub Server

```bash
# Start server with default SQLite storage
./bin/mangohub server

# Or customize listen address
MANGOHUB_LISTEN="0.0.0.0:25774" ./bin/mangohub server
```

Access the dashboard at `http://localhost:25774`.

### 3. Deploy Mangohub Agent

```bash
./bin/mangohub-agent --endpoint "http://your-hub-ip:25774" --token "your-agent-token"
```

---

## 📄 License

Mangohub is released under the [MIT License](./LICENSE).
