package cmd

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/openclaw/gogcli/internal/config"
	"github.com/openclaw/gogcli/internal/googleapi"
)

func TestMCPEnabledToolsDefaultReadOnly(t *testing.T) {
	tools := mcpEnabledTools(McpCmd{})
	if len(tools) == 0 {
		t.Fatal("expected default tools")
	}
	for _, tool := range tools {
		if tool.Risk != mcpRiskRead {
			t.Fatalf("default enabled write tool %s", tool.Name)
		}
	}
	if hasMCPTool(tools, "docs_write") {
		t.Fatal("docs_write should require --allow-write")
	}
	if !hasMCPTool(tools, "gmail_search") {
		t.Fatal("gmail_search should be enabled by default")
	}
}

func TestMCPEnabledToolsAllowWriteAndFilter(t *testing.T) {
	tools := mcpEnabledTools(McpCmd{AllowWrite: true, AllowTool: []string{"docs.*"}})
	if !hasMCPTool(tools, "docs_get") || !hasMCPTool(tools, "docs_write") {
		t.Fatalf("expected docs read and write tools, got %#v", toolNames(tools))
	}
	if hasMCPTool(tools, "gmail_search") {
		t.Fatalf("gmail tool leaked through docs filter: %#v", toolNames(tools))
	}
}

func TestMCPPolicyDefaultsToReadOnly(t *testing.T) {
	policy, err := selectMCPPolicy(config.MCPConfig{}, "")
	if err != nil {
		t.Fatalf("selectMCPPolicy: %v", err)
	}
	tools, err := mcpEnabledToolsWithPolicy(McpCmd{}, &RootFlags{}, policy)
	if err != nil {
		t.Fatalf("mcpEnabledToolsWithPolicy: %v", err)
	}
	if !hasMCPTool(tools, "gmail_search") || hasMCPTool(tools, "docs_write") {
		t.Fatalf("unexpected default policy tools: %#v", toolNames(tools))
	}
}

func TestMCPPolicyAccountReplacesGlobalAndEnablesNarrowWrites(t *testing.T) {
	cfg := config.MCPConfig{
		MCPPolicy: config.MCPPolicy{AllowTools: []string{"read"}},
		Accounts: map[string]config.MCPPolicy{
			" Personal@Example.com ": {AllowTools: []string{"docs.*"}, AllowWrite: true},
		},
	}
	policy, err := selectMCPPolicy(cfg, "personal@example.com")
	if err != nil {
		t.Fatalf("selectMCPPolicy: %v", err)
	}
	tools, err := mcpEnabledToolsWithPolicy(McpCmd{}, &RootFlags{}, policy)
	if err != nil {
		t.Fatalf("mcpEnabledToolsWithPolicy: %v", err)
	}
	if !hasMCPTool(tools, "docs_get") || !hasMCPTool(tools, "docs_write") {
		t.Fatalf("expected configured Docs tools: %#v", toolNames(tools))
	}
	if hasMCPTool(tools, "gmail_search") {
		t.Fatalf("global policy leaked into account replacement: %#v", toolNames(tools))
	}
}

func TestMCPPolicyRuntimeCanOnlyNarrow(t *testing.T) {
	policy, err := normalizeMCPPolicy(config.MCPPolicy{AllowTools: []string{"docs.*"}, AllowWrite: true})
	if err != nil {
		t.Fatalf("normalizeMCPPolicy: %v", err)
	}
	tools, err := mcpEnabledToolsWithPolicy(McpCmd{AllowTool: []string{"docs_get"}}, &RootFlags{}, policy)
	if err != nil {
		t.Fatalf("mcpEnabledToolsWithPolicy: %v", err)
	}
	if got := toolNames(tools); len(got) != 1 || got[0] != "docs_get" {
		t.Fatalf("runtime narrowed tools = %#v", got)
	}

	_, err = mcpEnabledToolsWithPolicy(McpCmd{AllowWrite: true}, &RootFlags{}, config.MCPPolicy{AllowTools: []string{"read"}})
	if err == nil || !strings.Contains(err.Error(), "cannot widen") {
		t.Fatalf("allow-write widening error = %v", err)
	}
}

