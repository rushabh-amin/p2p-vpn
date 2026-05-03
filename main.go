package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os/exec"

	"github.com/songgao/water"
)

func main() {
	// --- Flags ---
	mode := flag.String("mode", "", "a or b")
	peer := flag.String("peer", "", "real IP of the other machine, e.g. 192.168.1.10")
	flag.Parse()

	if *mode != "a" && *mode != "b" {
		log.Fatal("--mode must be 'a' or 'b'")
	}
	if *peer == "" {
		log.Fatal("--peer must be set to the real IP of the other machine")
	}

	// --- TUN virtual IPs ---
	// These are the IPs inside the tunnel. Not real network IPs.
	// A talks to B as 10.0.0.2, B talks to A as 10.0.0.1.
	tunIP := map[string]string{
		"a": "10.0.0.1/24",
		"b": "10.0.0.2/24",
	}[*mode]

	// --- UDP ports ---
	// A listens on 9001, B listens on 9002.
	// Each sends to the other's port.
	localPort := map[string]string{"a": "9001", "b": "9002"}[*mode]
	peerPort  := map[string]string{"a": "9002", "b": "9001"}[*mode]

	// --- Create TUN interface ---
	iface, err := water.New(water.Config{DeviceType: water.TUN})
	if err != nil {
		log.Fatal("create TUN:", err)
	}
	fmt.Printf("[tun] interface: %s\n", iface.Name())

	// --- Bring TUN up with virtual IP ---
	mustRun("ip", "addr", "add", tunIP, "dev", iface.Name())
	mustRun("ip", "link", "set", "dev", iface.Name(), "up")
	fmt.Printf("[tun] configured with %s\n", tunIP)

	// --- UDP listen socket ---
	localAddr, err := net.ResolveUDPAddr("udp4", ":"+localPort)
	if err != nil {
		log.Fatal("resolve local:", err)
	}
	conn, err := net.ListenUDP("udp4", localAddr)
	if err != nil {
		log.Fatal("listen UDP:", err)
	}
	fmt.Printf("[udp] listening on :%s\n", localPort)

	// Peer's real UDP address (real network IP, not tunnel IP)
	peerAddr, err := net.ResolveUDPAddr("udp4", *peer+":"+peerPort)
	if err != nil {
		log.Fatal("resolve peer:", err)
	}
	fmt.Printf("[udp] peer at %s\n", peerAddr)

	// --- Start the two goroutines ---
	// They run forever, independently.

	// TUN → UDP: read IP packets from kernel, send to peer over UDP
	go tunToUDP(iface, conn, peerAddr)

	// UDP → TUN: receive UDP datagrams from peer, inject into kernel
	udpToTUN(conn, iface)
}

// tunToUDP reads raw IP packets from the TUN interface
// and forwards each one as a single UDP datagram to the peer.
func tunToUDP(iface *water.Interface, conn *net.UDPConn, peer *net.UDPAddr) {
	buf := make([]byte, 1500) // standard Ethernet MTU
	for {
		n, err := iface.Read(buf)
		if err != nil {
			log.Fatal("[tun→udp] read TUN:", err)
		}

		_, err = conn.WriteToUDP(buf[:n], peer)
		if err != nil {
			log.Printf("[tun→udp] send UDP: %v", err)
			continue
		}

		fmt.Printf("[tun→udp] %d bytes\n", n)
	}
}

// udpToTUN receives UDP datagrams from the peer
// and injects each one as a raw IP packet into the TUN interface.
func udpToTUN(conn *net.UDPConn, iface *water.Interface) {
	buf := make([]byte, 1500)
	for {
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Fatal("[udp→tun] read UDP:", err)
		}

		_, err = iface.Write(buf[:n])
		if err != nil {
			log.Printf("[udp→tun] write TUN: %v", err)
			continue
		}

		fmt.Printf("[udp→tun] %d bytes from %s\n", n, remoteAddr)
	}
}

// mustRun runs a shell command and fatals if it fails.
func mustRun(name string, args ...string) {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		log.Fatalf("cmd %s %v: %v\n%s", name, args, err, out)
	}
}