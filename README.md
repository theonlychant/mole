# mole

A simple TCP/UDP/Unix port forwarder written in Go. Zero external dependencies.

## Usage

```
mole --proto <mode> --from <local> --to <remote>
mole --config <file.yaml>
```

### Modes

| Mode | Description |
|------|-------------|
| `tcp` | Forward TCP connections from a local port to `host:port` |
| `udp` | Forward UDP packets from a local port to `host:port` |
| `unix-server` | **GPU host side** — accept TCP on `--from` port, pipe into Unix socket `--to` |
| `unix-proxy` | **Local machine side** — create Unix socket `--from`, tunnel to TCP `--to` |

### Examples

```bash
# Basic TCP forward
mole --proto tcp --from 8080 --to 192.168.1.10:9090

# Basic UDP forward
mole --proto udp --from 5353 --to 8.8.8.8:53

# Multi-rule config file
mole --config amdinfs.yaml
```

## Build

```bash
go build -o mole .
```

## Install

```bash
go install github.com/theonlychant/mole@latest
```

## Project Structure

```
mole/
├── main.go             # CLI entry point
├── go.mod
├── amdinfs.yaml        # Example config for AMD-NFS
├── config/
│   └── config.go       # YAML config loader (no external deps)
└── forwarder/
    ├── tcp.go          # TCP forwarding
    ├── udp.go          # UDP forwarding
    └── unix.go         # Unix socket <-> TCP bridging
```

---

## AMD-NFS Integration

[AMD-NFS](https://github.com/theonlychant/AMD-NFS) is an AMD GPU inference server that exposes two channels:

| Channel | Address | Purpose |
|---------|---------|---------|
| HTTP inference API | TCP `:8080` | Inference requests |
| Control socket | `/tmp/amdinfer.sock` (Unix) | `amdinfer_ctl` commands (boost, ping, status, markers) |

The Unix socket is local-only. Mole bridges it over TCP so you can run `amdinfer_ctl` from a remote machine.

### Setup

**On the GPU host** — start `amdinfer_server`, then run mole:

```bash
mole --config amdinfs.yaml
```

This starts two forwarders:
- TCP `:8080` → `localhost:8080` (HTTP API relay)
- TCP `:9090` → `/tmp/amdinfer.sock` (Unix control socket exposed over TCP)

**On your local machine** — create a local Unix socket that tunnels to the GPU host:

```bash
mole --proto unix-proxy --from /tmp/amdinfer.sock --to <gpu-host>:9090
```

Now `amdinfer_ctl` works on your local machine as if it were on the GPU host:

```bash
amdinfer_ctl -cmd ping
amdinfer_ctl -cmd status
amdinfer_ctl -cmd boost -mode fastmath
amdinfer_ctl -cmd marker -marker start_gen
```

And HTTP inference requests go through as normal:

```bash
curl http://localhost:8080/
```

### Config file (`amdinfs.yaml`)

```yaml
rules:
  - proto: tcp
    from: "8080"
    to: localhost:8080

  - proto: unix-server
    from: "9090"
    to: /tmp/amdinfer.sock
```

---

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--proto` | — | Mode: `tcp`, `udp`, `unix-server`, `unix-proxy` |
| `--from` | *(required)* | Local port number, or Unix socket path (`unix-proxy`) |
| `--to` | *(required)* | Remote `host:port`, or Unix socket path (`unix-server`) |
| `--config` | — | Path to YAML config file (runs all rules concurrently) |
