package sandbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// agy runs terminal commands inside its built-in Standard Sandbox Mode by
// default (BypassSandbox: false, no network access, workspace-only writes) and
// protects ~/.gemini/antigravity-cli/settings.json at the system boundary. A
// session is contained unless it was launched with --dangerously-skip-permissions
// or the reader's settings.json disables the terminal sandbox or grants
// unsandboxed/wildcard permissions.
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
		Permissions           struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
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
	for _, rule := range settings.Permissions.Allow {
		rule = strings.TrimSpace(rule)
		if strings.HasPrefix(rule, "unsandboxed(") || rule == "unsandboxed" ||
			(strings.HasPrefix(rule, "read_url(") || strings.HasPrefix(rule, "execute_url(")) &&
				strings.Contains(rule, "*") {
			return Posture{Known: true, Detail: "agy policy permits an unrestricted tool"}, nil
		}
	}
	return Posture{Known: true, Contained: true, Detail: "agy built-in terminal sandbox"}, nil
}
