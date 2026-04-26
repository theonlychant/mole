package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/theonlychant/mole/config"
	"github.com/theonlychant/mole/forwarder"
)

func main() {
	proto := flag.String("proto", "", "Protocol mode (see below)")
	from := flag.String("from", "", "Local port number (tcp/udp/unix-server) or Unix socket path (unix-proxy)")
	to := flag.String("to", "", "Remote address host:port, or Unix socket path (unix-server)")
	cfg := flag.String("config", "", "Path to YAML config file (runs multiple rules)")
	flag.Usage = usage
	flag.Parse()

	// Config-file mode: ignore other flags and run all rules.
	if *cfg != "" {
		rules, err := config.Load(*cfg)
		if err != nil {
			log.Fatalf("config: %v", err)
		}
		log.Printf("mole: loaded %d rule(s) from %s", len(rules), *cfg)
		runRules(rules)
		return
	}

	if *proto == "" || *from == "" || *to == "" {
		flag.Usage()
		os.Exit(1)
	}

	*proto = strings.ToLower(*proto)
	runSingle(*proto, *from, *to)
}

func runSingle(proto, from, to string) {
	log.Printf("mole: %s  %s -> %s", proto, from, to)
	var err error
	switch proto {
	case "tcp":
		port := mustPort(from)
		err = forwarder.StartTCP(port, to)
	case "udp":
		port := mustPort(from)
		err = forwarder.StartUDP(port, to)

	// unix-server: run on the GPU host.
	//   Listens on a TCP port and pipes each connection into the local Unix socket.
	//   e.g. --proto unix-server --from 9090 --to /tmp/amdinfer.sock
	case "unix-server":
		port := mustPort(from)
		err = forwarder.StartUnixToTCP(port, to)

	// unix-proxy: run on your local/dev machine.
	//   Creates a local Unix socket and tunnels it to a remote TCP port
	//   (which should be served by mole in unix-server mode on the GPU host).
	//   e.g. --proto unix-proxy --from /tmp/amdinfer.sock --to gpu-host:9090
	case "unix-proxy":
		err = forwarder.StartUnixListener(from, to)

	default:
		log.Fatalf("unsupported protocol %q — valid values: tcp, udp, unix-server, unix-proxy", proto)
	}
	if err != nil {
		log.Fatal(err)
	}
}

func runRules(rules []config.Rule) {
	done := make(chan error, len(rules))
	for _, r := range rules {
		r := r
		go func() {
			done <- runRuleErr(r)
		}()
	}
	// Block until any rule fails.
	if err := <-done; err != nil {
		log.Fatal(err)
	}
}

func runRuleErr(r config.Rule) error {
	log.Printf("mole: [%s] %s -> %s", r.Proto, r.From, r.To)
	switch strings.ToLower(r.Proto) {
	case "tcp":
		return forwarder.StartTCP(mustPort(r.From), r.To)
	case "udp":
		return forwarder.StartUDP(mustPort(r.From), r.To)
	case "unix-server":
		return forwarder.StartUnixToTCP(mustPort(r.From), r.To)
	case "unix-proxy":
		return forwarder.StartUnixListener(r.From, r.To)
	default:
		return fmt.Errorf("unknown proto %q in config", r.Proto)
	}
}

func mustPort(s string) int {
	var p int
	if _, err := fmt.Sscanf(s, "%d", &p); err != nil || p < 1 || p > 65535 {
		log.Fatalf("invalid port %q", s)
	}
	return p
}

func usage() {
	fmt.Fprintf(os.Stderr, `mole — TCP/UDP/Unix port forwarder

Usage:
  mole --proto <mode> --from <local> --to <remote>
  mole --config <file.yaml>

Modes:
  tcp          Forward TCP connections from a local port to host:port
  udp          Forward UDP packets from a local port to host:port
  unix-server  Run on the GPU host: accept TCP on <from> port, pipe to Unix socket <to>
  unix-proxy   Run on local machine: create Unix socket <from>, tunnel to TCP <to>

Examples (AMD-NFS):
  # On the GPU host — forward inference HTTP and expose Unix control socket over TCP:
  mole --config amdinfs.yaml

  # Or individually:
  mole --proto tcp          --from 8080 --to localhost:8080
  mole --proto unix-server  --from 9090 --to /tmp/amdinfer.sock

  # On your local machine — reach the GPU host's control socket as if it were local:
  mole --proto unix-proxy   --from /tmp/amdinfer.sock --to gpu-host:9090

Flags:
`)
	flag.PrintDefaults()
}
