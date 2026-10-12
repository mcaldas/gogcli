package cmd

// Fork-only MCP tools (openclaw/gogcli fork, mcaldas).
//
// Kept in their own file so upstream's mcp_tools.go and mcp_gmail.go stay
// byte-identical to origin/main and future rebases touch one line there.
// Upstream owns the Gmail mailbox surface; this file owns Drive, Calendar,
// Docs and Sheets, plus two read-only discovery tools and two Gmail attachment
// tools upstream has no equivalent for.
//
// Tiering follows upstream's two axes: Risk gates on --allow-write, and
// Capability gates on a named flag. The four tools carrying mcpCapabilityCreate
// are the ones --allow-write must not reach on its own: uploading local file
// content, creating a folder, creating a calendar event (which emails
// invitations to its attendees), and clearing a range (not recoverable).
//
// Note what the capability does NOT cover: docs_create and sheets_create are
// plain write, as they were before this fork, so --allow-write can still create
// a Doc or Sheet in a named folder. drive_download and gmail_get_attachment are
// also plain write and take a caller-chosen local path, so --allow-write can
// write files on the host running the server.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// mcpForkTools returns every tool this fork adds on top of upstream's set.
func mcpForkTools() []mcpToolSpec {
	return []mcpToolSpec{
		mcpListCommandsTool(),
		mcpDescribeCommandTool(),
		mcpGmailReadAttachmentTool(),
		mcpDriveLsTool(),
		mcpDriveTreeTool(),
		mcpCalendarFreeBusyTool(),
		mcpDriveDownloadTool(),
		mcpGmailGetAttachmentTool(),
		mcpDriveMoveTool(),
		mcpDriveRenameTool(),
		mcpCalendarEditTool(),
		mcpDocsCreateTool(),
		mcpSheetsCreateTool(),
		mcpSheetsAppendTool(),
		mcpDriveUploadTool(),
		mcpDriveMkdirTool(),
		mcpCalendarCreateTool(),
		mcpSheetsClearTool(),
		mcpGmailDraftWithAttachmentsTool(),
	}
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

// mcpFlagArg maps a tool request field to the CLI flag it populates.
type mcpFlagArg struct{ field, flag string }

// mcpPositionalArgs builds a child command from optional string and boolean
// flags followed by one required trailing positional. Most typed tools share
// this shape; expressing it declaratively keeps them from drifting apart.
func mcpPositionalArgs(
	command []string,
	positional string,
	stringFlags []mcpFlagArg,
	boolFlags []mcpFlagArg,
) func(mcp.CallToolRequest) ([]string, error) {
	return func(req mcp.CallToolRequest) ([]string, error) {
		value, err := requireMCPString(req, positional)
		if err != nil {
			return nil, err
		}
		args := append([]string(nil), command...)
		for _, f := range stringFlags {
			if v := strings.TrimSpace(req.GetString(f.field, "")); v != "" {
				args = append(args, f.flag, v)
			}
		}
		for _, f := range boolFlags {
			if req.GetBool(f.field, false) {
				args = append(args, f.flag)
			}
		}
		return append(args, "--", value), nil
	}
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

func mcpGmailReadAttachmentTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "gmail_read_attachment",
		Service:     "gmail",
		Risk:        mcpRiskRead,
		Description: "Read a Gmail attachment's content inline as base64 (no file is written). Get message_id and attachment_id from gmail_get_message (format=full exposes attachment IDs in the payload parts). Best for small attachments; content above the inline size limit is withheld with a reason instead of returned — use gmail_get_attachment to save those to a file instead.",
		Options: []mcp.ToolOption{
			mcp.WithString("message_id", mcp.Description("Gmail message ID"), mcp.Required()),
			mcp.WithString("attachment_id", mcp.Description("Attachment ID from the message payload parts"), mcp.Required()),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			messageID, err := requireMCPString(req, "message_id")
			if err != nil {
				return nil, err
			}
			attachmentID, err := requireMCPString(req, "attachment_id")
			if err != nil {
				return nil, err
			}
			return []string{"gmail", "attachment", "--out", "-", "--", messageID, attachmentID}, nil
		},
	}
}

func mcpDriveLsTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "drive_ls",
		Service:     "drive",
		Risk:        mcpRiskRead,
		Description: "List files in a Drive folder (defaults to root). Use 'parent' for a specific folder, or all=true to list all accessible files (mutually exclusive with parent).",
		Options: []mcp.ToolOption{
			mcp.WithString("parent", mcp.Description("Folder ID to list; default root")),
			mcp.WithString("query", mcp.Description("Optional Drive query filter")),
			mcp.WithBoolean("all", mcp.Description("List all accessible files instead of one folder"), mcp.DefaultBool(false)),
			mcp.WithInteger("max", mcp.Description("Maximum results"), mcp.DefaultNumber(50), mcp.Min(1), mcp.Max(1000)),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			parent := strings.TrimSpace(req.GetString("parent", ""))
			all := req.GetBool("all", false)
			if all && parent != "" {
				return nil, fmt.Errorf("all and parent are mutually exclusive")
			}
			args := []string{"drive", "ls", "--max", strconv.Itoa(clampMCPInt(req.GetInt("max", 50), 1, 1000))}
			if all {
				args = append(args, "--all")
			}
			if parent != "" {
				args = append(args, "--parent", parent)
			}
			if query := strings.TrimSpace(req.GetString("query", "")); query != "" {
				args = append(args, "--query", query)
			}
			return args, nil
		},
	}
}

func mcpDriveTreeTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "drive_tree",
		Service:     "drive",
		Risk:        mcpRiskRead,
		Description: "Print a read-only Drive folder tree starting at a folder (defaults to root).",
		Options: []mcp.ToolOption{
			mcp.WithString("parent", mcp.Description("Folder ID to start from; default root")),
			mcp.WithInteger("depth", mcp.Description("Max depth (0 = unlimited)"), mcp.DefaultNumber(0), mcp.Min(0), mcp.Max(50)),
			mcp.WithInteger("max", mcp.Description("Max items to return (0 = unlimited)"), mcp.DefaultNumber(200), mcp.Min(0), mcp.Max(5000)),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			args := []string{"drive", "tree"}
			if parent := strings.TrimSpace(req.GetString("parent", "")); parent != "" {
				args = append(args, "--parent", parent)
			}
			if depth := req.GetInt("depth", 0); depth > 0 {
				args = append(args, "--depth", strconv.Itoa(clampMCPInt(depth, 1, 50)))
			}
			args = append(args, "--max", strconv.Itoa(clampMCPInt(req.GetInt("max", 200), 0, 5000)))
			return args, nil
		},
	}
}

func mcpCalendarFreeBusyTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "calendar_freebusy",
		Service:     "calendar",
		Risk:        mcpRiskRead,
		Description: "Query free/busy availability over a time window. Pass calendar IDs (comma-separated) or all=true for every calendar; default is the primary calendar.",
		Options: []mcp.ToolOption{
			mcp.WithString("from", mcp.Description("Start time: RFC3339, date, or relative (now, today)"), mcp.Required()),
			mcp.WithString("to", mcp.Description("End time: RFC3339, date, or relative"), mcp.Required()),
			mcp.WithString("calendars", mcp.Description("Calendar IDs to check (comma-separated); default primary")),
			mcp.WithBoolean("all", mcp.Description("Query all calendars"), mcp.DefaultBool(false)),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			from, err := requireMCPString(req, "from")
			if err != nil {
				return nil, err
			}
			to, err := requireMCPString(req, "to")
			if err != nil {
				return nil, err
			}
			all := req.GetBool("all", false)
			calendars := strings.TrimSpace(req.GetString("calendars", ""))
			if all && calendars != "" {
				return nil, fmt.Errorf("all and calendars are mutually exclusive")
			}
			args := []string{"calendar", "freebusy", "--from", from, "--to", to}
			if all {
				args = append(args, "--all")
			}
			for _, cal := range strings.Split(calendars, ",") {
				if cal = strings.TrimSpace(cal); cal != "" {
					args = append(args, "--cal", cal)
				}
			}
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
		BuildArgs: mcpPositionalArgs(
			[]string{"drive", "download"}, "file_id",
			[]mcpFlagArg{{"out", "--out"}, {"format", "--format"}},
			[]mcpFlagArg{{"overwrite", "--overwrite"}},
		),
	}
}

func mcpGmailGetAttachmentTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "gmail_get_attachment",
		Service:     "gmail",
		Risk:        mcpRiskWrite,
		Description: "Save a Gmail attachment to a local file on the machine running the server, and return its path. Requires --allow-write. Get message_id and attachment_id from gmail_get_message (format=full). Use gmail_read_attachment instead to get small attachment content inline without writing a file.",
		Options: []mcp.ToolOption{
			mcp.WithString("message_id", mcp.Description("Gmail message ID"), mcp.Required()),
			mcp.WithString("attachment_id", mcp.Description("Attachment ID from the message payload parts"), mcp.Required()),
			mcp.WithString("out", mcp.Description("Output local file path or directory (default: gogcli config dir)")),
			mcp.WithString("name", mcp.Description("Filename to use when 'out' is empty or a directory")),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			messageID, err := requireMCPString(req, "message_id")
			if err != nil {
				return nil, err
			}
			attachmentID, err := requireMCPString(req, "attachment_id")
			if err != nil {
				return nil, err
			}
			out := strings.TrimSpace(req.GetString("out", ""))
			if out == "-" {
				return nil, fmt.Errorf("out=- is not valid here; use gmail_read_attachment for inline content")
			}
			args := []string{"gmail", "attachment"}
			if out != "" {
				args = append(args, "--out", out)
			}
			if name := strings.TrimSpace(req.GetString("name", "")); name != "" {
				args = append(args, "--name", name)
			}
			return append(args, "--", messageID, attachmentID), nil
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
		BuildArgs: mcpPositionalArgs(
			[]string{"docs", "create"}, "title",
			[]mcpFlagArg{{"parent", "--parent"}, {"markdown_file", "--file"}},
			[]mcpFlagArg{{"pageless", "--pageless"}},
		),
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
		Risk:        mcpRiskWrite,
		Capability:  mcpCapabilityCreate,
		Description: "Upload a local file to Google Drive. Requires --allow-create on the MCP server.",
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
		Risk:        mcpRiskWrite,
		Capability:  mcpCapabilityCreate,
		Description: "Create a folder in Google Drive. Requires --allow-create on the MCP server.",
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
		Risk:        mcpRiskWrite,
		Capability:  mcpCapabilityCreate,
		Description: "Create a Google Calendar event. Requires --allow-create on the MCP server. Inviting attendees notifies them.",
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
		Risk:        mcpRiskWrite,
		Capability:  mcpCapabilityCreate,
		Description: "Clear values from a Google Sheets range. DESTRUCTIVE (data loss). Requires --allow-create on the MCP server.",
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

// mcpAttachArgs turns a comma-separated attachment string into repeated
// --attach flags, trimming blanks.
func mcpAttachArgs(raw string) []string {
	var out []string
	for _, path := range strings.Split(raw, ",") {
		if path = strings.TrimSpace(path); path != "" {
			out = append(out, "--attach", path)
		}
	}
	return out
}

// mcpGmailDraftWithAttachmentsTool fills the one gap upstream's Gmail suite
// leaves: none of its compose tools accept an attachment, and an MCP client
// cannot work around that, because passing bytes inline costs far more context
// than one call can carry. The CLI takes file PATHS instead, so the bytes never
// enter the conversation.
//
// Carries mcpCapabilityCreate rather than plain write: it reads arbitrary local
// files off the machine running the server and stages them for outbound mail.
// It only ever creates a draft, so sending stays behind upstream's
// --allow-gmail-send on a separate tool.
func mcpGmailDraftWithAttachmentsTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "gmail_create_draft_with_attachments",
		Service:     "gmail",
		Risk:        mcpRiskWrite,
		Capability:  mcpCapabilityCreate,
		Description: "Create a Gmail draft with file attachments, by local path. Does NOT send. Prefer upstream's gmail_create_draft when there is nothing to attach. Requires --allow-create on the MCP server.",
		Options: []mcp.ToolOption{
			mcp.WithString("subject", mcp.Description("Subject line"), mcp.Required()),
			mcp.WithString("attach", mcp.Description("Attachment local file path(s), comma-separated. Files are read from the machine running the server, not from this conversation."), mcp.Required()),
			mcp.WithString("body", mcp.Description("Plain-text body. Provide this or body_html (or both)."), mcp.DefaultString("")),
			mcp.WithString("body_html", mcp.Description("HTML body (optional)"), mcp.DefaultString("")),
			mcp.WithString("to", mcp.Description("Recipients (comma-separated; optional for a draft)")),
			mcp.WithString("cc", mcp.Description("CC recipients (comma-separated)")),
			mcp.WithString("bcc", mcp.Description("BCC recipients (comma-separated)")),
			mcp.WithString("from", mcp.Description("Send-as address (must be a configured alias of the account)")),
			mcp.WithString("reply_to_message_id", mcp.Description("Message ID to thread this draft onto. Set it at creation; a later update cannot attach a draft to a thread.")),
		},
		BuildArgs: func(req mcp.CallToolRequest) ([]string, error) {
			subject, err := requireMCPString(req, "subject")
			if err != nil {
				return nil, err
			}
			attach, err := requireMCPString(req, "attach")
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
			for _, pair := range [][2]string{
				{"to", "--to"},
				{"cc", "--cc"},
				{"bcc", "--bcc"},
				{"from", "--from"},
				{"reply_to_message_id", "--reply-to-message-id"},
			} {
				if v := strings.TrimSpace(req.GetString(pair[0], "")); v != "" {
					args = append(args, pair[1], v)
				}
			}
			return append(args, mcpAttachArgs(attach)...), nil
		},
	}
}
