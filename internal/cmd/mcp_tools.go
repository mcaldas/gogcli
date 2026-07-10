package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

func mcpAllTools() []mcpToolSpec {
	return []mcpToolSpec{
		mcpListCommandsTool(),
		mcpDescribeCommandTool(),
		mcpGmailSearchTool(),
		mcpGmailGetMessageTool(),
		mcpGmailGetThreadTool(),
		mcpDriveSearchTool(),
		mcpDriveGetTool(),
		mcpDocsGetTool(),
		mcpSheetsReadRangeTool(),
		mcpCalendarEventsTool(),
		mcpDocsWriteTool(),
		mcpSheetsUpdateRangeTool(),
		mcpGmailDraftsCreateTool(),
		mcpDriveDownloadTool(),
		mcpDriveMoveTool(),
		mcpDriveRenameTool(),
		mcpCalendarEditTool(),
		mcpDocsCreateTool(),
		mcpSheetsCreateTool(),
		mcpSheetsAppendTool(),
		mcpGmailSendTool(),
		mcpDriveUploadTool(),
		mcpDriveMkdirTool(),
		mcpCalendarCreateTool(),
		mcpSheetsClearTool(),
	}
}

// mcpAttachArgs turns a comma-separated attachment string into repeated
// --attach flags (trimming blanks). Shared by gmail_send and gmail_drafts_create.
func mcpAttachArgs(raw string) []string {
	var out []string
	for _, path := range strings.Split(raw, ",") {
		if path = strings.TrimSpace(path); path != "" {
			out = append(out, "--attach", path)
		}
	}
	return out
}

// mcpCommandPathArgs splits a space-separated command path into argv tokens,
// trimming blanks. Tokens are plain command words appended to a gog argv (no
// shell), and the commands they feed (--help, schema) execute nothing.
func mcpCommandPathArgs(raw string) []string {
	var out []string
	for _, tok := range strings.Fields(raw) {
		if tok = strings.TrimSpace(tok); tok != "" {
			out = append(out, tok)
		}
	}
	return out
}

func mcpListCommandsTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "gog_list_commands",
		Service:     "meta",
		Risk:        mcpRiskRead,
		Description: "Discover available gog commands. Returns the compact help listing (subcommands + one-line summaries) under a command path. Omit 'path' for the top-level groups (gmail, drive, calendar, docs, sheets, contacts, tasks, ...). Drill down by passing a deeper path (e.g. 'gmail drafts'), then call gog_describe for a specific command's full flags. Read-only: runs no Google API calls and executes nothing.",
		Options: []mcp.ToolOption{
			mcp.WithString("path", mcp.Description("Command path to list under, e.g. 'drive' or 'gmail drafts'. Omit for the top-level command groups.")),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			args := mcpCommandPathArgs(req.GetString("path", ""))
			return append(args, "--help"), nil
		},
	}
}

func mcpDescribeCommandTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "gog_describe",
		Service:     "meta",
		Risk:        mcpRiskRead,
		Description: "Get the machine-readable schema (subcommands, flags, arguments, types) for a specific gog command path, e.g. 'drive ls' or 'gmail send'. Use gog_list_commands first to find paths. Describe a specific command rather than a whole group to keep output small. Read-only: executes nothing.",
		Options: []mcp.ToolOption{
			mcp.WithString("command", mcp.Description("Command path to describe, e.g. 'drive ls' or 'calendar create'"), mcp.Required()),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			command, err := requireMCPString(req, "command")
			if err != nil {
				return nil, err
			}
			return append([]string{"schema"}, mcpCommandPathArgs(command)...), nil
		},
	}
}

func mcpGmailSearchTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "gmail_search",
		Service:     "gmail",
		Risk:        mcpRiskRead,
		Description: "Search Gmail messages with Gmail query syntax. Returns message summaries and optional sanitized bodies.",
		Options: []mcp.ToolOption{
			mcp.WithString("query", mcp.Description("Gmail search query, e.g. newer_than:7d from:person@example.com"), mcp.Required()),
			mcp.WithInteger("max", mcp.Description("Maximum results"), mcp.DefaultNumber(10), mcp.Min(1), mcp.Max(100)),
			mcp.WithBoolean("include_body", mcp.Description("Include decoded message body"), mcp.DefaultBool(false)),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			query, err := requireMCPString(req, "query")
			if err != nil {
				return nil, err
			}
			args := []string{"gmail", "messages", "search", "--max", strconv.Itoa(clampMCPInt(req.GetInt("max", 10), 1, 100))}
			if req.GetBool("include_body", false) {
				args = append(args, "--include-body")
			}
			return append(args, "--", query), nil
		},
	}
}

func mcpGmailGetMessageTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "gmail_get_message",
		Service:     "gmail",
		Risk:        mcpRiskRead,
		Description: "Get one Gmail message by ID. Sanitized content is enabled by default.",
		Options: []mcp.ToolOption{
			mcp.WithString("message_id", mcp.Description("Gmail message ID"), mcp.Required()),
			mcp.WithBoolean("sanitize_content", mcp.Description("Strip URLs/HTML and omit raw payloads"), mcp.DefaultBool(true)),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			messageID, err := requireMCPString(req, "message_id")
			if err != nil {
				return nil, err
			}
			args := []string{"gmail", "get"}
			if req.GetBool("sanitize_content", true) {
				args = append(args, "--sanitize-content")
			}
			return append(args, "--", messageID), nil
		},
	}
}

func mcpGmailGetThreadTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "gmail_get_thread",
		Service:     "gmail",
		Risk:        mcpRiskRead,
		Description: "Get one Gmail thread by ID. Sanitized content is enabled by default.",
		Options: []mcp.ToolOption{
			mcp.WithString("thread_id", mcp.Description("Gmail thread ID"), mcp.Required()),
			mcp.WithBoolean("sanitize_content", mcp.Description("Strip URLs/HTML and omit raw payloads"), mcp.DefaultBool(true)),
			mcp.WithBoolean("full", mcp.Description("Include full message bodies"), mcp.DefaultBool(false)),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			threadID, err := requireMCPString(req, "thread_id")
			if err != nil {
				return nil, err
			}
			args := []string{"gmail", "thread", "get"}
			if req.GetBool("sanitize_content", true) {
				args = append(args, "--sanitize-content")
			}
			if req.GetBool("full", false) {
				args = append(args, "--full")
			}
			return append(args, "--", threadID), nil
		},
	}
}

func mcpDriveSearchTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "drive_search",
		Service:     "drive",
		Risk:        mcpRiskRead,
		Description: "Search Google Drive files using text search or Drive query language.",
		Options: []mcp.ToolOption{
			mcp.WithString("query", mcp.Description("Search text or Drive query"), mcp.Required()),
			mcp.WithInteger("max", mcp.Description("Maximum results"), mcp.DefaultNumber(20), mcp.Min(1), mcp.Max(100)),
			mcp.WithBoolean("raw_query", mcp.Description("Treat query as Drive query language"), mcp.DefaultBool(false)),
			mcp.WithString("parent", mcp.Description("Optional parent folder/shared drive ID")),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			query, err := requireMCPString(req, "query")
			if err != nil {
				return nil, err
			}
			args := []string{"drive", "search", "--max", strconv.Itoa(clampMCPInt(req.GetInt("max", 20), 1, 100))}
			if req.GetBool("raw_query", false) {
				args = append(args, "--raw-query")
			}
			if parent := strings.TrimSpace(req.GetString("parent", "")); parent != "" {
				args = append(args, "--parent", parent)
			}
			return append(args, "--", query), nil
		},
	}
}

func mcpDriveGetTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "drive_get",
		Service:     "drive",
		Risk:        mcpRiskRead,
		Description: "Get Google Drive file metadata by ID.",
		Options: []mcp.ToolOption{
			mcp.WithString("file_id", mcp.Description("Drive file ID"), mcp.Required()),
			mcp.WithString("fields", mcp.Description("Optional Drive API field mask")),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			fileID, err := requireMCPString(req, "file_id")
			if err != nil {
				return nil, err
			}
			args := []string{"drive", "get"}
			if fields := strings.TrimSpace(req.GetString("fields", "")); fields != "" {
				args = append(args, "--fields", fields)
			}
			return append(args, "--", fileID), nil
		},
	}
}

func mcpDocsGetTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "docs_get",
		Service:     "docs",
		Risk:        mcpRiskRead,
		Description: "Read a Google Doc as wrapped text, all tabs, or one tab.",
		Options: []mcp.ToolOption{
			mcp.WithString("document_id", mcp.Description("Google Docs document ID"), mcp.Required()),
			mcp.WithString("tab", mcp.Description("Optional tab title or ID")),
			mcp.WithBoolean("all_tabs", mcp.Description("Read all tabs"), mcp.DefaultBool(false)),
			mcp.WithInteger("max_bytes", mcp.Description("Maximum text bytes, 0 for unlimited"), mcp.DefaultNumber(2000000), mcp.Min(0)),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			docID, err := requireMCPString(req, "document_id")
			if err != nil {
				return nil, err
			}
			args := []string{"docs", "cat", "--max-bytes", strconv.Itoa(clampMCPInt(req.GetInt("max_bytes", 2000000), 0, 20_000_000))}
			tab := strings.TrimSpace(req.GetString("tab", ""))
			_, tabProvided := req.GetArguments()["tab"]
			if tabProvided && tab == "" {
				return nil, fmt.Errorf("tab cannot be empty")
			}
			allTabs := req.GetBool("all_tabs", false)
			if tab != "" && allTabs {
				return nil, fmt.Errorf("tab and all_tabs are mutually exclusive")
			}
			if tab != "" {
				args = append(args, "--tab", tab)
			}
			if allTabs {
				args = append(args, "--all-tabs")
			}
			return append(args, "--", docID), nil
		},
	}
}

func mcpSheetsReadRangeTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "sheets_read_range",
		Service:     "sheets",
		Risk:        mcpRiskRead,
		Description: "Read values from a Google Sheets range.",
		Options: []mcp.ToolOption{
			mcp.WithString("spreadsheet_id", mcp.Description("Google Sheets spreadsheet ID"), mcp.Required()),
			mcp.WithString("range", mcp.Description("A1 notation or named range"), mcp.Required()),
			mcp.WithString("render", mcp.Description("Value render option"), mcp.Enum("FORMATTED_VALUE", "UNFORMATTED_VALUE", "FORMULA")),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			spreadsheetID, err := requireMCPString(req, "spreadsheet_id")
			if err != nil {
				return nil, err
			}
			rangeSpec, err := requireMCPString(req, "range")
			if err != nil {
				return nil, err
			}
			args := []string{"sheets", "get"}
			if render := strings.TrimSpace(req.GetString("render", "")); render != "" {
				args = append(args, "--render", render)
			}
			return append(args, "--", spreadsheetID, rangeSpec), nil
		},
	}
}

func mcpCalendarEventsTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "calendar_events",
		Service:     "calendar",
		Risk:        mcpRiskRead,
		Description: "List Google Calendar events from primary or selected calendars.",
		Options: []mcp.ToolOption{
			mcp.WithString("calendar_id", mcp.Description("Calendar ID or selector; default primary")),
			mcp.WithString("from", mcp.Description("Start time: RFC3339, date, or relative value")),
			mcp.WithString("to", mcp.Description("End time: RFC3339, date, or relative value")),
			mcp.WithBoolean("today", mcp.Description("Today only"), mcp.DefaultBool(false)),
			mcp.WithBoolean("tomorrow", mcp.Description("Tomorrow only"), mcp.DefaultBool(false)),
			mcp.WithInteger("days", mcp.Description("Next N days"), mcp.DefaultNumber(0), mcp.Min(0), mcp.Max(31)),
			mcp.WithInteger("max", mcp.Description("Maximum results"), mcp.DefaultNumber(10), mcp.Min(1), mcp.Max(250)),
			mcp.WithString("query", mcp.Description("Free text search")),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			args := []string{"calendar", "events"}
			calendarID := strings.TrimSpace(req.GetString("calendar_id", ""))
			for _, pair := range [][2]string{{"from", "--from"}, {"to", "--to"}, {"query", "--query"}} {
				if v := strings.TrimSpace(req.GetString(pair[0], "")); v != "" {
					args = append(args, pair[1], v)
				}
			}
			if req.GetBool("today", false) {
				args = append(args, "--today")
			}
			if req.GetBool("tomorrow", false) {
				args = append(args, "--tomorrow")
			}
			if days := req.GetInt("days", 0); days > 0 {
				args = append(args, "--days", strconv.Itoa(clampMCPInt(days, 1, 31)))
			}
			args = append(args, "--max", strconv.Itoa(clampMCPInt(req.GetInt("max", 10), 1, 250)))
			if calendarID != "" {
				args = append(args, "--", calendarID)
			}
			return args, nil
		},
	}
}

func mcpDocsWriteTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "docs_write",
		Service:     "docs",
		Risk:        mcpRiskWrite,
		Description: "Write text to a Google Doc. Requires --allow-write on the MCP server.",
		Options: []mcp.ToolOption{
			mcp.WithString("document_id", mcp.Description("Google Docs document ID"), mcp.Required()),
			mcp.WithString("text", mcp.Description("Text or markdown to write"), mcp.Required()),
			mcp.WithString("tab", mcp.Description("Optional tab title or ID")),
			mcp.WithBoolean("append", mcp.Description("Append instead of replacing"), mcp.DefaultBool(true)),
			mcp.WithBoolean("replace", mcp.Description("Replace all existing content"), mcp.DefaultBool(false)),
			mcp.WithBoolean("markdown", mcp.Description("Convert markdown to Docs formatting"), mcp.DefaultBool(false)),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			docID, err := requireMCPString(req, "document_id")
			if err != nil {
				return nil, err
			}
			text, err := requireMCPText(req, "text")
			if err != nil {
				return nil, err
			}
			args := []string{"docs", "write", "--text", text}
			reqArgs := req.GetArguments()
			replace := req.GetBool("replace", false)
			appendProvided := false
			if reqArgs != nil {
				_, appendProvided = reqArgs["append"]
			}
			appendMode := req.GetBool("append", true)
			if replace && appendProvided && appendMode {
				return nil, fmt.Errorf("append and replace are mutually exclusive")
			}
			switch {
			case replace:
				args = append(args, "--replace")
			case appendMode:
				args = append(args, "--append")
			default:
				return nil, fmt.Errorf("append=false requires replace=true to avoid implicit document replacement")
			}
			if req.GetBool("markdown", false) {
				args = append(args, "--markdown")
			}
			if tab := strings.TrimSpace(req.GetString("tab", "")); tab != "" {
				args = append(args, "--tab", tab)
			}
			return append(args, "--", docID), nil
		},
	}
}

func mcpSheetsUpdateRangeTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "sheets_update_range",
		Service:     "sheets",
		Risk:        mcpRiskWrite,
		Description: "Update values in a Google Sheets range. Requires --allow-write on the MCP server.",
		Options: []mcp.ToolOption{
			mcp.WithString("spreadsheet_id", mcp.Description("Google Sheets spreadsheet ID"), mcp.Required()),
			mcp.WithString("range", mcp.Description("A1 notation or named range"), mcp.Required()),
			mcp.WithString("values_json", mcp.Description("JSON 2D array of values"), mcp.Required()),
			mcp.WithString("input", mcp.Description("Value input option"), mcp.Enum("RAW", "USER_ENTERED"), mcp.DefaultString("USER_ENTERED")),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			spreadsheetID, err := requireMCPString(req, "spreadsheet_id")
			if err != nil {
				return nil, err
			}
			rangeSpec, err := requireMCPString(req, "range")
			if err != nil {
				return nil, err
			}
			valuesJSON, err := requireMCPLiteralValuesJSON(req, "values_json")
			if err != nil {
				return nil, err
			}
			input := strings.TrimSpace(req.GetString("input", "USER_ENTERED"))
			if input == "" {
				input = "USER_ENTERED"
			}
			return []string{"sheets", "update", "--values-json", valuesJSON, "--input", input, "--", spreadsheetID, rangeSpec}, nil
		},
	}
}

func mcpGmailSendTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "gmail_send",
		Service:     "gmail",
		Risk:        mcpRiskSend,
		Description: "Send an email from the active account. Requires --allow-send on the MCP server. This action is irreversible and leaves your account.",
		Options: []mcp.ToolOption{
			mcp.WithString("to", mcp.Description("Recipients (comma-separated)"), mcp.Required()),
			mcp.WithString("subject", mcp.Description("Subject line"), mcp.Required()),
			mcp.WithString("body", mcp.Description("Plain-text body. Provide this or body_html (or both)."), mcp.DefaultString("")),
			mcp.WithString("body_html", mcp.Description("HTML body (optional)"), mcp.DefaultString("")),
			mcp.WithString("cc", mcp.Description("CC recipients (comma-separated)")),
			mcp.WithString("bcc", mcp.Description("BCC recipients (comma-separated)")),
			mcp.WithString("from", mcp.Description("Send-as address (must be a configured alias of the account)")),
			mcp.WithString("attach", mcp.Description("Attachment local file path(s), comma-separated. Files are read from the machine running the server.")),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			to, err := requireMCPString(req, "to")
			if err != nil {
				return nil, err
			}
			subject, err := requireMCPString(req, "subject")
			if err != nil {
				return nil, err
			}
			body := strings.TrimSpace(req.GetString("body", ""))
			bodyHTML := strings.TrimSpace(req.GetString("body_html", ""))
			if body == "" && bodyHTML == "" {
				return nil, fmt.Errorf("provide body or body_html")
			}
			args := []string{"gmail", "send", "--to", to, "--subject", subject}
			if body != "" {
				args = append(args, "--body", body)
			}
			if bodyHTML != "" {
				args = append(args, "--body-html", bodyHTML)
			}
			if cc := strings.TrimSpace(req.GetString("cc", "")); cc != "" {
				args = append(args, "--cc", cc)
			}
			if bcc := strings.TrimSpace(req.GetString("bcc", "")); bcc != "" {
				args = append(args, "--bcc", bcc)
			}
			if from := strings.TrimSpace(req.GetString("from", "")); from != "" {
				args = append(args, "--from", from)
			}
			args = append(args, mcpAttachArgs(req.GetString("attach", ""))...)
			return args, nil
		},
	}
}

func mcpGmailDraftsCreateTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "gmail_drafts_create",
		Service:     "gmail",
		Risk:        mcpRiskWrite,
		Description: "Create a Gmail draft (does NOT send). Requires --allow-write on the MCP server.",
		Options: []mcp.ToolOption{
			mcp.WithString("subject", mcp.Description("Subject line"), mcp.Required()),
			mcp.WithString("body", mcp.Description("Plain-text body. Provide this or body_html (or both)."), mcp.DefaultString("")),
			mcp.WithString("body_html", mcp.Description("HTML body (optional)"), mcp.DefaultString("")),
			mcp.WithString("to", mcp.Description("Recipients (comma-separated; optional for a draft)")),
			mcp.WithString("cc", mcp.Description("CC recipients (comma-separated)")),
			mcp.WithString("bcc", mcp.Description("BCC recipients (comma-separated)")),
			mcp.WithString("from", mcp.Description("Send-as address (must be a configured alias of the account)")),
			mcp.WithString("attach", mcp.Description("Attachment local file path(s), comma-separated. Files are read from the machine running the server.")),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			subject, err := requireMCPString(req, "subject")
			if err != nil {
				return nil, err
			}
			body := strings.TrimSpace(req.GetString("body", ""))
			bodyHTML := strings.TrimSpace(req.GetString("body_html", ""))
			if body == "" && bodyHTML == "" {
				return nil, fmt.Errorf("provide body or body_html")
			}
			args := []string{"gmail", "drafts", "create", "--subject", subject}
			if body != "" {
				args = append(args, "--body", body)
			}
			if bodyHTML != "" {
				args = append(args, "--body-html", bodyHTML)
			}
			for _, pair := range [][2]string{{"to", "--to"}, {"cc", "--cc"}, {"bcc", "--bcc"}, {"from", "--from"}} {
				if v := strings.TrimSpace(req.GetString(pair[0], "")); v != "" {
					args = append(args, pair[1], v)
				}
			}
			args = append(args, mcpAttachArgs(req.GetString("attach", ""))...)
			return args, nil
		},
	}
}

func mcpDriveDownloadTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "drive_download",
		Service:     "drive",
		Risk:        mcpRiskWrite,
		Description: "Download a Drive file to a local path. Requires --allow-write on the MCP server. Writes to the machine running the server.",
		Options: []mcp.ToolOption{
			mcp.WithString("file_id", mcp.Description("Drive file ID"), mcp.Required()),
			mcp.WithString("out", mcp.Description("Output local file path (default: gogcli config dir)")),
			mcp.WithString("format", mcp.Description("Export format for Google-native files, e.g. pdf, docx, xlsx, csv")),
			mcp.WithBoolean("overwrite", mcp.Description("Overwrite an existing output file"), mcp.DefaultBool(false)),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			fileID, err := requireMCPString(req, "file_id")
			if err != nil {
				return nil, err
			}
			args := []string{"drive", "download"}
			if out := strings.TrimSpace(req.GetString("out", "")); out != "" {
				args = append(args, "--out", out)
			}
			if format := strings.TrimSpace(req.GetString("format", "")); format != "" {
				args = append(args, "--format", format)
			}
			if req.GetBool("overwrite", false) {
				args = append(args, "--overwrite")
			}
			return append(args, "--", fileID), nil
		},
	}
}

func mcpDriveMoveTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "drive_move",
		Service:     "drive",
		Risk:        mcpRiskWrite,
		Description: "Move a Drive file to a different folder. Requires --allow-write on the MCP server.",
		Options: []mcp.ToolOption{
			mcp.WithString("file_id", mcp.Description("Drive file ID"), mcp.Required()),
			mcp.WithString("parent", mcp.Description("New parent folder ID"), mcp.Required()),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			fileID, err := requireMCPString(req, "file_id")
			if err != nil {
				return nil, err
			}
			parent, err := requireMCPString(req, "parent")
			if err != nil {
				return nil, err
			}
			return []string{"drive", "move", "--parent", parent, "--", fileID}, nil
		},
	}
}

func mcpDriveRenameTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "drive_rename",
		Service:     "drive",
		Risk:        mcpRiskWrite,
		Description: "Rename a Drive file. Requires --allow-write on the MCP server.",
		Options: []mcp.ToolOption{
			mcp.WithString("file_id", mcp.Description("Drive file ID"), mcp.Required()),
			mcp.WithString("new_name", mcp.Description("New file name"), mcp.Required()),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			fileID, err := requireMCPString(req, "file_id")
			if err != nil {
				return nil, err
			}
			newName, err := requireMCPString(req, "new_name")
			if err != nil {
				return nil, err
			}
			return []string{"drive", "rename", "--", fileID, newName}, nil
		},
	}
}

func mcpCalendarEditTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "calendar_edit",
		Service:     "calendar",
		Risk:        mcpRiskWrite,
		Description: "Edit an existing Calendar event. Requires --allow-write on the MCP server. If the event has attendees, Google may notify them of changes.",
		Options: []mcp.ToolOption{
			mcp.WithString("event_id", mcp.Description("Event ID"), mcp.Required()),
			mcp.WithString("calendar_id", mcp.Description("Calendar ID; default primary"), mcp.DefaultString("primary")),
			mcp.WithString("summary", mcp.Description("New title/summary")),
			mcp.WithString("from", mcp.Description("New start: RFC3339 or date")),
			mcp.WithString("to", mcp.Description("New end: RFC3339 or date")),
			mcp.WithString("description", mcp.Description("New description")),
			mcp.WithString("location", mcp.Description("New location")),
			mcp.WithString("add_attendees", mcp.Description("Comma-separated attendee emails to ADD (does not remove existing)")),
			mcp.WithBoolean("all_day", mcp.Description("Make it an all-day event (use date-only from/to)"), mcp.DefaultBool(false)),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			eventID, err := requireMCPString(req, "event_id")
			if err != nil {
				return nil, err
			}
			calendarID := strings.TrimSpace(req.GetString("calendar_id", "primary"))
			if calendarID == "" {
				calendarID = "primary"
			}
			args := []string{"calendar", "edit"}
			for _, pair := range [][2]string{{"summary", "--summary"}, {"from", "--from"}, {"to", "--to"}, {"description", "--description"}, {"location", "--location"}, {"add_attendees", "--add-attendee"}} {
				if v := strings.TrimSpace(req.GetString(pair[0], "")); v != "" {
					args = append(args, pair[1], v)
				}
			}
			if req.GetBool("all_day", false) {
				args = append(args, "--all-day")
			}
			return append(args, "--", calendarID, eventID), nil
		},
	}
}

func mcpDocsCreateTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "docs_create",
		Service:     "docs",
		Risk:        mcpRiskWrite,
		Description: "Create a new Google Doc. Requires --allow-write on the MCP server.",
		Options: []mcp.ToolOption{
			mcp.WithString("title", mcp.Description("Document title"), mcp.Required()),
			mcp.WithString("parent", mcp.Description("Destination folder ID")),
			mcp.WithString("markdown_file", mcp.Description("Optional local Markdown file to import as initial content")),
			mcp.WithBoolean("pageless", mcp.Description("Create in pageless mode"), mcp.DefaultBool(false)),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			title, err := requireMCPString(req, "title")
			if err != nil {
				return nil, err
			}
			args := []string{"docs", "create"}
			if parent := strings.TrimSpace(req.GetString("parent", "")); parent != "" {
				args = append(args, "--parent", parent)
			}
			if file := strings.TrimSpace(req.GetString("markdown_file", "")); file != "" {
				args = append(args, "--file", file)
			}
			if req.GetBool("pageless", false) {
				args = append(args, "--pageless")
			}
			return append(args, "--", title), nil
		},
	}
}

func mcpSheetsCreateTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "sheets_create",
		Service:     "sheets",
		Risk:        mcpRiskWrite,
		Description: "Create a new Google Sheets spreadsheet. Requires --allow-write on the MCP server.",
		Options: []mcp.ToolOption{
			mcp.WithString("title", mcp.Description("Spreadsheet title"), mcp.Required()),
			mcp.WithString("sheets", mcp.Description("Comma-separated tab names to create")),
			mcp.WithString("parent", mcp.Description("Destination folder ID")),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			title, err := requireMCPString(req, "title")
			if err != nil {
				return nil, err
			}
			args := []string{"sheets", "create"}
			if sheets := strings.TrimSpace(req.GetString("sheets", "")); sheets != "" {
				args = append(args, "--sheets", sheets)
			}
			if parent := strings.TrimSpace(req.GetString("parent", "")); parent != "" {
				args = append(args, "--parent", parent)
			}
			return append(args, "--", title), nil
		},
	}
}

func mcpSheetsAppendTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "sheets_append",
		Service:     "sheets",
		Risk:        mcpRiskWrite,
		Description: "Append rows to a Google Sheets range (additive; does not overwrite existing rows by default). Requires --allow-write on the MCP server.",
		Options: []mcp.ToolOption{
			mcp.WithString("spreadsheet_id", mcp.Description("Google Sheets spreadsheet ID"), mcp.Required()),
			mcp.WithString("range", mcp.Description("A1 notation or named range, e.g. Sheet1!A:C"), mcp.Required()),
			mcp.WithString("values_json", mcp.Description("JSON 2D array of values"), mcp.Required()),
			mcp.WithString("input", mcp.Description("Value input option"), mcp.Enum("RAW", "USER_ENTERED"), mcp.DefaultString("USER_ENTERED")),
			mcp.WithString("insert", mcp.Description("Insert data option"), mcp.Enum("OVERWRITE", "INSERT_ROWS")),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			spreadsheetID, err := requireMCPString(req, "spreadsheet_id")
			if err != nil {
				return nil, err
			}
			rangeSpec, err := requireMCPString(req, "range")
			if err != nil {
				return nil, err
			}
			valuesJSON, err := requireMCPLiteralValuesJSON(req, "values_json")
			if err != nil {
				return nil, err
			}
			input := strings.TrimSpace(req.GetString("input", "USER_ENTERED"))
			if input == "" {
				input = "USER_ENTERED"
			}
			args := []string{"sheets", "append", "--values-json", valuesJSON, "--input", input}
			if insert := strings.TrimSpace(req.GetString("insert", "")); insert != "" {
				args = append(args, "--insert", insert)
			}
			return append(args, "--", spreadsheetID, rangeSpec), nil
		},
	}
}

func mcpDriveUploadTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "drive_upload",
		Service:     "drive",
		Risk:        mcpRiskSend,
		Description: "Upload a local file to Google Drive. Requires --allow-send on the MCP server.",
		Options: []mcp.ToolOption{
			mcp.WithString("local_path", mcp.Description("Path to the local file to upload"), mcp.Required()),
			mcp.WithString("name", mcp.Description("Override the uploaded file name")),
			mcp.WithString("parent", mcp.Description("Destination folder ID")),
			mcp.WithString("mime_type", mcp.Description("Override MIME type inference")),
			mcp.WithBoolean("convert", mcp.Description("Auto-convert to native Google format"), mcp.DefaultBool(false)),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			localPath, err := requireMCPString(req, "local_path")
			if err != nil {
				return nil, err
			}
			args := []string{"drive", "upload"}
			if name := strings.TrimSpace(req.GetString("name", "")); name != "" {
				args = append(args, "--name", name)
			}
			if parent := strings.TrimSpace(req.GetString("parent", "")); parent != "" {
				args = append(args, "--parent", parent)
			}
			if mimeType := strings.TrimSpace(req.GetString("mime_type", "")); mimeType != "" {
				args = append(args, "--mime-type", mimeType)
			}
			if req.GetBool("convert", false) {
				args = append(args, "--convert")
			}
			return append(args, "--", localPath), nil
		},
	}
}

func mcpDriveMkdirTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "drive_mkdir",
		Service:     "drive",
		Risk:        mcpRiskSend,
		Description: "Create a folder in Google Drive. Requires --allow-send on the MCP server.",
		Options: []mcp.ToolOption{
			mcp.WithString("name", mcp.Description("Folder name"), mcp.Required()),
			mcp.WithString("parent", mcp.Description("Parent folder ID")),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			name, err := requireMCPString(req, "name")
			if err != nil {
				return nil, err
			}
			args := []string{"drive", "mkdir"}
			if parent := strings.TrimSpace(req.GetString("parent", "")); parent != "" {
				args = append(args, "--parent", parent)
			}
			return append(args, "--", name), nil
		},
	}
}

func mcpCalendarCreateTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "calendar_create",
		Service:     "calendar",
		Risk:        mcpRiskSend,
		Description: "Create a Google Calendar event. Requires --allow-send on the MCP server. Inviting attendees notifies them.",
		Options: []mcp.ToolOption{
			mcp.WithString("summary", mcp.Description("Event title/summary"), mcp.Required()),
			mcp.WithString("from", mcp.Description("Start: RFC3339 timestamp, or date (YYYY-MM-DD) for all-day"), mcp.Required()),
			mcp.WithString("to", mcp.Description("End: RFC3339 timestamp, or date (YYYY-MM-DD) for all-day"), mcp.Required()),
			mcp.WithString("calendar_id", mcp.Description("Target calendar ID; default primary"), mcp.DefaultString("primary")),
			mcp.WithString("description", mcp.Description("Event description")),
			mcp.WithString("location", mcp.Description("Event location")),
			mcp.WithString("attendees", mcp.Description("Comma-separated attendee emails")),
			mcp.WithString("timezone", mcp.Description("IANA timezone for start/end (e.g. Asia/Jerusalem)")),
			mcp.WithBoolean("all_day", mcp.Description("All-day event (use date-only from/to)"), mcp.DefaultBool(false)),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			summary, err := requireMCPString(req, "summary")
			if err != nil {
				return nil, err
			}
			from, err := requireMCPString(req, "from")
			if err != nil {
				return nil, err
			}
			to, err := requireMCPString(req, "to")
			if err != nil {
				return nil, err
			}
			calendarID := strings.TrimSpace(req.GetString("calendar_id", "primary"))
			if calendarID == "" {
				calendarID = "primary"
			}
			args := []string{"calendar", "create", "--summary", summary, "--from", from, "--to", to}
			if desc := strings.TrimSpace(req.GetString("description", "")); desc != "" {
				args = append(args, "--description", desc)
			}
			if location := strings.TrimSpace(req.GetString("location", "")); location != "" {
				args = append(args, "--location", location)
			}
			if attendees := strings.TrimSpace(req.GetString("attendees", "")); attendees != "" {
				args = append(args, "--attendees", attendees)
			}
			if tz := strings.TrimSpace(req.GetString("timezone", "")); tz != "" {
				args = append(args, "--timezone", tz)
			}
			if req.GetBool("all_day", false) {
				args = append(args, "--all-day")
			}
			return append(args, "--", calendarID), nil
		},
	}
}

func mcpSheetsClearTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "sheets_clear",
		Service:     "sheets",
		Risk:        mcpRiskSend,
		Description: "Clear values from a Google Sheets range. DESTRUCTIVE (data loss). Requires --allow-send on the MCP server.",
		Options: []mcp.ToolOption{
			mcp.WithString("spreadsheet_id", mcp.Description("Google Sheets spreadsheet ID"), mcp.Required()),
			mcp.WithString("range", mcp.Description("A1 notation or named range to clear, e.g. Sheet1!A1:B2"), mcp.Required()),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			spreadsheetID, err := requireMCPString(req, "spreadsheet_id")
			if err != nil {
				return nil, err
			}
			rangeSpec, err := requireMCPString(req, "range")
			if err != nil {
				return nil, err
			}
			return []string{"sheets", "clear", "--", spreadsheetID, rangeSpec}, nil
		},
	}
}

func requireMCPText(req mcp.CallToolRequest, key string) (string, error) {
	value, err := req.RequireString(key)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("empty %s", key)
	}
	return value, nil
}

func requireMCPLiteralValuesJSON(req mcp.CallToolRequest, key string) (string, error) {
	value, err := requireMCPText(req, key)
	if err != nil {
		return "", err
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "-" || strings.HasPrefix(trimmed, "@") {
		return "", fmt.Errorf("%s must be literal JSON, not stdin or @file input", key)
	}
	var rows [][]any
	dec := json.NewDecoder(bytes.NewReader([]byte(trimmed)))
	dec.UseNumber()
	if unmarshalErr := dec.Decode(&rows); unmarshalErr != nil {
		return "", fmt.Errorf("invalid %s JSON 2D array: %w", key, unmarshalErr)
	}
	var extra any
	if extraErr := dec.Decode(&extra); extraErr != io.EOF {
		return "", fmt.Errorf("invalid %s JSON 2D array: trailing content", key)
	}
	canonical, err := json.Marshal(rows)
	if err != nil {
		return "", fmt.Errorf("canonicalize %s: %w", key, err)
	}
	return string(canonical), nil
}