func TestMCPPolicyReadOnlyRootHidesConfiguredWrites(t *testing.T) {
	policy, err := normalizeMCPPolicy(config.MCPPolicy{AllowTools: []string{"docs.*"}, AllowWrite: true})
	if err != nil {
		t.Fatalf("normalizeMCPPolicy: %v", err)
	}
	tools, err := mcpEnabledToolsWithPolicy(McpCmd{}, &RootFlags{ReadOnly: true}, policy)
	if err != nil {
		t.Fatalf("mcpEnabledToolsWithPolicy: %v", err)
	}
	if !hasMCPTool(tools, "docs_get") || hasMCPTool(tools, "docs_write") {
		t.Fatalf("readonly tools = %#v", toolNames(tools))
	}
}

func TestMCPPolicySendTierRequiresConfiguredAllowSend(t *testing.T) {
	// A configured policy must gate send-risk tools exactly as it gates writes;
	// otherwise an mcp config block silently bypasses --allow-send.
	policy, err := normalizeMCPPolicy(config.MCPPolicy{AllowTools: []string{"gmail.*"}})
	if err != nil {
		t.Fatalf("normalizeMCPPolicy: %v", err)
	}
	tools, err := mcpEnabledToolsWithPolicy(McpCmd{}, &RootFlags{}, policy)
	if err != nil {
		t.Fatalf("mcpEnabledToolsWithPolicy: %v", err)
	}
	if !hasMCPTool(tools, "gmail_search") || hasMCPTool(tools, "gmail_send") {
		t.Fatalf("send tool exposed without allow_send: %#v", toolNames(tools))
	}

	if _, err = mcpEnabledToolsWithPolicy(McpCmd{AllowSend: true}, &RootFlags{}, policy); err == nil ||
		!strings.Contains(err.Error(), "cannot widen") {
		t.Fatalf("allow-send widening error = %v", err)
	}

	sendPolicy, err := normalizeMCPPolicy(config.MCPPolicy{AllowTools: []string{"gmail.*"}, AllowSend: true})
	if err != nil {
		t.Fatalf("normalizeMCPPolicy: %v", err)
	}
	tools, err = mcpEnabledToolsWithPolicy(McpCmd{}, &RootFlags{}, sendPolicy)
	if err != nil {
		t.Fatalf("mcpEnabledToolsWithPolicy: %v", err)
	}
	if !hasMCPTool(tools, "gmail_send") {
		t.Fatalf("configured send tools missing: %#v", toolNames(tools))
	}

	tools, err = mcpEnabledToolsWithPolicy(McpCmd{}, &RootFlags{ReadOnly: true}, sendPolicy)
	if err != nil {
		t.Fatalf("mcpEnabledToolsWithPolicy: %v", err)
	}
	if hasMCPTool(tools, "gmail_send") {
		t.Fatalf("readonly root kept send tools: %#v", toolNames(tools))
	}

	if _, err = normalizeMCPPolicy(config.MCPPolicy{AllowSend: true}); err == nil ||
		!strings.Contains(err.Error(), "allow_send requires an explicit allow_tools list") {
		t.Fatalf("allow_send without selectors error = %v", err)
	}
}

func TestMCPPolicyRejectsUnsafeOrUnknownConfig(t *testing.T) {
	for _, policy := range []config.MCPPolicy{
		{AllowWrite: true},
		{AllowTools: []string{}},
		{AllowTools: []string{"not_a_tool"}},
	} {
		if _, err := normalizeMCPPolicy(policy); err == nil {
			t.Fatalf("expected policy error for %#v", policy)
		}
	}

	duplicateAccount := " user@example.com "
	_, err := selectMCPPolicy(config.MCPConfig{Accounts: map[string]config.MCPPolicy{
		"User@example.com": {},
		duplicateAccount:   {},
	}}, "user@example.com")
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate account error = %v", err)
	}

	_, err = selectMCPPolicy(config.MCPConfig{
		Accounts: map[string]config.MCPPolicy{
			"selected@example.com": {AllowTools: []string{"read"}},
			"other@example.com":    {AllowTools: []string{"not_a_tool"}},
		},
	}, "selected@example.com")
	if err == nil || !strings.Contains(err.Error(), "other@example.com") || !strings.Contains(err.Error(), "matches no tool") {
		t.Fatalf("unselected account validation error = %v", err)
	}
}

