// Copyright 2026 runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package browserbridge

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	HostName    = "com.runtime.browser_bridge"
	ExtensionID = "agidgjchdiodbkkaggifpflepjgoedff"
	Protocol    = 1
)

type State struct {
	Protocol    int       `json:"protocol"`
	Address     string    `json:"address"`
	Token       string    `json:"token"`
	Origin      string    `json:"origin"`
	ExtensionID string    `json:"extension_id"`
	PID         int       `json:"pid"`
	StartedAt   time.Time `json:"started_at"`
}

func StatePath(home string) string {
	return filepath.Join(home, "browser-extension", "bridge.json")
}

func ReadState(home string) (State, error) {
	data, err := os.ReadFile(StatePath(home))
	if err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, err
	}
	if state.Protocol != Protocol || strings.TrimSpace(state.Address) == "" || strings.TrimSpace(state.Token) == "" {
		return State{}, errors.New("invalid Runtime Chrome bridge state")
	}
	return state, nil
}

func resolveRuntimeHome() (string, error) {
	if value := strings.TrimSpace(os.Getenv("RUNTIME_CORE_HOME")); value != "" {
		return filepath.Abs(value)
	}
	if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
		data, err := os.ReadFile(filepath.Join(local, "RuntimeCore", "install.json"))
		if err == nil {
			var settings struct {
				RuntimeHome string `json:"runtime_home"`
			}
			if json.Unmarshal(data, &settings) == nil && strings.TrimSpace(settings.RuntimeHome) != "" {
				return filepath.Abs(settings.RuntimeHome)
			}
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".runtime-core"), nil
}
