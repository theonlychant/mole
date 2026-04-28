// Copyright(C) 2025-2026 Advanced Micro Devices, Inc. All rights reserved.
// Author: theonlychant
package main

import (
	"bufio"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"
)

// AMD-build helper
// This tool provides a tiny CLI to perform common AMD host tasks over SSH:
// - run remote build commands (`build` subcommand)
// - run arbitrary remote commands (`run` subcommand)
// - start a local->remote TCP tunnel over an SSH session (`tunnel` subcommand)
//
// It intentionally depends only on the standard library + golang.org/x/crypto/ssh
// so it can be used from CI or developer machines without extra tooling.

const (
	defaultSSHPort = 22
)

func main() {
	log.SetFlags(0)

	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	cmd := os.Args[1]
	switch cmd {
	case "build":
		buildCmd(os.Args[2:])
	case "run":
		runCmd(os.Args[2:])
	case "tunnel":
		tunnelCmd(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", cmd)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `AMD-build — small SSH and tunneling helper

Usage:
  amd-build <subcommand> [flags]

Subcommands:
  build   Run a build command on a remote host over SSH (default: make)
  run     Run an arbitrary command on a remote host over SSH
  tunnel  Start a local->remote TCP tunnel via SSH

Common flags (apply to all subcommands):
  --host string      remote host (user@host or host)
  --user string      SSH username (overrides user@host)
  --port int         SSH port (default 22)
  --key string       path to private key (PEM) to use for auth
  --pass string      password for SSH auth (not recommended)

Examples:
  amd-build build --host gpu-host --key ~/.ssh/id_rsa --cmd "make all"
  amd-build run --host theonlychant@gpu-host --cmd "systemctl restart amdinfer"
  amd-build tunnel --host gpu-host --local 9090 --remote 127.0.0.1:9090 --key ~/.ssh/id_rsa
`)
}

// commonSSHFlags registers flags shared by multiple subcommands
func commonSSHFlags(fs *flag.FlagSet) (host *string, user *string, port *int, key *string, pass *string) {
	host = fs.String("host", "", "remote host (user@host or host)")
	user = fs.String("user", "", "ssh username (optional)")
	port = fs.Int("port", defaultSSHPort, "ssh port")
	key = fs.String("key", "", "path to private key file (PEM)")
	pass = fs.String("pass", "", "password for SSH auth (not recommended)")
	return
}

func buildCmd(args []string) {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	host, user, port, key, pass := commonSSHFlags(fs)
	cmd := fs.String("cmd", "make", "build command to run on remote host")
	workdir := fs.String("workdir", "", "remote working directory (optional)")
	fs.Parse(args)

	if *host == "" {
		log.Fatal("--host is required")
	}

	client, err := dialSSH(resolveUserHost(*host, *user), *port, *key, *pass)
	if err != nil {
		log.Fatalf("ssh connect: %v", err)
	}
	defer client.Close()

	fullCmd := *cmd
	if *workdir != "" {
		fullCmd = fmt.Sprintf("cd %s && %s", shellEscape(*workdir), *cmd)
	}

	out, err := runRemoteCommand(client, fullCmd)
	if err != nil {
		log.Fatalf("remote build failed: %v", err)
	}
	fmt.Print(out)
}

func runCmd(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	host, user, port, key, pass := commonSSHFlags(fs)
	cmd := fs.String("cmd", "", "command to run on remote host (required)")
	fs.Parse(args)

	if *host == "" || *cmd == "" {
		log.Fatal("--host and --cmd are required")
	}

	client, err := dialSSH(resolveUserHost(*host, *user), *port, *key, *pass)
	if err != nil {
		log.Fatalf("ssh connect: %v", err)
	}
	defer client.Close()

	out, err := runRemoteCommand(client, *cmd)
	if err != nil {
		log.Fatalf("remote command failed: %v", err)
	}
	fmt.Print(out)
}

func tunnelCmd(args []string) {
	fs := flag.NewFlagSet("tunnel", flag.ExitOnError)
	host, user, port, key, pass := commonSSHFlags(fs)
	local := fs.Int("local", 0, "local port to listen on (required)")
	remote := fs.String("remote", "", "remote address to connect to from the SSH server (host:port) (required)")
	fs.Parse(args)

	if *host == "" || *local == 0 || *remote == "" {
		log.Fatal("--host, --local and --remote are required")
	}

	client, err := dialSSH(resolveUserHost(*host, *user), *port, *key, *pass)
	if err != nil {
		log.Fatalf("ssh connect: %v", err)
	}
	defer client.Close()

	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *local))
	if err != nil {
		log.Fatalf("listen local: %v", err)
	}
	defer l.Close()

	log.Printf("listening on 127.0.0.1:%d -> remote %s via %s", *local, *remote, *host)

	// handle interrupts to close listener and ssh client cleanly
	ctx, cancel := signalContext()
	defer cancel()

	for {
		conn, err := l.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
			}
			log.Printf("accept error: %v", err)
			continue
		}
		go func(c net.Conn) {
			defer c.Close()
			// Dial from the server side to the desired remote address using the SSH
			remoteConn, err := client.Dial("tcp", *remote)
			if err != nil {
				log.Printf("ssh dial to remote %s failed: %v", *remote, err)
				return
			}
			defer remoteConn.Close()
			pipe(c, remoteConn)
		}(conn)
	}
}

// resolveUserHost handles "user@host" or separate user flag
func resolveUserHost(hostArg, userFlag string) (user, host string) {
	if strings.Contains(hostArg, "@") {
		parts := strings.SplitN(hostArg, "@", 2)
		return parts[0], parts[1]
	}
	return userFlag, hostArg
}

// dialSSH creates an SSH client using a private key or password
func dialSSH(user, host string, port int, keyPath, password string) (*ssh.Client, error) {
	if host == "" {
		return nil, errors.New("empty host")
	}
	addr := fmt.Sprintf("%s:%d", host, port)

	var auth []ssh.AuthMethod
	if keyPath != "" {
		signer, err := signerFromKeyFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("parse key: %w", err)
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if password != "" {
		auth = append(auth, ssh.Password(password))
	}
	if len(auth) == 0 {
		// try agent via SSH_AUTH_SOCK is omitted intentionally; require explicit key or pass
		return nil, errors.New("no auth method provided; use --key or --pass")
	}

	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}
	return ssh.Dial("tcp", addr, cfg)
}

func signerFromKeyFile(path string) (ssh.Signer, error) {
	b, err := ioutil.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(b)
	if err == nil {
		return signer, nil
	}
	// attempt to parse encrypted key using PEM and PKCS1 (passwordless) as a fallback
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, errors.New("invalid key format")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	return ssh.NewSignerFromKey(key)
}

// runRemoteCommand runs a command on the remote host and returns combined output
func runRemoteCommand(client *ssh.Client, command string) (string, error) {
	sess, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()

	var b strings.Builder
	sess.Stdout = &b
	sess.Stderr = &b

	if err := sess.Run(command); err != nil {
		return b.String(), err
	}
	return b.String(), nil
}

// pipe copies data between two connections until EOF
func pipe(a net.Conn, b net.Conn) {
	done := make(chan struct{}, 2)
	go func() {
		io.Copy(a, b)
		a.Close()
		done <- struct{}{}
	}()
	go func() {
		io.Copy(b, a)
		b.Close()
		done <- struct{}{}
	}()
	<-done
}

// signalContext returns a context that is canceled on SIGINT/SIGTERM
func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		cancel()
	}()
	return ctx, func() {
		signal.Stop(c)
		cancel()
	}
}

// shellEscape naively escapes a path/arg for remote shell usage
func shellEscape(s string) string {
	if s == "" {
		return ""
	}
	// simple single-quote wrapper; replace any single quotes properly
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// lightweight NewSignerFromKey wrapper for rsa key fallback
func sshNewSignerFromRSA(key *rsa.PrivateKey) (ssh.Signer, error) {
	return ssh.NewSignerFromKey(key)
}
