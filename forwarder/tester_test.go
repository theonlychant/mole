package forwarder_test

import (
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/theonlychant/mole/forwarder"
)

func startEchoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("echo server listen: %v", err)
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 4096)
				n, _ := c.Read(buf)
				c.Write(buf[:n])
			}(conn)
		}
	}()

	t.Cleanup(func() { ln.Close() })
	return ln.Addr().String()
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	defer ln.Close()
	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split hostport: %v", err)
	}
	p, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("atoi port: %v", err)
	}
	return p
}

func TestTCPForwarder_StartAndAcceptsConnections(t *testing.T) {
	echoAddr := startEchoServer(t)
	port := freePort(t)

	go func() {
		// run the forwarder in a goroutine; it listens forever until the test ends
		_ = forwarder.StartTCP(port, echoAddr)
	}()

	time.Sleep(20 * time.Millisecond)

	addr := "127.0.0.1:" + strconv.Itoa(port)
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial forwarder: %v", err)
	}
	defer conn.Close()

	msg := []byte("hello mole")
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("write: %v", err)
	}

	buf := make([]byte, len(msg))
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if string(buf[:n]) != string(msg) {
		t.Errorf("expected %q, got %q", msg, buf[:n])
	}
}

func TestTCPForwarder_BadTarget(t *testing.T) {
	port := freePort(t)

	go func() {
		_ = forwarder.StartTCP(port, "127.0.0.1:1")
	}()

	time.Sleep(20 * time.Millisecond)

	addr := "127.0.0.1:" + strconv.Itoa(port)
	conn, err := net.DialTimeout("tcp", addr, 1*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 4)
	_, err = conn.Read(buf)
	if err == nil {
		t.Error("expected read to fail when the target is unreachable, but it succeeded")
	}
}

// UDP helpers and tests
func startUDPEchoServer(t *testing.T) string {
	t.Helper()
	addr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatalf("udp echo listen: %v", err)
	}

	go func() {
		buf := make([]byte, 65535)
		for {
			n, remote, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			conn.WriteToUDP(buf[:n], remote)
		}
	}()

	t.Cleanup(func() { conn.Close() })
	return conn.LocalAddr().String()
}

func freeUDPPort(t *testing.T) int {
	t.Helper()
	addr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatalf("freeUDPPort: %v", err)
	}
	defer conn.Close()
	_, portStr, err := net.SplitHostPort(conn.LocalAddr().String())
	if err != nil {
		t.Fatalf("split hostport udp: %v", err)
	}
	p, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("atoi udp port: %v", err)
	}
	return p
}

func TestUDPForwarder_ForwarderAndReceiver(t *testing.T) {
	echoAddr := startUDPEchoServer(t)
	port := freeUDPPort(t)

	go func() {
		_ = forwarder.StartUDP(port, echoAddr)
	}()

	time.Sleep(20 * time.Millisecond)

	raddr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:"+strconv.Itoa(port))
	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		t.Fatalf("dial udp forwarder: %v", err)
	}
	defer conn.Close()

	msg := []byte("mole udp test")
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("udp write: %v", err)
	}

	buf := make([]byte, len(msg))
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("udp read: %v", err)
	}
	if string(buf[:n]) != string(msg) {
		t.Fatalf("udp expected %q got %q", msg, buf[:n])
	}
}
