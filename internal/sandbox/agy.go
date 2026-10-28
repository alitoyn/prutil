package sandbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
)

// agy runs terminal commands inside its built-in Standard Sandbox Mode by
// default (BypassSandbox: false, no network access, workspace-only writes) and
// protects ~/.gemini/antigravity-cli/settings.json at the system boundary
// (verified on agy 1.2.11 / 1.4.9; agyInspect does not check the binary
// version, so re-probe on version bumps). A session is contained unless it was
// launched with --dangerously-skip-permissions or the reader's settings.json
// disables the terminal sandbox or sets a toolPermission that bypasses sandbox
// approval.
func agy() Profile {
	return Profile{Kind: "agy", Bin: "agy", Launch: agyLaunch, Inspect: agyInspect}
}

func agyLaunch(_ context.Context, env Env, _ Runner, _ Target) (Launch, error) {
	args := []string{"--sandbox"}
	p, err := agyPosture(env, args)
	if err != nil {
		return Launch{}, err
	}
	return Launch{Args: args, Posture: p}, nil
}

func agyInspect(_ context.Context, env Env, _ Runner, _ string, argv []string) (Posture, error) {
	return agyPosture(env, argv)
}

func agyPosture(env Env, argv []string) (Posture, error) {
	flags := sessionFlags(argv)
	if slices.Contains(flags, "--dangerously-skip-permissions") {
		return Posture{Known: true, Detail: "agy was launched with --dangerously-skip-permissions"}, nil
	}
	var settings struct {
		EnableTerminalSandbox *bool  `json:"enableTerminalSandbox"`
		ToolPermission        string `json:"toolPermission"`
	}
	path := filepath.Join(env.Home, ".gemini", "antigravity-cli", "settings.json")
	if err := readVendorSettings(path, &settings); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Posture{Known: true, Contained: true, Detail: "agy built-in terminal sandbox"}, nil
		}
		return Posture{}, err
	}
	if settings.EnableTerminalSandbox != nil && !*settings.EnableTerminalSandbox {
		return Posture{Known: true, Detail: "agy terminal sandbox is off"}, nil
	}
	if settings.ToolPermission != "" && settings.ToolPermission != "ask" && settings.ToolPermission != "proceed-in-sandbox" {
		return Posture{Known: true, Detail: "agy tool permission bypasses sandbox approval"}, nil
	}
	return Posture{Known: true, Contained: true, Detail: "agy built-in terminal sandbox"}, nil
}