func TestMCPPolicyAccountResolutionPinsAliasAndRejectsUnverifiableIdentity(t *testing.T) {
	store := config.NewConfigStore(config.Layout{ConfigDir: t.TempDir()})
	if err := store.Write(config.File{AccountAliases: map[string]string{"personal": "Personal@Example.com"}}); err != nil {
		t.Fatalf("write config: %v", err)
	}
	flags := &RootFlags{
		Account: "personal",
		configStoreResolver: func() (*config.ConfigStore, error) {
			return store, nil
		},
	}
	account, err := resolveMCPPolicyAccount(flags)
	if err != nil {
		t.Fatalf("resolveMCPPolicyAccount: %v", err)
	}
	if account != "Personal@Example.com" {
		t.Fatalf("resolved account = %q", account)
	}

	for _, unverifiable := range []*RootFlags{
		{AccessToken: "token", Account: "label@example.com"},
		{authMode: googleapi.AuthModeADC, Account: "label@example.com"},
	} {
		account, err := resolveMCPPolicyAccount(unverifiable)
		if err != nil || account != "" {
			t.Fatalf("unverifiable identity resolution = %q, %v", account, err)
		}
	}
}

func TestMCPDiscoveryToolsDefaultReadOnly(t *testing.T) {
	tools := mcpEnabledTools(McpCmd{})
	if !hasMCPTool(tools, "gog_list_commands") || !hasMCPTool(tools, "gog_describe") {
		t.Fatalf("discovery tools should be enabled by default, got %#v", toolNames(tools))
	}
	for _, name := range []string{"gog_list_commands", "gog_describe"} {
		tool := findMCPTool(t, name)
		if tool.Risk != mcpRiskRead {
			t.Fatalf("%s should be read-risk, got %q", name, tool.Risk)
		}
	}

	// list_commands: no path -> just --help (top level).
	list := findMCPTool(t, "gog_list_commands")
	args, err := list.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{}}})
	if err != nil || strings.Join(args, "\x00") != "--help" {
		t.Fatalf("list top-level args = %#v (err %v), want [--help]", args, err)
	}
	// list_commands: path -> tokens then --help.
	args, err = list.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"path": "gmail drafts"}}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(args, "\x00") != strings.Join([]string{"gmail", "drafts", "--help"}, "\x00") {
		t.Fatalf("list path args = %#v", args)
	}

	// describe: command -> schema <tokens>.
	desc := findMCPTool(t, "gog_describe")
	args, err = desc.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"command": "drive ls"}}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(args, "\x00") != strings.Join([]string{"schema", "drive", "ls"}, "\x00") {
		t.Fatalf("describe args = %#v", args)
	}
	if _, err := desc.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{}}}); err == nil {
		t.Fatal("describe should require 'command'")
	}
}

