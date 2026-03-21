package main

import (
	"crypto/tls"
	"encoding/asn1"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// RDCleanPath protocol version used by IronRDP.
const rdCleanPathVersion = 3390

var rdpUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type rdpEntry struct {
	RDPPort  int
	Username string
	Password string
}

// RDPManager tracks RDP ports for each Lima instance and serves the
// RDCleanPath WebSocket proxy required by IronRDP WASM.
type RDPManager struct {
	limaHome string
	mu       sync.Mutex
	entries  map[string]*rdpEntry
}

func NewRDPManager(limaHome string) *RDPManager {
	return &RDPManager{
		limaHome: limaHome,
		entries:  make(map[string]*rdpEntry),
	}
}

// ASN.1 structures for the RDCleanPath protocol.

type rdCleanPathRequest struct {
	Version           int    `asn1:"tag:0,explicit"`
	Destination       string `asn1:"tag:2,explicit,utf8"`
	ProxyAuth         string `asn1:"tag:3,explicit,utf8"`
	X224ConnectionReq []byte `asn1:"tag:5,explicit"`
	PreconnectionBlob []byte `asn1:"tag:6,explicit,optional"`
}

type rdCleanPathResponse struct {
	Version               int             `asn1:"tag:0,explicit"`
	X224ConnectionConfirm []byte          `asn1:"tag:1,explicit"`
	ServerCertChain       []asn1.RawValue `asn1:"tag:4,explicit"`
	ServerAddr            string          `asn1:"tag:6,explicit,utf8"`
}

// StartRDP registers RDP connection info for an instance.
func (m *RDPManager) StartRDP(instance string) error {
	port, err := m.detectRDPPort(instance)
	if err != nil {
		return fmt.Errorf("detecting RDP port: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.entries[instance] = &rdpEntry{RDPPort: port}
	log.Printf("RDP registered for %q: port=%d", instance, port)
	return nil
}

// StopRDP removes the RDP entry for an instance.
func (m *RDPManager) StopRDP(instance string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.entries[instance]; !ok {
		return fmt.Errorf("no RDP entry for %q", instance)
	}
	delete(m.entries, instance)
	log.Printf("RDP unregistered for %q", instance)
	return nil
}

// DetectRDP checks whether an RDP entry exists for the instance.
func (m *RDPManager) DetectRDP(instance string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.entries[instance]
	return ok
}

// GetRDPInfo returns connection info for an instance's RDP.
func (m *RDPManager) GetRDPInfo(instance string) (map[string]any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	e, ok := m.entries[instance]
	if !ok {
		return nil, fmt.Errorf("no RDP available for %q", instance)
	}

	return map[string]any{
		"available": true,
		"url":       fmt.Sprintf("/rdp/rdp.html?host=127.0.0.1:%d&ws=/rdp/%s", e.RDPPort, instance),
	}, nil
}

// HandleRDPProxy implements the RDCleanPath WebSocket proxy for IronRDP.
// It handles the TLS handshake with the RDP server, extracts certificates,
// and relays encrypted traffic bidirectionally.
func (m *RDPManager) HandleRDPProxy(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	// Look up registered port, fall back to Lima's portForward default (3389).
	m.mu.Lock()
	e, ok := m.entries[name]
	m.mu.Unlock()

	rdpPort := 3389
	if ok {
		rdpPort = e.RDPPort
	} else {
		log.Printf("RDP no registration for %q, using default port %d", name, rdpPort)
	}

	ws, err := rdpUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("RDP WS upgrade failed for %q: %v", name, err)
		return
	}
	defer ws.Close()

	// Phase 1: Read RDCleanPath request from browser.
	_, msg, err := ws.ReadMessage()
	if err != nil {
		log.Printf("RDP read RDCleanPath request failed for %q: %v", name, err)
		return
	}

	var req rdCleanPathRequest
	if _, err := asn1.Unmarshal(msg, &req); err != nil {
		log.Printf("RDP ASN.1 unmarshal failed for %q: %v", name, err)
		_ = ws.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(1002, "invalid RDCleanPath request"))
		return
	}

	log.Printf("RDP RDCleanPath request for %q: version=%d dest=%s",
		name, req.Version, req.Destination)

	// Phase 2: Connect to the RDP server via Lima's port forward.
	addr := fmt.Sprintf("127.0.0.1:%d", rdpPort)
	tcp, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		log.Printf("RDP TCP dial failed for %q (%s): %v", name, addr, err)
		_ = ws.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(1011, "cannot connect to RDP server"))
		return
	}
	defer tcp.Close()

	// Send X.224 Connection Request.
	if _, err := tcp.Write(req.X224ConnectionReq); err != nil {
		log.Printf("RDP X.224 request write failed for %q: %v", name, err)
		return
	}

	// Read X.224 Connection Confirm (TPKT framing).
	_ = tcp.(*net.TCPConn).SetReadDeadline(time.Now().Add(10 * time.Second))
	x224Confirm, err := readTPKT(tcp)
	if err != nil {
		log.Printf("RDP X.224 confirm read failed for %q: %v", name, err)
		return
	}
	_ = tcp.(*net.TCPConn).SetReadDeadline(time.Time{})

	// TLS handshake with the RDP server.
	tlsConn := tls.Client(tcp, &tls.Config{InsecureSkipVerify: true})
	if err := tlsConn.HandshakeContext(r.Context()); err != nil {
		log.Printf("RDP TLS handshake failed for %q: %v", name, err)
		return
	}

	// Extract server certificate chain.
	state := tlsConn.ConnectionState()
	var certChain []asn1.RawValue
	for _, cert := range state.PeerCertificates {
		certChain = append(certChain, asn1.RawValue{
			Class: asn1.ClassUniversal,
			Tag:   asn1.TagOctetString,
			Bytes: cert.Raw,
		})
	}
	if len(certChain) == 0 {
		log.Printf("RDP warning: no peer certificates from %q", name)
	}

	// Phase 3: Build and send RDCleanPath response.
	resp := rdCleanPathResponse{
		Version:               rdCleanPathVersion,
		X224ConnectionConfirm: x224Confirm,
		ServerCertChain:       certChain,
		ServerAddr:            addr,
	}

	respBytes, err := asn1.Marshal(resp)
	if err != nil {
		log.Printf("RDP response ASN.1 marshal failed for %q: %v", name, err)
		return
	}

	if err := ws.WriteMessage(websocket.BinaryMessage, respBytes); err != nil {
		log.Printf("RDP response write failed for %q: %v", name, err)
		return
	}

	log.Printf("RDP proxy started for %q (%s)", name, addr)

	// Phase 4: Bidirectional relay (TLS ↔ WebSocket).
	done := make(chan struct{}, 2)

	// TLS → WebSocket
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := tlsConn.Read(buf)
			if n > 0 {
				if werr := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
		done <- struct{}{}
	}()

	// WebSocket → TLS
	go func() {
		for {
			_, data, err := ws.ReadMessage()
			if err != nil {
				break
			}
			if _, werr := tlsConn.Write(data); werr != nil {
				break
			}
		}
		done <- struct{}{}
	}()

	<-done
	log.Printf("RDP proxy ended for %q", name)
}

// readTPKT reads a TPKT-framed message from the connection.
// TPKT header: version(1) + reserved(1) + length(2 big-endian, total including header).
func readTPKT(conn net.Conn) ([]byte, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, fmt.Errorf("reading TPKT header: %w", err)
	}

	length := int(binary.BigEndian.Uint16(header[2:4]))
	if length < 4 {
		return nil, fmt.Errorf("invalid TPKT length: %d", length)
	}

	pdu := make([]byte, length)
	copy(pdu, header)
	if length > 4 {
		if _, err := io.ReadFull(conn, pdu[4:]); err != nil {
			return nil, fmt.Errorf("reading TPKT body: %w", err)
		}
	}

	return pdu, nil
}

// detectRDPPort reads the RDP port from $LIMA_HOME/<instance>/rdpport,
// falling back to the standard RDP port 3389.
func (m *RDPManager) detectRDPPort(instance string) (int, error) {
	path := filepath.Join(m.limaHome, instance, "rdpport")
	data, err := os.ReadFile(path)
	if err == nil {
		port, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err == nil && port > 0 {
			return port, nil
		}
	}
	return 3389, nil
}
