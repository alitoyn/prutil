package sandbox_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeVendorPolicy(t *testing.T, path string, doc any) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	data, err := json.Marshal(doc)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o600))
}

func copilotPolicy(home string) map[string]any {
	paths := []string{}
	for _, name := range []string{".ssh", ".aws", ".gnupg", ".netrc", ".config/herdr", ".config/prutil"} {
		paths = append(paths, filepath.Join(home, name))
	}
	return map[string]any{"sandbox": map[string]any{
		"allowBypass": false,
		"userPolicy": map[string]any{
			"filesystem": map[string]any{"deniedPaths": paths},
			"network":    map[string]any{"allowLocalNetwork": false},
		},
	}}
}

func agyPolicy() map[string]any {
	return map[string]any{
		"enableTerminalSandbox": true,
		"toolPermission":        "proceed-in-sandbox",
		"permissions":           map[string]any{"allow": []string{"read_url(github.com)", "read_url(api.github.com)"}},
	}
}

func TestCopilotLaunchNeedsTheReadersStrictPolicy(t *testing.T) {
	t.Setenv("COPILOT_HOME", "")
	s, env := newSandbox(t, &fakeClaude{})
	path := filepath.Join(env.Home, ".copilot", "settings.json")
	l, err := s.Launch(context.Background(), "copilot", target(t.TempDir()))
	require.NoError(t, err)
	assert.False(t, l.Posture.Contained, "a missing policy does not pass the automatic gate")

	writeVendorPolicy(t, path, copilotPolicy(env.Home))
	l, err = s.Launch(context.Background(), "copilot", target(t.TempDir()))
	require.NoError(t, err)
	assert.Equal(t, []string{"--experimental", "--sandbox",
		"--secret-env-vars=AWS_ACCESS_KEY_ID,AWS_SECRET_ACCESS_KEY,AWS_SESSION_TOKEN"}, l.Args)
	assert.True(t, l.Posture.Contained)
	assert.True(t, l.Posture.Strict)
}

func TestCopilotInspectRejectsMissingFlagsAndPermissivePolicies(t *testing.T) {
	t.Setenv("COPILOT_HOME", "")
	for _, tc := range []struct {
		name   string
		argv   []string
		change func(map[string]any)
	}{
		{"a command line without a sandbox switch", []string{"copilot"}, nil},
		{"a prompt mentioning a sandbox switch", []string{"copilot", "-p", "--sandbox"}, nil},
		{"a sandbox switch without experimental features", []string{"copilot", "--sandbox"}, nil},
		{"a command line disabling experimental features", []string{"copilot", "--sandbox", "--no-experimental"}, nil},
		{"a policy that permits bypass", []string{"copilot", "--sandbox"}, func(doc map[string]any) {
			doc["sandbox"].(map[string]any)["allowBypass"] = true
		}},
		{"a policy that permits the local network", []string{"copilot", "--sandbox"}, func(doc map[string]any) {
			doc["sandbox"].(map[string]any)["userPolicy"].(map[string]any)["network"].(map[string]any)["allowLocalNetwork"] = true
		}},
		{"a policy without a secret path denial", []string{"copilot", "--sandbox"}, func(doc map[string]any) {
			doc["sandbox"].(map[string]any)["userPolicy"].(map[string]any)["filesystem"].(map[string]any)["deniedPaths"] = []string{}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, env := newSandbox(t, &fakeClaude{})
			doc := copilotPolicy(env.Home)
			if tc.change != nil {
				tc.change(doc)
			}
			writeVendorPolicy(t, filepath.Join(env.Home, ".copilot", "settings.json"), doc)
			p, err := s.Inspect(context.Background(), "copilot", t.TempDir(), tc.argv)
			require.NoError(t, err)
			assert.True(t, p.Known)
			assert.False(t, p.Contained)
		})
	}
}

func TestCopilotInspectReadsTheSelectedHomeWithoutRewritingIt(t *testing.T) {
	s, env := newSandbox(t, &fakeClaude{})
	dir := t.TempDir()
	t.Setenv("COPILOT_HOME", dir)
	path := filepath.Join(dir, "settings.json")
	writeVendorPolicy(t, path, copilotPolicy(env.Home))
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	p, err := s.Inspect(context.Background(), "copilot", t.TempDir(), []string{"copilot", "--experimental", "--sandbox"})
	require.NoError(t, err)
	assert.True(t, p.Contained)
	assert.True(t, p.Strict)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, before, data,
		"the reader's settings must not be replaced or moved")
}