func TestMCPEnabledToolsAllowSendGating(t *testing.T) {
	sendTools := []string{"gmail_send", "drive_upload", "drive_mkdir", "calendar_create"}

	// Default: no send tools.
	def := mcpEnabledTools(McpCmd{})
	for _, name := range sendTools {
		if hasMCPTool(def, name) {
			t.Fatalf("%s should require --allow-send", name)
		}
	}

	// --allow-write must NOT expose send tools (the key isolation property).
	write := mcpEnabledTools(McpCmd{AllowWrite: true})
	for _, name := range sendTools {
		if hasMCPTool(write, name) {
			t.Fatalf("%s leaked through --allow-write; must require --allow-send", name)
		}
	}

	// --allow-send exposes all send tools but no write tools.
	send := mcpEnabledTools(McpCmd{AllowSend: true})
	for _, name := range sendTools {
		if !hasMCPTool(send, name) {
			t.Fatalf("%s should be enabled by --allow-send, got %#v", name, toolNames(send))
		}
	}
	if hasMCPTool(send, "docs_write") || hasMCPTool(send, "sheets_update_range") {
		t.Fatalf("write tools leaked through --allow-send: %#v", toolNames(send))
	}

	// --allow-send composes with --allow-tool.
	filtered := mcpEnabledTools(McpCmd{AllowSend: true, AllowTool: []string{"gmail.*"}})
	if !hasMCPTool(filtered, "gmail_send") {
		t.Fatalf("gmail_send should survive gmail.* filter, got %#v", toolNames(filtered))
	}
	if hasMCPTool(filtered, "drive_upload") {
		t.Fatalf("drive_upload leaked through gmail.* filter: %#v", toolNames(filtered))
	}
}

func TestMCPGmailSendBuildArgs(t *testing.T) {
	tool := findMCPTool(t, "gmail_send")
	args, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{
			"to":      "a@example.com,b@example.com",
			"subject": "Hello",
			"body":    "Body text",
			"cc":      "c@example.com",
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"gmail", "send", "--to", "a@example.com,b@example.com", "--subject", "Hello", "--body", "Body text", "--cc", "c@example.com"}
	if strings.Join(args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args = %#v, want %#v", args, want)
	}

	// Comma-separated attachments become repeated --attach flags (spaces trimmed, empties dropped).
	attArgs, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{
			"to":      "a@example.com",
			"subject": "Hi",
			"body":    "b",
			"attach":  "/tmp/report.pdf, /tmp/chart.png ,",
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	wantAtt := []string{"gmail", "send", "--to", "a@example.com", "--subject", "Hi", "--body", "b", "--attach", "/tmp/report.pdf", "--attach", "/tmp/chart.png"}
	if strings.Join(attArgs, "\x00") != strings.Join(wantAtt, "\x00") {
		t.Fatalf("attach args = %#v, want %#v", attArgs, wantAtt)
	}

	// Missing both body and body_html is rejected.
	if _, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{"to": "a@example.com", "subject": "Hi"},
	}}); err == nil {
		t.Fatal("expected error when neither body nor body_html provided")
	}

	// Missing required recipient is rejected.
	if _, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{"subject": "Hi", "body": "x"},
	}}); err == nil {
		t.Fatal("expected error when 'to' missing")
	}
}

