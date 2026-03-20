package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var vncUpgrader = websocket.Upgrader{
	CheckOrigin:  func(r *http.Request) bool { return true },
	Subprotocols: []string{"binary"},
}

type vncEntry struct {
	VNCPort  int
	Password string
}

// VNCManager tracks QEMU VNC ports for each Lima instance.
type VNCManager struct {
	limaHome string
	mu       sync.Mutex
	entries  map[string]*vncEntry
}

func NewVNCManager(limaHome string) *VNCManager {
	return &VNCManager{
		limaHome: limaHome,
		entries:  make(map[string]*vncEntry),
	}
}

// StartVNC registers VNC connection info for an instance.
// No subprocess is started; connections are proxied on-demand by HandleVNCProxy.
func (v *VNCManager) StartVNC(instance string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	vncPort, err := v.readVNCPort(instance)
	if err != nil {
		return fmt.Errorf("reading VNC port: %w", err)
	}

	password := v.readVNCPassword(instance)

	v.entries[instance] = &vncEntry{
		VNCPort:  vncPort,
		Password: password,
	}

	log.Printf("VNC registered for %q: port=%d", instance, vncPort)
	return nil
}

// StopVNC removes the VNC entry for an instance.
func (v *VNCManager) StopVNC(instance string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if _, ok := v.entries[instance]; !ok {
		return fmt.Errorf("no VNC entry for %q", instance)
	}
	delete(v.entries, instance)
	log.Printf("VNC unregistered for %q", instance)
	return nil
}

// GetVNCInfo returns connection info for an instance's VNC.
func (v *VNCManager) GetVNCInfo(instance string) (map[string]any, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	e, ok := v.entries[instance]
	if !ok {
		return nil, fmt.Errorf("no VNC bridge running for %q", instance)
	}

	url := fmt.Sprintf("/vnc/vnc.html?autoconnect=1&resize=remote&path=/websockify/%s", instance)
	if e.Password != "" {
		url += "&password=" + e.Password
	}

	return map[string]any{
		"port":     e.VNCPort,
		"password": e.Password,
		"url":      url,
	}, nil
}

// HandleVNCProxy upgrades the HTTP connection to WebSocket and proxies raw
// TCP to the QEMU VNC port for the named instance. noVNC connects here.
func (v *VNCManager) HandleVNCProxy(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	v.mu.Lock()
	e, ok := v.entries[name]
	v.mu.Unlock()

	if !ok {
		http.Error(w, "no VNC registered for instance "+name, http.StatusNotFound)
		return
	}

	ws, err := vncUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("VNC WS upgrade failed for %q: %v", name, err)
		return
	}
	defer ws.Close()

	addr := fmt.Sprintf("127.0.0.1:%d", e.VNCPort)
	tcp, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		log.Printf("VNC TCP dial failed for %q (%s): %v", name, addr, err)
		_ = ws.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(1011, "cannot connect to VNC server"))
		return
	}
	defer tcp.Close()

	log.Printf("VNC proxy started for %q (%s)", name, addr)

	done := make(chan struct{}, 2)

	// TCP → WebSocket
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := tcp.Read(buf)
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

	// WebSocket → TCP
	go func() {
		for {
			_, data, err := ws.ReadMessage()
			if err != nil {
				break
			}
			if _, werr := tcp.Write(data); werr != nil {
				break
			}
		}
		done <- struct{}{}
	}()

	<-done
	log.Printf("VNC proxy ended for %q", name)
}

// readVNCPort reads the VNC display number from $LIMA_HOME/<instance>/vncdisplay
// and converts it to a port number (5900 + display).
func (v *VNCManager) readVNCPort(instance string) (int, error) {
	path := filepath.Join(v.limaHome, instance, "vncdisplay")
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	// Format: "127.0.0.1:<display>"
	line := strings.TrimSpace(string(data))
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("unexpected vncdisplay format: %q", line)
	}
	display, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, fmt.Errorf("parsing display number %q: %w", parts[1], err)
	}
	return 5900 + display, nil
}

// readVNCPassword reads the VNC password from $LIMA_HOME/<instance>/vncpassword.
func (v *VNCManager) readVNCPassword(instance string) string {
	path := filepath.Join(v.limaHome, instance, "vncpassword")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// reloadNginx sends a reload signal to nginx.
func reloadNginx() {
	cmd := exec.Command("nginx", "-s", "reload")
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("nginx reload failed: %v: %s", err, string(out))
	}
}
