package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// Instance represents a Lima VM as returned by limactl list --json.
type Instance struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Dir    string `json:"dir"`
	Arch   string `json:"arch"`
	CPUs   int    `json:"cpus"`
	Memory int64  `json:"memory"`
	Disk   int64  `json:"disk"`
	// raw stores the full JSON object so we can proxy it to the API client.
	raw json.RawMessage
}

func (i *Instance) UnmarshalJSON(data []byte) error {
	// Store a validated copy of the raw JSON.
	i.raw = make(json.RawMessage, len(data))
	copy(i.raw, data)

	// Unmarshal known fields without infinite recursion via alias type.
	type Alias Instance
	aux := &struct {
		*Alias
	}{Alias: (*Alias)(i)}
	return json.Unmarshal(data, aux)
}

func (i Instance) MarshalJSON() ([]byte, error) {
	if len(i.raw) > 0 {
		// Validate before returning to prevent encoding errors.
		if json.Valid(i.raw) {
			return i.raw, nil
		}
	}
	type Alias Instance
	return json.Marshal((Alias)(i))
}

// LimaCtl wraps the limactl CLI.
type LimaCtl struct {
	home string
	// writeMu serializes mutating limactl operations.
	writeMu sync.Mutex
}

func NewLimaCtl(home string) *LimaCtl {
	return &LimaCtl{home: home}
}

// Home returns the LIMA_HOME directory path.
func (l *LimaCtl) Home() string { return l.home }

// run executes lima-as-user limactl <args> and returns stdout.
func (l *LimaCtl) run(args ...string) ([]byte, error) {
	cmdArgs := append([]string{"limactl"}, args...)
	cmd := exec.Command("lima-as-user", cmdArgs...)
	cmd.Env = append(cmd.Environ(), "LIMA_HOME="+l.home)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := extractLimaError(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("%s", msg)
	}
	return stdout.Bytes(), nil
}

// List returns all Lima instances.
func (l *LimaCtl) List() ([]Instance, error) {
	out, err := l.run("list", "--json")
	if err != nil {
		return nil, err
	}
	return parseNDJSON(out)
}

// Get returns a single instance by name.
func (l *LimaCtl) Get(name string) (*Instance, error) {
	instances, err := l.List()
	if err != nil {
		return nil, err
	}
	for _, inst := range instances {
		if inst.Name == name {
			return &inst, nil
		}
	}
	return nil, fmt.Errorf("instance %q not found", name)
}

// Start starts a stopped instance.
func (l *LimaCtl) Start(name string) error {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	_, err := l.run("start", name)
	return err
}

// Stop stops a running instance.
func (l *LimaCtl) Stop(name string) error {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	_, err := l.run("stop", name)
	return err
}

// Delete force-deletes an instance.
func (l *LimaCtl) Delete(name string) error {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	_, err := l.run("delete", "--force", name)
	return err
}

// Create creates and starts a VM from a template path.
func (l *LimaCtl) Create(templatePath, name string) error {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()

	args := []string{"start", "--tty=false"}
	if name != "" {
		args = append(args, "--name="+name)
	}
	args = append(args, templatePath)

	_, err := l.run(args...)
	return err
}

// CreateBootc starts a VM built from a bootc image. bootc VMs don't run
// cloud-init so Lima's SSH provisioning will time out; we use a short timeout
// and let the caller check IsRunning() to determine actual success.
func (l *LimaCtl) CreateBootc(qcow2Path, name string, cpus int, memory string) error {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()

	args := []string{"start", "--tty=false", "--timeout=3m"}
	if cpus > 0 {
		args = append(args, fmt.Sprintf("--cpus=%d", cpus))
	}
	if memory != "" {
		args = append(args, fmt.Sprintf("--memory=%s", memory))
	}
	if name != "" {
		args = append(args, "--name="+name)
	}
	args = append(args, qcow2Path)

	_, err := l.run(args...)
	return err
}

// IsRunning returns true if the named Lima instance is in Running status.
func (l *LimaCtl) IsRunning(name string) (bool, error) {
	out, err := l.run("list", "--json", name)
	if err != nil {
		return false, err
	}
	var inst Instance
	if jsonErr := json.Unmarshal(out, &inst); jsonErr != nil {
		// limactl list may return NDJSON; try line by line.
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			if jsonErr2 := json.Unmarshal([]byte(line), &inst); jsonErr2 == nil && inst.Name == name {
				return inst.Status == "Running", nil
			}
		}
		return false, nil
	}
	return inst.Status == "Running", nil
}

// Info returns limactl info output.
func (l *LimaCtl) Info() (json.RawMessage, error) {
	out, err := l.run("info")
	if err != nil {
		return nil, err
	}
	// Validate it's JSON.
	var raw json.RawMessage
	if err := json.Unmarshal(out, &raw); err != nil {
		return out, nil
	}
	return raw, nil
}

// parseNDJSON parses newline-delimited JSON (one object per line).
func parseNDJSON(data []byte) ([]Instance, error) {
	var instances []Instance
	dec := json.NewDecoder(bytes.NewReader(data))
	for dec.More() {
		var inst Instance
		if err := dec.Decode(&inst); err != nil {
			return instances, fmt.Errorf("parsing instance JSON: %w", err)
		}
		instances = append(instances, inst)
	}
	return instances, nil
}

// extractLimaError pulls the most useful error message from limactl's
// NDJSON log output. It prefers the last "fatal" or "error" level line's
// "msg" field, falling back to the last non-empty line.
func extractLimaError(raw string) string {
	var lastMsg string
	var lastFatal string

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Try to parse as limactl NDJSON log line.
		var entry struct {
			Level string `json:"level"`
			Msg   string `json:"msg"`
		}
		if json.Unmarshal([]byte(line), &entry) == nil && entry.Msg != "" {
			lastMsg = entry.Msg
			if entry.Level == "fatal" || entry.Level == "error" {
				lastFatal = entry.Msg
			}
		} else {
			lastMsg = line
		}
	}

	if lastFatal != "" {
		return lastFatal
	}
	return strings.TrimSpace(lastMsg)
}