func TestMCPExpandedTypedToolsGatingAndArgs(t *testing.T) {
	// New write-tier tools: exposed by --allow-write, NOT by default, NOT by --allow-send.
	writeTools := []string{"gmail_drafts_create", "drive_download", "drive_move", "drive_rename", "calendar_edit", "docs_create", "sheets_create", "sheets_append"}
	def := mcpEnabledTools(McpCmd{})
	write := mcpEnabledTools(McpCmd{AllowWrite: true})
	send := mcpEnabledTools(McpCmd{AllowSend: true})
	for _, name := range writeTools {
		if hasMCPTool(def, name) {
			t.Fatalf("%s should require --allow-write", name)
		}
		if !hasMCPTool(write, name) {
			t.Fatalf("%s should be enabled by --allow-write", name)
		}
		if hasMCPTool(send, name) {
			t.Fatalf("%s should NOT be exposed by --allow-send alone", name)
		}
	}
	// sheets_clear is destructive -> send tier, NOT write.
	if hasMCPTool(write, "sheets_clear") {
		t.Fatal("sheets_clear must NOT be under --allow-write (destructive)")
	}
	if !hasMCPTool(send, "sheets_clear") {
		t.Fatal("sheets_clear should be under --allow-send")
	}

	// argv mappings.
	cases := []struct {
		tool string
		args map[string]any
		want []string
	}{
		{"drive_rename", map[string]any{"file_id": "F1", "new_name": "new.txt"}, []string{"drive", "rename", "--", "F1", "new.txt"}},
		{"drive_move", map[string]any{"file_id": "F1", "parent": "P1"}, []string{"drive", "move", "--parent", "P1", "--", "F1"}},
		{"sheets_append", map[string]any{"spreadsheet_id": "S1", "range": "Sheet1!A:B", "values_json": `[["a","b"]]`, "input": "RAW", "insert": "INSERT_ROWS"}, []string{"sheets", "append", "--values-json", `[["a","b"]]`, "--input", "RAW", "--insert", "INSERT_ROWS", "--", "S1", "Sheet1!A:B"}},
		{"sheets_clear", map[string]any{"spreadsheet_id": "S1", "range": "Sheet1!A1:B2"}, []string{"sheets", "clear", "--", "S1", "Sheet1!A1:B2"}},
		{"docs_create", map[string]any{"title": "My Doc"}, []string{"docs", "create", "--", "My Doc"}},
		{"calendar_edit", map[string]any{"event_id": "E1", "summary": "New", "add_attendees": "x@y.com"}, []string{"calendar", "edit", "--summary", "New", "--add-attendee", "x@y.com", "--", "primary", "E1"}},
		{"gmail_drafts_create", map[string]any{"subject": "S", "body": "B", "to": "a@b.com", "attach": "/tmp/x.pdf"}, []string{"gmail", "drafts", "create", "--subject", "S", "--body", "B", "--to", "a@b.com", "--attach", "/tmp/x.pdf"}},
	}
	for _, tc := range cases {
		tool := findMCPTool(t, tc.tool)
		args, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: tc.args}})
		if err != nil {
			t.Fatalf("%s: %v", tc.tool, err)
		}
		if strings.Join(args, "\x00") != strings.Join(tc.want, "\x00") {
			t.Fatalf("%s args = %#v, want %#v", tc.tool, args, tc.want)
		}
	}

	// sheets_append rejects @file / stdin JSON expansion (same guard as update).
	if _, err := findMCPTool(t, "sheets_append").BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{"spreadsheet_id": "S1", "range": "A:B", "values_json": "@/etc/passwd"},
	}}); err == nil {
		t.Fatal("sheets_append should reject @file values_json")
	}
}

func TestMCPAttachmentToolsGatingAndArgs(t *testing.T) {
	// gmail_read_attachment: read-tier, on by default, no disk write (--out -).
	read := findMCPTool(t, "gmail_read_attachment")
	if read.Risk != mcpRiskRead {
		t.Fatalf("gmail_read_attachment should be read-risk, got %q", read.Risk)
	}
	if !hasMCPTool(mcpEnabledTools(McpCmd{}), "gmail_read_attachment") {
		t.Fatal("gmail_read_attachment should be enabled by default (read tier)")
	}
	args, err := read.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{"message_id": "M1", "attachment_id": "A1"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"gmail", "attachment", "--out", "-", "--", "M1", "A1"}
	if strings.Join(args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("read args = %#v, want %#v", args, want)
	}

	// gmail_get_attachment: write-tier (writes a file), requires --allow-write.
	if hasMCPTool(mcpEnabledTools(McpCmd{}), "gmail_get_attachment") {
		t.Fatal("gmail_get_attachment should require --allow-write")
	}
	if hasMCPTool(mcpEnabledTools(McpCmd{AllowSend: true}), "gmail_get_attachment") {
		t.Fatal("gmail_get_attachment should NOT be exposed by --allow-send alone")
	}
	if !hasMCPTool(mcpEnabledTools(McpCmd{AllowWrite: true}), "gmail_get_attachment") {
		t.Fatal("gmail_get_attachment should be enabled by --allow-write")
	}
	get := findMCPTool(t, "gmail_get_attachment")
	getArgs, err := get.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{"message_id": "M1", "attachment_id": "A1", "out": "/tmp/out", "name": "r.pdf"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	wantGet := []string{"gmail", "attachment", "--out", "/tmp/out", "--name", "r.pdf", "--", "M1", "A1"}
	if strings.Join(getArgs, "\x00") != strings.Join(wantGet, "\x00") {
		t.Fatalf("get args = %#v, want %#v", getArgs, wantGet)
	}

	// gmail_get_attachment rejects out=- (that is the read tool's job).
	if _, err := get.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{"message_id": "M1", "attachment_id": "A1", "out": "-"},
	}}); err == nil {
		t.Fatal("gmail_get_attachment should reject out=-")
	}
}

