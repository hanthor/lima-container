package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

var wsUpgrader = websocket.Upgrader{
	// Allow all origins — access control is handled by the container network boundary.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// ShellWS upgrades the connection to a WebSocket and proxies a PTY SSH session
// into the named Lima instance.
//
// Protocol (simple binary framing):
//   - Server → Client: binary frames — raw terminal output
//   - Client → Server: binary frames — raw keyboard/paste input
//   - Client → Server: text frame   — JSON resize event {"type":"resize","cols":N,"rows":N}
func (h *Handler) ShellWS(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	inst, err := h.lima.Get(name)
	if err != nil {
		http.Error(w, "instance not found", http.StatusNotFound)
		return
	}
	if inst.Status != "Running" {
		http.Error(w, "instance not running — start it first", http.StatusServiceUnavailable)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ShellWS upgrade error for %q: %v", name, err)
		return
	}
	defer conn.Close()

	limaHome := os.Getenv("LIMA_HOME")
	if limaHome == "" {
		limaHome = "/var/lib/lima"
	}

	// limactl shell opens an SSH session into the instance with a proper PTY.
	// We run it via lima-as-user so it executes as UID 1000 (lima) and has
	// access to the Lima SSH key and config.
	cmd := exec.Command("lima-as-user", "limactl", "shell", name)
	cmd.Env = append(os.Environ(),
		"LIMA_HOME="+limaHome,
		"TERM=xterm-256color",
	)

	ptmx, err := pty.Start(cmd)
	if err != nil {
		log.Printf("ShellWS pty.Start error for %q: %v", name, err)
		conn.WriteMessage(websocket.TextMessage, //nolint:errcheck
			[]byte(fmt.Sprintf("\r\n[error starting shell: %v]\r\n", err)))
		return
	}
	defer func() {
		ptmx.Close()
		if cmd.Process != nil {
			cmd.Process.Kill() //nolint:errcheck
		}
		cmd.Wait() //nolint:errcheck
	}()

	// PTY → WebSocket (terminal output)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				if werr := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// WebSocket → PTY (keyboard input + resize)
	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		switch msgType {
		case websocket.BinaryMessage:
			ptmx.Write(data) //nolint:errcheck
		case websocket.TextMessage:
			var msg struct {
				Type string `json:"type"`
				Cols uint16 `json:"cols"`
				Rows uint16 `json:"rows"`
			}
			if json.Unmarshal(data, &msg) == nil && msg.Type == "resize" && msg.Cols > 0 && msg.Rows > 0 {
				pty.Setsize(ptmx, &pty.Winsize{Cols: msg.Cols, Rows: msg.Rows}) //nolint:errcheck
			}
		}
	}
}
