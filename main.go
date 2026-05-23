package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os/exec"
	"sync/atomic"

	"github.com/songgao/water"
)

func main() {
	mode     := flag.String("mode", "", "a or b")
	serverAddr := flag.String("server", "", "signaling server address e.g. 1.2.3.4:7654")
	myID     := flag.String("id", "", "your peer ID e.g. alice")
	peerID   := flag.String("peer", "", "peer ID to connect to e.g. bob")
	flag.Parse()

	if *mode != "a" && *mode != "b" {
		log.Fatal("--mode must be 'a' or 'b'")
	}
	if *serverAddr == "" || *myID == "" || *peerID == "" {
		log.Fatal("--server, --id, and --peer are required")
	}

	// TUN virtual IPs — same as before
	tunIP := map[string]string{
		"a": "10.0.0.1/24",
		"b": "10.0.0.2/24",
	}[*mode]

	// UDP port this machine listens on for the tunnel
	localUDPPort := map[string]string{"a": "9001", "b": "9002"}[*mode]

	// --- Step 1: Connect to signaling server ---
	fmt.Printf("[signal] connecting to %s\n", *serverAddr)
	sig, err := Connect(*serverAddr)
	if err != nil {
		log.Fatal("[signal] connect:", err)
	}
	defer sig.Close()

	// --- Step 2: Register ourselves, get back our public IP:port ---
	myEndpoint, err := sig.Register(*myID, localUDPPort)
	if err != nil {
		log.Fatal("[signal] register:", err)
	}
	fmt.Printf("[signal] I am %s, public endpoint: %s\n", *myID, myEndpoint)

	// --- Step 3: Look up the peer ---
	fmt.Printf("[signal] looking up peer %q...\n", *peerID)
	peerEndpoint, err := sig.Lookup(*peerID)
	if err != nil {
		log.Fatal("[signal] lookup:", err)
	}
	fmt.Printf("[signal] found peer %s at %s\n", *peerID, peerEndpoint)

	// --- Step 4: Create TUN interface ---
	iface, err := water.New(water.Config{DeviceType: water.TUN})
	if err != nil {
		log.Fatal("create TUN:", err)
	}
	fmt.Printf("[tun] interface: %s\n", iface.Name())

	mustRun("ip", "addr", "add", tunIP, "dev", iface.Name())
	mustRun("ip", "link", "set", "dev", iface.Name(), "up")
	fmt.Printf("[tun] configured with %s\n", tunIP)

	// --- Step 5: UDP socket ---
	localAddr, err := net.ResolveUDPAddr("udp4", ":"+localUDPPort)
	if err != nil {
		log.Fatal("resolve local:", err)
	}
	conn, err := net.ListenUDP("udp4", localAddr)
	if err != nil {
		log.Fatal("listen UDP:", err)
	}
	fmt.Printf("[udp] listening on :%s\n", localUDPPort)

	// Parse peer's public endpoint
	peerAddr, err := net.ResolveUDPAddr("udp4", peerEndpoint)
	if err != nil {
		log.Fatal("resolve peer:", err)
	}
	fmt.Printf("[udp] peer at %s\n", peerAddr)

	// --- Step 6: Run the tunnel ---
	go tunToUDP(iface, conn, peerAddr)
	udpToTUN(conn, iface)
}

func tunToUDP(iface *water.Interface, conn *net.UDPConn, peer *net.UDPAddr) {
	aead := newCipher()
	buf := make([]byte, 1500)
	var counter uint64

	for {
		n, err := iface.Read(buf)
		if err != nil {
			log.Fatal("[tun→udp] read TUN:", err)
		}

		encrypted := Encrypt(aead, counter, buf[:n])
		atomic.AddUint64(&counter, 1)

		_, err = conn.WriteToUDP(encrypted, peer)
		if err != nil {
			log.Printf("[tun→udp] send UDP: %v", err)
			continue
		}

		fmt.Printf("[tun→udp] %d bytes → %d bytes encrypted\n", n, len(encrypted))
	}
}

func udpToTUN(conn *net.UDPConn, iface *water.Interface) {
	aead := newCipher()
	buf := make([]byte, 1500+12+16)

	for {
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Fatal("[udp→tun] read UDP:", err)
		}

		plaintext, err := Decrypt(aead, buf[:n])
		if err != nil {
			log.Printf("[udp→tun] dropping packet from %s: %v", remoteAddr, err)
			continue
		}

		_, err = iface.Write(plaintext)
		if err != nil {
			log.Printf("[udp→tun] write TUN: %v", err)
			continue
		}

		fmt.Printf("[udp→tun] %d bytes encrypted → %d bytes plaintext\n", n, len(plaintext))
	}
}

func mustRun(name string, args ...string) {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		log.Fatalf("cmd %s %v: %v\n%s", name, args, err, out)
	}
}