func TestMCPSafeReadToolsDefaultOnAndArgs(t *testing.T) {
	def := mcpEnabledTools(McpCmd{})
	for _, name := range []string{"drive_ls", "drive_tree", "calendar_freebusy"} {
		tool := findMCPTool(t, name)
		if tool.Risk != mcpRiskRead {
			t.Fatalf("%s should be read-risk, got %q", name, tool.Risk)
		}
		if !hasMCPTool(def, name) {
			t.Fatalf("%s should be enabled by default (read tier)", name)
		}
	}

	cases := []struct {
		tool string
		args map[string]any
		want []string
	}{
		{"drive_ls", map[string]any{"parent": "F1", "max": 25}, []string{"drive", "ls", "--max", "25", "--parent", "F1"}},
		{"drive_ls", map[string]any{"all": true, "query": "name contains 'x'"}, []string{"drive", "ls", "--max", "50", "--all", "--query", "name contains 'x'"}},
		{"drive_tree", map[string]any{"parent": "F1", "depth": 2}, []string{"drive", "tree", "--parent", "F1", "--depth", "2", "--max", "200"}},
		{"calendar_freebusy", map[string]any{"from": "today", "to": "today+7d", "calendars": "primary, work@x.com"}, []string{"calendar", "freebusy", "--from", "today", "--to", "today+7d", "--cal", "primary", "--cal", "work@x.com"}},
		{"calendar_freebusy", map[string]any{"from": "now", "to": "now+1d", "all": true}, []string{"calendar", "freebusy", "--from", "now", "--to", "now+1d", "--all"}},
	}
	for _, tc := range cases {
		tool := findMCPTool(t, tc.tool)
		args, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: tc.args}})
		if err != nil {
			t.Fatalf("%s: %v", tc.tool, err)
		}
		if strings.Join(args, "\x00") != strings.Join(tc.want, "\x00") {
			t.Fatalf("%s args = %#v, want %#v", tc.tool, args, tc.want)
		}
	}

	// mutual-exclusion guards.
	if _, err := findMCPTool(t, "drive_ls").BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{"all": true, "parent": "F1"},
	}}); err == nil {
		t.Fatal("drive_ls should reject all+parent")
	}
	if _, err := findMCPTool(t, "calendar_freebusy").BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{"from": "a", "to": "b", "all": true, "calendars": "primary"},
	}}); err == nil {
		t.Fatal("calendar_freebusy should reject all+calendars")
	}
}

