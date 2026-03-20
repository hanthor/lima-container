package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestVNCUpgrader_BinarySubprotocol(t *testing.T) {
	found := false
	for _, p := range vncUpgrader.Subprotocols {
		if p == "binary" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("vncUpgrader.Subprotocols does not contain \"binary\"; noVNC will fail to connect")
	}
}

func TestVNCProxy_MissingInstance(t *testing.T) {
	vnc := NewVNCManager(t.TempDir())

	mux := http.NewServeMux()
	mux.HandleFunc("GET /websockify/{name}", vnc.HandleVNCProxy)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// A plain HTTP GET (no WebSocket upgrade) to a missing instance should 404.
	resp, err := http.Get(srv.URL + "/websockify/nonexistent")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for missing instance, got %d", resp.StatusCode)
	}
}

func TestVNCProxy_MissingInstance_WebSocket(t *testing.T) {
	vnc := NewVNCManager(t.TempDir())

	mux := http.NewServeMux()
	mux.HandleFunc("GET /websockify/{name}", vnc.HandleVNCProxy)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// A WebSocket upgrade to a missing instance should fail with a non-101 status.
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/websockify/nonexistent"
	dialer := websocket.Dialer{Subprotocols: []string{"binary"}}
	_, resp, err := dialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected WebSocket dial to fail for missing instance")
	}
	if resp != nil && resp.StatusCode == http.StatusSwitchingProtocols {
		t.Fatal("should not get 101 for a nonexistent instance")
	}
}

func TestVNCProxy_HandshakeSubprotocol(t *testing.T) {
	// Start a fake TCP "VNC server" so the handler can complete its dial.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port

	// Accept one connection and hold it open for the duration of the test.
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			accepted <- c
		}
	}()

	vnc := NewVNCManager(t.TempDir())
	// Directly inject an entry so we don't need real Lima files.
	vnc.mu.Lock()
	vnc.entries["test-vm"] = &vncEntry{VNCPort: port}
	vnc.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /websockify/{name}", vnc.HandleVNCProxy)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/websockify/test-vm"
	dialer := websocket.Dialer{Subprotocols: []string{"binary"}}
	conn, resp, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close()

	// Clean up the fake VNC connection.
	defer func() {
		select {
		case c := <-accepted:
			c.Close()
		default:
		}
	}()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("expected 101, got %d", resp.StatusCode)
	}

	proto := resp.Header.Get("Sec-WebSocket-Protocol")
	if proto != "binary" {
		t.Fatalf("expected Sec-WebSocket-Protocol \"binary\", got %q", proto)
	}

	if conn.Subprotocol() != "binary" {
		t.Fatalf("negotiated subprotocol = %q, want \"binary\"", conn.Subprotocol())
	}
}

func TestVNCProxy_BinaryFrameProxy(t *testing.T) {
	// Start a fake TCP VNC server that sends a known payload.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	payload := []byte("RFB 003.008\n") // VNC protocol version handshake

	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		c.Write(payload)
		// Hold connection open until client disconnects.
		buf := make([]byte, 1)
		c.Read(buf)
	}()

	vnc := NewVNCManager(t.TempDir())
	vnc.mu.Lock()
	vnc.entries["test-vm"] = &vncEntry{VNCPort: port}
	vnc.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /websockify/{name}", vnc.HandleVNCProxy)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/websockify/test-vm"
	dialer := websocket.Dialer{Subprotocols: []string{"binary"}}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close()

	msgType, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if msgType != websocket.BinaryMessage {
		t.Fatalf("expected BinaryMessage (%d), got %d", websocket.BinaryMessage, msgType)
	}
	if string(data) != string(payload) {
		t.Fatalf("payload mismatch: got %q, want %q", data, payload)
	}
}
