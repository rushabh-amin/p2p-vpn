package server

import (
	"bufio"
	"encoding/json"
	"flag"
	// "fmt"
	"log"
	"net"
	"sync"
	"time"
)
 
// Message is the envelope for all communication between peer and server
type Message struct {
	Type     string `json:"type"`               // "register", "ack", "lookup", "peer", "error"
	PeerID   string `json:"peer_id,omitempty"`  // who am i / who do i want
	UDPPort  string `json:"udp_port,omitempty"` // my local UDP port for the tunnel
	Endpoint string `json:"endpoint,omitempty"` // public IP:port (filled by server)
}
 
// PeerRecord is what the server stores for each registered peer
type PeerRecord struct {
	PeerID    string
	Endpoint  string    // public IP:port as seen by server
	UpdatedAt time.Time
}
 
// Registry holds all registered peers
type Registry struct {
	mu    sync.RWMutex
	peers map[string]*PeerRecord
}
 
func NewRegistry() *Registry {
	return &Registry{peers: make(map[string]*PeerRecord)}
}
 
func (r *Registry) Register(peerID, endpoint string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.peers[peerID] = &PeerRecord{
		PeerID:    peerID,
		Endpoint:  endpoint,
		UpdatedAt: time.Now(),
	}
	log.Printf("[registry] registered peer=%s endpoint=%s", peerID, endpoint)
}
 
func (r *Registry) Lookup(peerID string) (*PeerRecord, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rec, ok := r.peers[peerID]
	return rec, ok
}
 
func main() {
	addr := flag.String("addr", ":7654", "TCP listen address")
	flag.Parse()
 
	registry := NewRegistry()
 
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal("listen:", err)
	}
	log.Printf("[server] listening on %s", *addr)
 
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("[server] accept error: %v", err)
			continue
		}
		go handleConn(conn, registry)
	}
}
 
func handleConn(conn net.Conn, registry *Registry) {
	defer conn.Close()
 
	// This is the key STUN part:
	// remoteAddr is the public IP:port of the peer as seen by the server.
	// For a peer behind NAT, this is their post-NAT address.
	remoteAddr := conn.RemoteAddr().String()
	remoteHost, _, _ := net.SplitHostPort(remoteAddr)
	log.Printf("[server] new connection from %s", remoteAddr)
 
	scanner := bufio.NewScanner(conn)
	enc := json.NewEncoder(conn)
 
	for scanner.Scan() {
		var msg Message
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			enc.Encode(Message{Type: "error", PeerID: "", Endpoint: "invalid json"})
			continue
		}
 
		switch msg.Type {
 
		case "register":
			// Client tells us its UDP port.
			// We combine that with the observed TCP source IP.
			// Result: the public IP:port other peers should send UDP to.
			if msg.PeerID == "" || msg.UDPPort == "" {
				enc.Encode(Message{Type: "error", Endpoint: "peer_id and udp_port required"})
				continue
			}
 
			// Combine observed public IP + client's UDP port
			publicEndpoint := net.JoinHostPort(remoteHost, msg.UDPPort)
			registry.Register(msg.PeerID, publicEndpoint)
 
			// Send back the public endpoint so the peer knows its own public IP:port
			enc.Encode(Message{
				Type:     "ack",
				PeerID:   msg.PeerID,
				Endpoint: publicEndpoint,
			})
 
		case "lookup":
			if msg.PeerID == "" {
				enc.Encode(Message{Type: "error", Endpoint: "peer_id required"})
				continue
			}
 
			rec, ok := registry.Lookup(msg.PeerID)
			if !ok {
				enc.Encode(Message{Type: "error", Endpoint: "peer not found: " + msg.PeerID})
				continue
			}
 
			enc.Encode(Message{
				Type:     "peer",
				PeerID:   rec.PeerID,
				Endpoint: rec.Endpoint,
			})
 
		default:
			enc.Encode(Message{Type: "error", Endpoint: "unknown type: " + msg.Type})
		}
	}
 
	log.Printf("[server] connection closed: %s", remoteAddr)
}
 
// func send(enc *json.Encoder, msg Message) {
// 	if err := enc.Encode(msg); err != nil {
// 		log.Printf("[server] send error: %v", err)
// 	}
// }
 
// func must(err error) {
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// }
 
// func check(err error) {
// 	if err != nil {
// 		fmt.Println("error:", err)
// 	}
// }