func TestMCPListToolsUsesRuntimeStdout(t *testing.T) {
	var output bytes.Buffer
	err := (&McpCmd{
		ListTools:      true,
		TimeoutSeconds: 60,
		MaxOutputBytes: 1024,
	}).Run(newCmdRuntimeOutputContext(t, &output, io.Discard), &RootFlags{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := output.String(); !strings.Contains(got, `"tools"`) || !strings.Contains(got, `"gmail_search"`) {
		t.Fatalf("unexpected tool list: %s", got)
	}
}

func TestMCPParentArgsPreserveContextAndSafety(t *testing.T) {
	flags := &RootFlags{
		Home:                "/tmp/gog-home",
		Account:             "bot@example.com",
		Client:              "test-client",
		ResultsOnly:         true,
		Select:              "messages",
		DryRun:              true,
		GmailNoSend:         true,
		ReadOnly:            true,
		EnableCommands:      "gmail.search,docs.cat",
		EnableCommandsExact: "mcp,gmail.messages.search",
		DisableCommands:     "drive.delete",
	}
	base := strings.Join(mcpParentRootArgs(flags), "\x00")
	for _, want := range []string{"--json", "--wrap-untrusted", "--no-input", "--color=never", "--home\x00/tmp/gog-home", "--account\x00bot@example.com", "--client\x00test-client", "--results-only", "--select\x00messages", "--dry-run"} {
		if !strings.Contains(base, want) {
			t.Fatalf("base args missing %q in %#v", want, mcpParentRootArgs(flags))
		}
	}
	safety := strings.Join(mcpParentSafetyArgs(flags), "\x00")
	for _, want := range []string{"--gmail-no-send", "--readonly", "--enable-commands=gmail.search,docs.cat", "--enable-commands-exact=mcp,gmail.messages.search", "--disable-commands=drive.delete"} {
		if !strings.Contains(safety, want) {
			t.Fatalf("safety args missing %q in %#v", want, mcpParentSafetyArgs(flags))
		}
	}
}

func TestMCPToolBuildArgsTypedOnly(t *testing.T) {
	tool := findMCPTool(t, "sheets_update_range")
	args, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{
			"spreadsheet_id": "sheet1",
			"range":          "Sheet1!A1:B1",
			"values_json":    `[[1,2]]`,
			"input":          "RAW",
			"args":           []any{"drive", "delete", "file"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(args, " ")
	if strings.Contains(got, "drive delete") {
		t.Fatalf("generic args leaked into typed tool argv: %#v", args)
	}
	want := []string{"sheets", "update", "--values-json", "[[1,2]]", "--input", "RAW", "--", "sheet1", "Sheet1!A1:B1"}
	if strings.Join(args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}

func TestMCPServerValidatesToolInputSchema(t *testing.T) {
	s := newMCPServer()
	handlerCalls := 0
	s.AddTool(newMCPTool(findMCPTool(t, "docs_write")), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		handlerCalls++
		return mcp.NewToolResultText("ok"), nil
	})

	client, err := mcpclient.NewInProcessClient(s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("close client: %v", err)
		}
	})
	if err := client.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{Name: "gog-test", Version: "1"}
	if _, err := client.Initialize(t.Context(), initRequest); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		arguments map[string]any
		wantError bool
		wantText  string
	}{
		{
			name: "unknown field",
			arguments: map[string]any{
				"document_id": "doc1",
				"text":        "hello",
				"argv":        []any{"drive", "delete", "file"},
			},
			wantError: true,
			wantText:  "argv",
		},
		{
			name: "wrong type",
			arguments: map[string]any{
				"document_id": "doc1",
				"text":        "hello",
				"append":      "yes",
			},
			wantError: true,
			wantText:  "append",
		},
		{
			name: "missing required field",
			arguments: map[string]any{
				"text": "hello",
			},
			wantError: true,
			wantText:  "document_id",
		},
		{
			name: "valid",
			arguments: map[string]any{
				"document_id": "doc1",
				"text":        "hello",
				"append":      true,
			},
			wantText: "ok",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := handlerCalls
			result, err := client.CallTool(t.Context(), mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      "docs_write",
					Arguments: tt.arguments,
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.IsError != tt.wantError {
				t.Fatalf("IsError = %v, want %v: %#v", result.IsError, tt.wantError, result.Content)
			}
			if tt.wantError && handlerCalls != before {
				t.Fatal("invalid arguments reached the tool handler")
			}
			if !strings.Contains(mcpResultText(result), tt.wantText) {
				t.Fatalf("result = %#v, want text containing %q", result.Content, tt.wantText)
			}
		})
	}
	if handlerCalls != 1 {
		t.Fatalf("handler calls = %d, want 1", handlerCalls)
	}
}

func TestMCPDocsWritePreservesTextWhitespace(t *testing.T) {
	tool := findMCPTool(t, "docs_write")
	args, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{
			"document_id": "doc1",
			"text":        "  indented\n",
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for i, arg := range args {
		if arg == "--text" && i+1 < len(args) {
			if args[i+1] != "  indented\n" {
				t.Fatalf("text = %q", args[i+1])
			}
			return
		}
	}
	t.Fatalf("missing --text in %#v", args)
}

func TestMCPDocsWriteRejectsNeitherAppendNorReplace(t *testing.T) {
	tool := findMCPTool(t, "docs_write")
	_, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{
			"document_id": "doc1",
			"text":        "hello",
			"append":      false,
			"replace":     false,
		},
	}})
	if err == nil || !strings.Contains(err.Error(), "append=false") {
		t.Fatalf("expected append=false error, got %v", err)
	}
}

