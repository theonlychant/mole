package forwarder

import (
	"fmt"
	"log"
	"net"
	"time"
)

const udpBufSize = 65507
const udpTimeout = 30 * time.Second

// StartUDP listens on the local UDP port and forwards packets to target.
// Replies from target are sent back to the original sender.
func StartUDP(localPort int, target string) error {
	addr := fmt.Sprintf(":%d", localPort)
	laddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return fmt.Errorf("resolve local addr %s: %w", addr, err)
	}

	conn, err := net.ListenUDP("udp", laddr)
	if err != nil {
		return fmt.Errorf("udp listen on %s: %w", addr, err)
	}
	defer conn.Close()
	log.Printf("[udp] listening on %s, forwarding to %s", addr, target)

	raddr, err := net.ResolveUDPAddr("udp", target)
	if err != nil {
		return fmt.Errorf("resolve target addr %s: %w", target, err)
	}

	buf := make([]byte, udpBufSize)
	for {
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Printf("[udp] read error: %v", err)
			continue
		}
		go handleUDP(conn, src, raddr, buf[:n])
	}
}

func handleUDP(conn *net.UDPConn, src *net.UDPAddr, dst *net.UDPAddr, data []byte) {
	// Dial a new UDP socket to the target so we can receive its reply.
	remote, err := net.DialUDP("udp", nil, dst)
	if err != nil {
		log.Printf("[udp] dial %s: %v", dst, err)
		return
	}
	defer remote.Close()

	_, err = remote.Write(data)
	if err != nil {
		log.Printf("[udp] write to %s: %v", dst, err)
		return
	}
	log.Printf("[udp] %s -> %s: %d bytes", src, dst, len(data))

	// Wait for a reply from the target and relay it back to the original sender.
	remote.SetReadDeadline(time.Now().Add(udpTimeout))
	reply := make([]byte, udpBufSize)
	n, err := remote.Read(reply)
	if err != nil {
		// Timeout or no reply — not necessarily an error for fire-and-forget protocols.
		return
	}

	_, err = conn.WriteToUDP(reply[:n], src)
	if err != nil {
		log.Printf("[udp] write reply to %s: %v", src, err)
		return
	}
	log.Printf("[udp] %s <- %s: %d bytes", src, dst, n)
}