func TestAgyLaunchUsesTheBuiltInTerminalSandboxByDefault(t *testing.T) {
	s, env := newSandbox(t, &fakeClaude{})
	path := filepath.Join(env.Home, ".gemini", "antigravity-cli", "settings.json")
	l, err := s.Launch(context.Background(), "agy", target(t.TempDir()))
	require.NoError(t, err)
	assert.Empty(t, l.Args)
	assert.True(t, l.Posture.Contained, "agy runs inside its built-in terminal sandbox by default")
	assert.False(t, l.Posture.Strict, "a person can approve an unsandboxed command")

	writeVendorPolicy(t, path, map[string]any{"enableTerminalSandbox": false})
	l, err = s.Launch(context.Background(), "agy", target(t.TempDir()))
	require.NoError(t, err)
	assert.False(t, l.Posture.Contained, "turning the terminal sandbox off in settings.json disables containment")
}

func TestAgyInspectRejectsAnUncontainedCommandOrPolicy(t *testing.T) {
	for _, tc := range []struct {
		name   string
		argv   []string
		change func(map[string]any)
	}{
		{"a command line that skips permissions", []string{"agy", "--dangerously-skip-permissions"}, nil},
		{"a policy without the terminal sandbox", []string{"agy"}, func(doc map[string]any) {
			doc["enableTerminalSandbox"] = false
		}},
		{"a policy with a tool permission that bypasses sandbox approval", []string{"agy"}, func(doc map[string]any) {
			doc["toolPermission"] = "proceed-always"
		}},
		{"a policy allowing an unrestricted domain", []string{"agy"}, func(doc map[string]any) {
			doc["permissions"].(map[string]any)["allow"] = []string{"read_url(*)"}
		}},
		{"a policy allowing an unrestricted execute_url domain", []string{"agy"}, func(doc map[string]any) {
			doc["permissions"].(map[string]any)["allow"] = []string{"execute_url(*)"}
		}},
		{"a policy allowing unsandboxed commands", []string{"agy"}, func(doc map[string]any) {
			doc["permissions"].(map[string]any)["allow"] = []string{"read_url(github.com)", "unsandboxed(*)"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, env := newSandbox(t, &fakeClaude{})
			doc := agyPolicy()
			if tc.change != nil {
				tc.change(doc)
			}
			writeVendorPolicy(t, filepath.Join(env.Home, ".gemini", "antigravity-cli", "settings.json"), doc)
			p, err := s.Inspect(context.Background(), "agy", t.TempDir(), tc.argv)
			require.NoError(t, err)
			assert.True(t, p.Known)
			assert.False(t, p.Contained)
		})
	}
}

func TestAgyInspectAcceptsDefaultAndSandboxedSessions(t *testing.T) {
	for _, tc := range []struct {
		name string
		argv []string
		doc  map[string]any
	}{
		{
			name: "a normal agy launch with no settings file",
			argv: []string{"agy"},
		},
		{
			name: "a normal agy launch with an explicit sandbox policy",
			argv: []string{"agy"},
			doc:  agyPolicy(),
		},
		{
			name: "an agy launch with --sandbox and ask tool permission",
			argv: []string{"agy", "--sandbox"},
			doc:  map[string]any{"toolPermission": "ask", "permissions": map[string]any{"allow": []string{"sandboxed(git status)"}}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, env := newSandbox(t, &fakeClaude{})
			if tc.doc != nil {
				writeVendorPolicy(t, filepath.Join(env.Home, ".gemini", "antigravity-cli", "settings.json"), tc.doc)
			}
			p, err := s.Inspect(context.Background(), "agy", t.TempDir(), tc.argv)
			require.NoError(t, err)
			assert.True(t, p.Known)
			assert.True(t, p.Contained)
			assert.False(t, p.Strict)
		})
	}
}

func TestMalformedVendorPolicyIsAnErrorRatherThanAContainedAnswer(t *testing.T) {
	for _, kind := range []string{"copilot", "agy"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv("COPILOT_HOME", "")
			s, env := newSandbox(t, &fakeClaude{})
			path := filepath.Join(env.Home, ".copilot", "settings.json")
			if kind == "agy" {
				path = filepath.Join(env.Home, ".gemini", "antigravity-cli", "settings.json")
			}
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
			require.NoError(t, os.WriteFile(path, []byte("{"), 0o600))
			_, err := s.Launch(context.Background(), kind, target(t.TempDir()))
			require.Error(t, err)
			assert.Contains(t, err.Error(), path)
		})
	}
}
