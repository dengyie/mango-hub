<!-- markdownlint-disable-file MD033 MD045 -->
<div align="center">

# MangoHub

**面向云原生与边缘基础设施的企业级可观测体系、零信任 SSH 密钥金库与高吞吐指标引擎**

[![License](https://img.shields.io/badge/license-MIT-green.svg?style=flat-square)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8.svg?style=flat-square&logo=go&logoColor=white)](https://go.dev/)
[![Docker](https://img.shields.io/badge/Docker-ghcr.io-2496ED.svg?style=flat-square&logo=docker&logoColor=white)](https://github.com/dengyie/mango-hub/pkgs/container/mangohub)
[![TypeScript](https://img.shields.io/badge/TypeScript-Next.js-3178C6.svg?style=flat-square&logo=typescript&logoColor=white)](https://nextjs.org/)
[![Architecture](https://img.shields.io/badge/Architecture-Monorepo-orange.svg?style=flat-square)](#仓库架构拓扑)
[![Tests](https://img.shields.io/badge/tests-passing-brightgreen.svg?style=flat-square)]()

[English](./README.md) · [简体中文](./README.zh-CN.md) · [核心特性](#核心特性) · [仓库架构](#仓库架构拓扑) · [快速上手](#快速上手) · [零信任金库](#零信任-ssh-密钥金库) · [配置参考](#配置参数参考)

<br>

<p align="center">
  <b>彻底消减分布式边缘节点长期凭证泄露隐患与监控代理过重损耗的两难困境。</b><br>
  在高性能 Go Monorepo 中深度融汇亚秒级指标轮询、cgroup v2 容器分层配额感知与零信任动态 SSH 租约下发机制。<br>
  内置 3D 正交地球仪可视化拓扑、支持跨 Linux 与 Windows ConPTY 的无缝网页交互式终端，以及滑动窗口防暴力破解装甲。
</p>

</div>

---

## 痛点与架构对比

传统服务器监控方案常陷入两难抉择：要么依赖重型探针与臃肿的第三方运行环境造成资源争抢，要么迫使运维团队维护散落的静态长期 SSH 密钥带来极高的安全供应链隐患。MangoHub 兼具 Go 原生低损耗采样与零信任动态凭据管理：

| 评估维度 | MangoHub 综合平台 | Prometheus + Node Exporter | Netdata Cloud | 商业闭源 APM SaaS |
| :--- | :--- | :--- | :--- | :--- |
| **探针资源占用** | 极简 Go 原生二进制（<15MB 内存），零外部依赖 | 需搭配 Prometheus 抓取端与导出器 | 内存占用高，深度绑定云服务 | 庞大私有探针，厂商生态锁定 |
| **集群 SSH 纳管** | 零信任临时租约 + 动态注入/自愈吊销 | 无（仅负责指标采集） | 无 | 昂贵的商业网页堡垒机 |
| **容器配额感知** | 原生穿透 cgroup v2（Docker/Podman/LXC/Pterodactyl） | 需额外配置 cAdvisor 等容器探针 | 部分容器支持 | 需付费加装容器监控组件 |
| **控制台交付** | 单二进制内置嵌入式 Next.js Deer 主题 + 3D 地球 | 依赖独立部署与维护 Grafana | 内置基础网页控制台 | 纯云端中心化控制台 |
| **防爆破装甲** | 识别反代真实 IP 的滑动窗口自适应防暴力破解 | 依赖外置 Nginx 或 Fail2ban 过滤 | 基础 HTTP 认证 | 云厂商高价 WAF 增值包 |
| **部署私密性** | 100% 自主可控、单文件分发、支持多种主流数据库 | 需多套组件协同编排 | 必须注册商业云端以使用完整功能 | 纯 SaaS 托管订阅 |

---

## 核心特性

| 模块 | 特性亮点 | 技术实现与工程优势 |
| :--- | :--- | :--- |
| **超低损耗指标轮询** | 毫秒级多维系统状态抓取 | 亚秒级实时指标流式推送，深度解析 `/proc/diskstats` 块设备与挂载点，支持多路径网络连通性探测。 |
| **Fleet SSH 零信任金库**| 临时租约注入与到期自愈 | 内存即时签发 Ed25519 临时密钥对与 PBKDF2 强加密；原子写入 `authorized_keys` 并自动过期清理。 |
| **分层容器配额感知** | 杜绝容器 CPU 抑制误报 | 原生遍历 Docker、Podman、LXC 与 Pterodactyl 的 cgroup v2 资源边界，精准呈现真实的核数配额。 |
| **低延迟网页交互终端** | 原生 PTY 跨平台双向透传 | 完美支持 Linux/macOS Unix PTY 以及 Windows ConPTY，支持完整 ANSI 彩色转义序列与窗口尺寸自适应。 |
| **边缘安全防爆破** | 穿透 CDN 的真实 IP 限流 | 自适应滑动窗口熔断机制，精确提取真实客户端 IP，有效抵御针对管理端点与 API 的暴力破解攻击。 |
| **算力与硬件负载监管** | 硬件级遥测与进程组管理 | 内置算力负载采集通道，实时监测哈希率、核心温度与功耗，提供进程树安全超时清理与僵尸进程回收。 |
| **3D 正交地球仪视效** | 实时全球网络状态沙盘 | 原生集成现代 Next.js 3D 正交地球仪，动态呈现节点地理分布、端到端延迟热力线以及区域故障告警。 |
| **统一 Monorepo 架构** | 探针与主控端工程闭环 | 借助 Go 工作区模式（`go.work`）维护一致的 RPC 通信契约、共享加密算法库与一键端到端编译。 |

---

## 仓库架构拓扑

本项目采用统一 Monorepo 组织结构：

```text
mangohub/
├── agent/            # 节点探针与零信任执行守护进程
│   ├── cmd/          # Cobra 命令行接口
│   ├── monitoring/   # 指标收集核心（CPU、内存、磁盘 IO、网络、Ping）
│   ├── sshauth/      # 零信任 authorized_keys 租约与 ACL 硬化
│   ├── terminal/     # 交互式网页终端桥接
│   └── update/       # 客户端自更新流水线
├── hub/              # 集中控制主服务与 Web 面板
│   ├── cmd/          # 服务端启动与维护 CLI 命令
│   ├── database/     # SQLite / MySQL / PostgreSQL 数据库模型
│   ├── internal/     # 服务端核心（SSH 金库、防爆破限流器、鉴权中间件）
│   ├── pkg/          # RPC 通信协议、JS 运行时引擎、指标处理引擎
│   └── web/          # 静态资源服务与内置 Deer 主题
├── go.work           # Go 多模块工作区配置
├── Makefile          # 统一构建与测试入口
└── LICENSE           # MIT 开源许可证
```

---

## 快速上手

### 环境要求
- Go 1.24+（运行 Hub 推荐 Go 1.25+）
- Node.js 20+（仅在重新构建前端静态资源时需要）
- GCC / Clang / Zig（用于编译支持 SQLite 的 CGO 二进制）

### 1. 源码编译

```bash
# 克隆代码仓库
git clone https://github.com/dengyie/mango-hub.git
cd mango-hub

# 统一编译探针与服务端
make all

# 或按需单独编译:
make hub      # 产物路径: ./bin/mangohub
make agent    # 产物路径: ./bin/mangohub-agent
```

### 2. 运行 MangoHub 服务端

```bash
# 使用内置 SQLite 启动服务端
./bin/mangohub server

# 或自定义监听地址与端口
MANGOHUB_LISTEN="0.0.0.0:25774" ./bin/mangohub server
```

服务启动后，在浏览器中访问控制台：`http://localhost:25774`。

### 3. 部署 MangoHub 探针

```bash
./bin/mangohub-agent --endpoint "http://your-hub-ip:25774" --token "your-agent-token"
```

---

## 零信任 SSH 密钥金库

Fleet SSH 金库彻底杜绝在多节点共享密码或留存长期密钥：

```text
运维人员浏览器 / 终端
  │
  ▼  1. 携带目标节点 ID 申请临时登录租约
MangoHub 主控服务 (Hub)
  │  2. 在内存中即时生成 Ed25519 临时密钥对
  │  3. 签发带有时效限制（TTL）的动态租约令牌
  ▼
MangoHub 探针端 (目标节点)
  │  4. 原子注入公钥至 authorized_keys (校验 StrictModes 与 Windows ACLs)
  │  5. 经由加密 WebSocket 建立 PTY 终端通道
  ▼
会话断开 / 租约到期
  │  6. 探针端自动删除注入的临时公钥并回滚权限
  └─► 主控端完整记录会话审计日志与操作人身份
```

---

## 配置参数参考

| 环境变量 | 默认值 | 配置说明 |
| :--- | :--- | :--- |
| `MANGOHUB_LISTEN` | `0.0.0.0:25774` | 主控服务端监听的 IP 地址与端口号。 |
| `MANGOHUB_DB_TYPE` | `sqlite` | 后端数据库驱动类型（`sqlite`、`mysql`、`postgres`）。 |
| `MANGOHUB_DB_DSN` | `data/mangohub.db` | 数据库连接字符串或本地数据文件保存路径。 |
| `MANGOHUB_JWT_SECRET` | 随机生成 | 管理员用户会话令牌签发的私有安全密钥。 |
| `MANGOHUB_LOG_LEVEL` | `info` | 日志输出级别（`debug`、`info`、`warn`、`error`）。 |
| `MANGOHUB_AGENT_TOKEN` | 必填 | 授权探针节点接入注册的预共享令牌。 |

---

## 参与贡献与许可证

欢迎提交 Issue 与 Pull Request！在提交代码改动前，请确保全量 Go 测试套件通过：

```bash
go test ./agent/... ./hub/...
```

本项目采用 [MIT License](LICENSE) 开源协议。
