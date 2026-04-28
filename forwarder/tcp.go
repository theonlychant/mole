// Copyright(C) 2025-2026 Advanced Micro Devices, Inc. All rights reserved.
// Author: theonlychant
package forwarder

import (
	"fmt"
	"log"
	"net"
)

// StartTCP listens on the local port and forwards all connections to target.
func StartTCP(localPort int, target string) error {
	addr := fmt.Sprintf(":%d", localPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("tcp listen on %s: %w", addr, err)
	}
	defer ln.Close()
	log.Printf("[tcp] listening on %s, forwarding to %s", addr, target)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("[tcp] accept error: %v", err)
			continue
		}
		go handleTCP(conn, target)
	}
}

func handleTCP(src net.Conn, target string) {
	defer src.Close()
	log.Printf("[tcp] new connection from %s -> %s", src.RemoteAddr(), target)

	dst, err := net.Dial("tcp", target)
	if err != nil {
		log.Printf("[tcp] dial %s: %v", target, err)
		return
	}
	defer dst.Close()

	pipe(src, dst, target)
}