func TestMCPDocsGetRejectsTabWithAllTabs(t *testing.T) {
	tool := findMCPTool(t, "docs_get")
	_, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{
			"document_id": "doc1",
			"tab":         "Overview",
			"all_tabs":    true,
		},
	}})
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected tab/all_tabs error, got %v", err)
	}

	_, err = tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{
			"document_id": "doc1",
			"tab":         "",
		},
	}})
	if err == nil || !strings.Contains(err.Error(), "tab cannot be empty") {
		t.Fatalf("expected empty tab error, got %v", err)
	}
}

func TestMCPSheetsUpdateRejectsFileExpansion(t *testing.T) {
	tool := findMCPTool(t, "sheets_update_range")
	_, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{
			"spreadsheet_id": "sheet1",
			"range":          "Sheet1!A1",
			"values_json":    "@/tmp/secret.json",
		},
	}})
	if err == nil || !strings.Contains(err.Error(), "literal JSON") {
		t.Fatalf("expected literal JSON error, got %v", err)
	}
}

func TestMCPSheetsUpdatePreservesLargeJSONNumbers(t *testing.T) {
	tool := findMCPTool(t, "sheets_update_range")
	args, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{
			"spreadsheet_id": "sheet1",
			"range":          "Sheet1!A1",
			"values_json":    `[[1234567890123456789]]`,
			"input":          "RAW",
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for i, arg := range args {
		if arg == "--values-json" && i+1 < len(args) {
			if args[i+1] != `[[1234567890123456789]]` {
				t.Fatalf("values_json = %q", args[i+1])
			}
			return
		}
	}
	t.Fatalf("missing --values-json in %#v", args)
}

func TestMCPSheetsUpdateRejectsTrailingJSON(t *testing.T) {
	tool := findMCPTool(t, "sheets_update_range")
	_, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{
			"spreadsheet_id": "sheet1",
			"range":          "Sheet1!A1",
			"values_json":    `[[1]] garbage`,
		},
	}})
	if err == nil || !strings.Contains(err.Error(), "trailing content") {
		t.Fatalf("expected trailing content error, got %v", err)
	}
}

func TestMCPLimitedBufferCapsDuringWrite(t *testing.T) {
	buf := newMCPLimitedBuffer(5)
	n, err := buf.Write([]byte("hello world"))
	if err != nil {
		t.Fatal(err)
	}
	if n != len("hello world") {
		t.Fatalf("Write returned %d", n)
	}
	got := buf.String()
	if !strings.HasPrefix(got, "hello") || !strings.Contains(got, "truncated") {
		t.Fatalf("unexpected buffer: %q", got)
	}
}

func hasMCPTool(tools []mcpToolSpec, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func toolNames(tools []mcpToolSpec) []string {
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		out = append(out, tool.Name)
	}
	return out
}

func findMCPTool(t *testing.T, name string) mcpToolSpec {
	t.Helper()
	for _, tool := range mcpAllTools() {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("missing tool %s", name)
	return mcpToolSpec{}
}

func mcpResultText(result *mcp.CallToolResult) string {
	var text strings.Builder
	for _, content := range result.Content {
		if item, ok := content.(mcp.TextContent); ok {
			text.WriteString(item.Text)
		}
	}
	return text.String()
}
