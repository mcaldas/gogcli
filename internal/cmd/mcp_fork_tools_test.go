package cmd

import (
	"slices"
	"testing"

	"github.com/openclaw/gogcli/internal/config"
)

// forkCreateTools are the fork tools gated by mcpCapabilityCreate. Each either
// creates something durable or destroys data, and calendar_create emails
// invitations to attendees, so --allow-write alone must never expose them.
var forkCreateTools = []string{"drive_upload", "drive_mkdir", "calendar_create", "sheets_clear"}

func forkToolNames(tools []mcpToolSpec) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

// TestMCPCreateCapabilityNotGrantedBySelector is the security property the
// fork exists for: no --allow-tool selector, however specific, may substitute
// for --allow-create. Mirrors TestMCPGmailCapabilityCeilings upstream.
func TestMCPCreateCapabilityNotGrantedBySelector(t *testing.T) {
	broad := []string{"drive", "drive.*", "calendar", "sheets", "write", "*", "all"}
	selectors := make([]string, 0, len(broad)+len(forkCreateTools))
	selectors = append(selectors, broad...)
	selectors = append(selectors, forkCreateTools...)
	for _, selector := range selectors {
		t.Run(selector, func(t *testing.T) {
			policy := config.MCPPolicy{AllowTools: []string{selector}, AllowWrite: true}
			configured, err := mcpEnabledToolsWithPolicy(McpCmd{}, nil, policy)
			if err != nil {
				t.Fatal(err)
			}
			runtime := mcpEnabledTools(McpCmd{AllowWrite: true, AllowTool: []string{selector}}, nil)
			for _, tools := range [][]mcpToolSpec{configured, runtime} {
				for _, tool := range tools {
					if tool.Capability == mcpCapabilityCreate {
						t.Fatalf("selector %q exposed create tool %s without --allow-create", selector, tool.Name)
					}
				}
			}
		})
	}
}

// TestMCPCreateCapabilityGating covers the flag itself: off hides all four, on
// exposes all four, and --readonly overrides the flag entirely.
func TestMCPCreateCapabilityGating(t *testing.T) {
	for _, tc := range []struct {
		name             string
		create, readonly bool
		wantExposed      bool
	}{
		{name: "write-only", wantExposed: false},
		{name: "create", create: true, wantExposed: true},
		{name: "readonly-overrides-create", create: true, readonly: true, wantExposed: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			flags := &RootFlags{ReadOnly: tc.readonly}
			policy := config.MCPPolicy{AllowTools: []string{"*"}, AllowWrite: true, AllowCreate: tc.create}
			configured, err := mcpEnabledToolsWithPolicy(McpCmd{}, flags, policy)
			if err != nil {
				t.Fatal(err)
			}
			runtime := mcpEnabledTools(McpCmd{AllowWrite: true, AllowCreate: tc.create}, flags)
			for _, tools := range [][]mcpToolSpec{configured, runtime} {
				names := forkToolNames(tools)
				for _, want := range forkCreateTools {
					if got := slices.Contains(names, want); got != tc.wantExposed {
						t.Fatalf("%s exposed=%v, want %v", want, got, tc.wantExposed)
					}
				}
			}
		})
	}
}

// TestMCPCreateCannotWidenPolicy locks in upstream's ceiling rule: the runtime
// flag may narrow a configured policy but never widen it.
func TestMCPCreateCannotWidenPolicy(t *testing.T) {
	policy := config.MCPPolicy{AllowTools: []string{"*"}, AllowWrite: true}
	if _, err := mcpEnabledToolsWithPolicy(McpCmd{AllowCreate: true}, nil, policy); err == nil {
		t.Fatal("--allow-create widened a policy with allow_create unset")
	}
}

// TestMCPForkToolsRegistered guards the wiring: every fork tool must reach
// mcpAllTools, and the read tier must stay free of write-risk tools.
func TestMCPForkToolsRegistered(t *testing.T) {
	all := forkToolNames(mcpAllTools())
	for _, tool := range mcpForkTools() {
		if !slices.Contains(all, tool.Name) {
			t.Fatalf("fork tool %s is not registered in mcpAllTools", tool.Name)
		}
	}
	readOnly := mcpEnabledTools(McpCmd{}, nil)
	for _, tool := range readOnly {
		if tool.Risk != mcpRiskRead || tool.Capability != "" {
			t.Fatalf("default surface exposed %s (risk=%s capability=%s)", tool.Name, tool.Risk, tool.Capability)
		}
	}
	for _, want := range forkCreateTools {
		if slices.Contains(forkToolNames(readOnly), want) {
			t.Fatalf("create tool %s exposed on the default read-only surface", want)
		}
	}
}
