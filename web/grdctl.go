package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// GRDManager handles GNOME Remote Desktop detection and configuration
// for Lima VM instances.
type GRDManager struct {
	limaHome string
}

func NewGRDManager(limaHome string) *GRDManager {
	return &GRDManager{limaHome: limaHome}
}

// RDPStatus describes the RDP availability for a VM instance.
type RDPStatus struct {
	Available bool   `json:"available"`
	Type      string `json:"type"` // "grd", "xrdp", "none"
	Port      int    `json:"port"`
	Enabled   bool   `json:"enabled"`
}

// DetectGRD checks if GNOME Remote Desktop is available in the VM.
func (g *GRDManager) DetectGRD(instance string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "limactl", "shell", instance, "--",
		"test", "-f", "/usr/libexec/gnome-remote-desktop-daemon",
		"-o", "-f", "/usr/lib/gnome-remote-desktop-daemon")
	cmd.Env = append(cmd.Environ(), "LIMA_HOME="+g.limaHome)

	err := cmd.Run()
	return err == nil, nil
}

// DetectXRDP checks if xrdp is available in the VM.
func (g *GRDManager) DetectXRDP(instance string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "limactl", "shell", instance, "--",
		"which", "xrdp")
	cmd.Env = append(cmd.Environ(), "LIMA_HOME="+g.limaHome)

	err := cmd.Run()
	return err == nil, nil
}

// EnableGRD enables and configures GNOME Remote Desktop with RDP credentials.
// Commands run as the regular Lima user (not root) since grdctl operates on
// the user's D-Bus session.
func (g *GRDManager) EnableGRD(instance, username, password string) error {
	commands := [][]string{
		{"grdctl", "rdp", "enable"},
		{"grdctl", "rdp", "disable-view-only"},
		{"grdctl", "rdp", "set-credentials", username, password},
		{"systemctl", "--user", "enable", "--now", "gnome-remote-desktop"},
	}

	for _, args := range commands {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		cmdArgs := append([]string{"shell", instance, "--"}, args...)
		cmd := exec.CommandContext(ctx, "limactl", cmdArgs...)
		cmd.Env = append(cmd.Environ(), "LIMA_HOME="+g.limaHome)

		output, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			return fmt.Errorf("grdctl command %v failed: %w\nOutput: %s", args, err, string(output))
		}
		log.Printf("[grd] %s: %s → %s", instance, strings.Join(args, " "), strings.TrimSpace(string(output)))
	}

	return nil
}

// EnableXRDP enables xrdp for non-GNOME desktops.
func (g *GRDManager) EnableXRDP(instance string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "limactl", "shell", instance, "--",
		"sudo", "systemctl", "enable", "--now", "xrdp")
	cmd.Env = append(cmd.Environ(), "LIMA_HOME="+g.limaHome)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("enable xrdp failed: %w\nOutput: %s", err, string(output))
	}
	log.Printf("[xrdp] %s: enabled → %s", instance, strings.TrimSpace(string(output)))
	return nil
}

// CheckRDPPort checks if port 3389 is listening inside the VM.
func (g *GRDManager) CheckRDPPort(instance string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "limactl", "shell", instance, "--",
		"ss", "-tlnp", "sport", "=", ":3389")
	cmd.Env = append(cmd.Environ(), "LIMA_HOME="+g.limaHome)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, nil
	}

	return strings.Contains(string(output), "LISTEN"), nil
}

// GetRDPStatus returns the RDP status for a VM instance, checking GNOME
// Remote Desktop first and falling back to xrdp.
func (g *GRDManager) GetRDPStatus(instance string) RDPStatus {
	status := RDPStatus{Port: 3389}

	if hasGRD, _ := g.DetectGRD(instance); hasGRD {
		status.Available = true
		status.Type = "grd"
		status.Enabled, _ = g.CheckRDPPort(instance)
		return status
	}

	if hasXRDP, _ := g.DetectXRDP(instance); hasXRDP {
		status.Available = true
		status.Type = "xrdp"
		status.Enabled, _ = g.CheckRDPPort(instance)
		return status
	}

	status.Type = "none"
	return status
}

// SetupPortForward ensures the RDP port is forwarded from the VM to the
// container. Lima with QEMU user-mode networking automatically forwards
// ports, so the VM's port 3389 is accessible at 127.0.0.1:3389.
func (g *GRDManager) SetupPortForward(instance string) (int, error) {
	return 3389, nil
}

// HandleRDPStatus returns RDP availability info for an instance.
// GET /api/instances/{name}/rdp
func (g *GRDManager) HandleRDPStatus(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "missing instance name")
		return
	}

	status := g.GetRDPStatus(name)
	writeData(w, status)
}

// HandleRDPEnable enables RDP for an instance.
// POST /api/instances/{name}/rdp/enable
func (g *GRDManager) HandleRDPEnable(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "missing instance name")
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Username == "" {
		req.Username = "lima"
	}
	if req.Password == "" {
		writeError(w, http.StatusBadRequest, "password is required")
		return
	}

	status := g.GetRDPStatus(name)

	var err error
	switch status.Type {
	case "grd":
		err = g.EnableGRD(name, req.Username, req.Password)
	case "xrdp":
		err = g.EnableXRDP(name)
	default:
		writeError(w, http.StatusNotFound, "no RDP server available in this VM (install gnome-remote-desktop or xrdp)")
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to enable RDP: %v", err))
		return
	}

	writeData(w, map[string]any{
		"enabled": true,
		"type":    status.Type,
		"message": fmt.Sprintf("RDP enabled via %s", status.Type),
	})
}
