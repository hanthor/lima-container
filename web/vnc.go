package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

const (
	wsPortMin       = 5710
	wsPortMax       = 5799
	nginxConfPath   = "/etc/nginx/lima-vnc-locations.conf"
	websocketdBin   = "websocketd"
	wsBridgeBin     = "/usr/local/bin/lima-websocket-bridge"
	vncPortFileDir  = "/run"
)

type vncEntry struct {
	Cmd      *exec.Cmd
	WSPort   int
	VNCPort  int
	Password string
}

// VNCManager tracks websocketd processes for each Lima instance.
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

// StartVNC starts a websocketd bridge for the given instance.
func (v *VNCManager) StartVNC(instance string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if e, ok := v.entries[instance]; ok && e.Cmd != nil && e.Cmd.Process != nil {
		return fmt.Errorf("VNC bridge already running for %q", instance)
	}

	vncPort, err := v.readVNCPort(instance)
	if err != nil {
		return fmt.Errorf("reading VNC port: %w", err)
	}

	password := v.readVNCPassword(instance)

	wsPort, err := v.allocatePort()
	if err != nil {
		return err
	}

	// Write per-instance VNC port file so lima-websocket-bridge reads it.
	portFilePath := fmt.Sprintf("%s/lima-vnc-port-%s", vncPortFileDir, instance)
	if err := os.WriteFile(portFilePath, []byte(strconv.Itoa(vncPort)), 0644); err != nil {
		return fmt.Errorf("writing VNC port file: %w", err)
	}

	cmd := exec.Command(websocketdBin,
		"--binary",
		"--address", "127.0.0.1",
		fmt.Sprintf("--port=%d", wsPort),
		wsBridgeBin,
	)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("LIMA_VNC_PORT_FILE=%s", portFilePath),
		fmt.Sprintf("LIMA_VNC_PORT=%d", vncPort),
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting websocketd: %w", err)
	}

	v.entries[instance] = &vncEntry{
		Cmd:      cmd,
		WSPort:   wsPort,
		VNCPort:  vncPort,
		Password: password,
	}

	// Reap the process asynchronously.
	go func() {
		_ = cmd.Wait()
	}()

	if err := v.writeNginxConfig(); err != nil {
		log.Printf("warning: failed to write nginx config: %v", err)
	}
	reloadNginx()

	log.Printf("VNC bridge started for %q: ws=:%d vnc=:%d", instance, wsPort, vncPort)
	return nil
}

// StopVNC stops the websocketd bridge for the given instance.
func (v *VNCManager) StopVNC(instance string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	e, ok := v.entries[instance]
	if !ok {
		return fmt.Errorf("no VNC bridge running for %q", instance)
	}

	if e.Cmd != nil && e.Cmd.Process != nil {
		_ = e.Cmd.Process.Kill()
	}

	// Remove per-instance port file.
	portFilePath := fmt.Sprintf("%s/lima-vnc-port-%s", vncPortFileDir, instance)
	_ = os.Remove(portFilePath)

	delete(v.entries, instance)

	if err := v.writeNginxConfig(); err != nil {
		log.Printf("warning: failed to write nginx config: %v", err)
	}
	reloadNginx()

	log.Printf("VNC bridge stopped for %q", instance)
	return nil
}

// GetVNCInfo returns connection info for an instance's VNC bridge.
func (v *VNCManager) GetVNCInfo(instance string) (map[string]any, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	e, ok := v.entries[instance]
	if !ok {
		return nil, fmt.Errorf("no VNC bridge running for %q", instance)
	}

	url := fmt.Sprintf("/vnc/vnc.html?autoconnect=1&resize=remote&path=/websockify/%s",
		instance)
	if e.Password != "" {
		url += "&password=" + e.Password
	}

	return map[string]any{
		"port":     e.WSPort,
		"password": e.Password,
		"url":      url,
	}, nil
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

// allocatePort finds an unused websocketd port in the range [wsPortMin, wsPortMax].
func (v *VNCManager) allocatePort() (int, error) {
	used := make(map[int]bool)
	for _, e := range v.entries {
		used[e.WSPort] = true
	}
	for p := wsPortMin; p <= wsPortMax; p++ {
		if !used[p] {
			return p, nil
		}
	}
	return 0, fmt.Errorf("no free websocketd ports in range %d-%d", wsPortMin, wsPortMax)
}

// writeNginxConfig regenerates the shared nginx config for all active VNC instances.
// The config is included at the http{} level, so it must define its own server block.
func (v *VNCManager) writeNginxConfig() error {
	var b strings.Builder
	b.WriteString("# Auto-generated by lima-web — do not edit\n")

	if len(v.entries) == 0 {
		// Write an empty file so nginx doesn't error on missing locations.
		return os.WriteFile(nginxConfPath, []byte(b.String()), 0644)
	}

	for instance, e := range v.entries {
		fmt.Fprintf(&b, `
# VNC bridge for instance %s
location /websockify/%s {
    proxy_http_version 1.1;
    proxy_read_timeout 61s;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_pass http://127.0.0.1:%d/;
}
`, instance, instance, e.WSPort)
	}

	return os.WriteFile(nginxConfPath, []byte(b.String()), 0644)
}

// reloadNginx sends a reload signal to nginx.
func reloadNginx() {
	cmd := exec.Command("nginx", "-s", "reload")
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("nginx reload failed: %v: %s", err, string(out))
	}
}
