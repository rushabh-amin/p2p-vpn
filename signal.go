package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// Message matches the server's Message struct exactly
type Message struct {
	Type     string `json:"type"`
	PeerID   string `json:"peer_id,omitempty"`
	UDPPort  string `json:"udp_port,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
}

// SignalClient handles all communication with the signaling server
type SignalClient struct {
	conn net.Conn
	enc  *json.Encoder
	dec  *json.Decoder
}

// Connect dials the signaling server and returns a ready client
func Connect(serverAddr string) (*SignalClient, error) {
	conn, err := net.DialTimeout("tcp", serverAddr, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect to signal server %s: %w", serverAddr, err)
	}

	return &SignalClient{
		conn: conn,
		enc:  json.NewEncoder(conn),
		dec:  json.NewDecoder(bufio.NewReader(conn)),
	}, nil
}

// Register tells the server who we are and what UDP port we're listening on.
// Returns our own public endpoint as seen from the internet e.g. "1.2.3.4:9001"
func (s *SignalClient) Register(peerID, udpPort string) (string, error) {
	err := s.enc.Encode(Message{
		Type:    "register",
		PeerID:  peerID,
		UDPPort: udpPort,
	})
	if err != nil {
		return "", fmt.Errorf("send register: %w", err)
	}

	var resp Message
	if err := s.dec.Decode(&resp); err != nil {
		return "", fmt.Errorf("read ack: %w", err)
	}
	if resp.Type == "error" {
		return "", fmt.Errorf("server error: %s", resp.Endpoint)
	}

	return resp.Endpoint, nil
}

// Lookup asks the server for a peer's public endpoint.
// Retries for up to 30 seconds — the peer might not have registered yet.
func (s *SignalClient) Lookup(peerID string) (string, error) {
	deadline := time.Now().Add(30 * time.Second)

	for time.Now().Before(deadline) {
		err := s.enc.Encode(Message{
			Type:   "lookup",
			PeerID: peerID,
		})
		if err != nil {
			return "", fmt.Errorf("send lookup: %w", err)
		}

		var resp Message
		if err := s.dec.Decode(&resp); err != nil {
			return "", fmt.Errorf("read lookup response: %w", err)
		}

		if resp.Type == "peer" {
			// Found it
			return resp.Endpoint, nil
		}

		// Peer not registered yet, wait and retry
		fmt.Printf("[signal] peer %q not found yet, retrying...\n", peerID)
		time.Sleep(2 * time.Second)
	}

	return "", fmt.Errorf("peer %q not found after 30 seconds", peerID)
}

func (s *SignalClient) Close() error {
	return s.conn.Close()
}