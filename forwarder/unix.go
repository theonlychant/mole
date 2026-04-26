package forwarder

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
)

// StartUnixToTCP listens on a local TCP port and forwards each connection to
// a Unix domain socket path on the same machine. Run this on the AMD GPU host
// alongside amdinfer_server to expose the Unix control socket over TCP so that
// a remote mole instance (or amdinfer_ctl) can reach it.
//
//	mole --proto unix-server --from 9090 --to /tmp/amdinfer.sock
func StartUnixToTCP(localPort int, socketPath string) error {
	addr := fmt.Sprintf(":%d", localPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("tcp listen on %s: %w", addr, err)
	}
	defer ln.Close()
	log.Printf("[unix-server] listening on TCP %s, forwarding to unix:%s", addr, socketPath)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("[unix-server] accept error: %v", err)
			continue
		}
		go bridgeTCPToUnix(conn, socketPath)
	}
}

func bridgeTCPToUnix(src net.Conn, socketPath string) {
	defer src.Close()
	log.Printf("[unix-server] new connection from %s -> unix:%s", src.RemoteAddr(), socketPath)

	dst, err := net.Dial("unix", socketPath)
	if err != nil {
		log.Printf("[unix-server] dial unix:%s: %v", socketPath, err)
		return
	}
	defer dst.Close()

	pipe(src, dst, fmt.Sprintf("unix:%s", socketPath))
}

// StartTCPToUnix listens on a local TCP port and, for each connection, dials
// a remote TCP address that itself bridges to the AMD-NFS Unix socket. Run
// this on your local/dev machine so amdinfer_ctl can talk to the remote server.
//
//	mole --proto unix-client --from 9091 --to <gpu-host>:9090
//
// Then point amdinfer_ctl at the local TCP port:
//
//	amdinfer_ctl -socket /dev/tcp/... (use socat or nc to bridge locally)
func StartTCPToTCPRelay(localPort int, remoteAddr string) error {
	// This is just TCP->TCP; it reuses StartTCP. Kept here for documentation.
	return StartTCP(localPort, remoteAddr)
}

func pipe(a, b net.Conn, label string) {
	done := make(chan struct{}, 2)
	go func() {
		n, _ := io.Copy(b, a)
		log.Printf("[pipe] -> %s: %d bytes", label, n)
		done <- struct{}{}
	}()
	go func() {
		n, _ := io.Copy(a, b)
		log.Printf("[pipe] <- %s: %d bytes", label, n)
		done <- struct{}{}
	}()
	<-done
}

// StartUnixListener listens on a Unix domain socket and forwards each
// connection to a remote TCP address. Run this on your local machine to let
// amdinfer_ctl (which dials /tmp/amdinfer.sock) transparently reach the remote
// GPU host's control port through mole.
//
//	mole --proto unix-proxy --from /tmp/amdinfer.sock --to <gpu-host>:9090
func StartUnixListener(socketPath string, remoteTCP string) error {
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale socket %s: %w", socketPath, err)
	}

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("unix listen on %s: %w", socketPath, err)
	}
	defer ln.Close()
	log.Printf("[unix-proxy] listening on unix:%s, forwarding to TCP %s", socketPath, remoteTCP)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("[unix-proxy] accept error: %v", err)
			continue
		}
		go bridgeUnixToTCP(conn, remoteTCP)
	}
}

func bridgeUnixToTCP(src net.Conn, remoteTCP string) {
	defer src.Close()
	log.Printf("[unix-proxy] new connection on unix socket -> TCP %s", remoteTCP)

	dst, err := net.Dial("tcp", remoteTCP)
	if err != nil {
		log.Printf("[unix-proxy] dial tcp %s: %v", remoteTCP, err)
		return
	}
	defer dst.Close()

	pipe(src, dst, fmt.Sprintf("tcp:%s", remoteTCP))
}
