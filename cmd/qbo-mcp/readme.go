package main

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// readmeText is readme.md: the approval rule, how names are matched and which
// tool does what. Embedded, so the text an assistant reads is the text of the
// build it is talking to.
//
//go:embed readme.md
var readmeText string

// readmeURI serves the same text as an MCP resource, for clients that read
// resources. The tool exists because many clients never do.
const readmeURI = "quickbooks-bridge://readme"

// instructions are sent on initialize. Most clients put them in the
// assistant's context, which is the one place a rule is sure to be seen.
const instructions = "QuickBooks Online through MCP. Call the readme tool before your first write. " +
	"Nothing is written without a draft the person approved: call draft_invoice, show the draft and its " +
	"problems, and call create_invoice only after the person says yes, with approved_total set to the total " +
	"they saw. Never pick between several matching customers or items yourself; ask."

type readmeArgs struct {
	Section string `json:"section,omitempty" jsonschema:"optional: part of a '## ' heading, such as 'approval rule' or 'tools'; returns just that section. Omit for the whole readme"`
}

func addReadme(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:  "readme",
		Title: "Readme: how this QuickBooks server works",
		Description: "Read this first. The approval rule for writing invoices, how customer and item names are " +
			"matched, and which tool does what. Returns Markdown. Read-only.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(_ context.Context, _ *mcp.CallToolRequest, a readmeArgs) (*mcp.CallToolResult, any, error) {
		text := readmeText
		if q := strings.TrimSpace(a.Section); q != "" {
			var ok bool
			if text, ok = readmeSection(readmeText, q); !ok {
				return nil, nil, fmt.Errorf("no section heading contains %q; omit section for the whole readme", q)
			}
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
	})

	s.AddResource(&mcp.Resource{
		Name:        "readme",
		Title:       "quickbooks-bridge readme",
		URI:         readmeURI,
		MIMEType:    "text/markdown",
		Description: "The approval rule and which tool does what. Same text as the readme tool.",
	}, func(_ context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
			URI: readmeURI, MIMEType: "text/markdown", Text: readmeText,
		}}}, nil
	})
}

// readmeSection returns the first "## " section whose heading contains q,
// case-insensitively, up to the next "## " heading.
func readmeSection(doc, q string) (string, bool) {
	q = strings.ToLower(q)
	var out []string
	in := false
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "## ") {
			if in {
				break
			}
			in = strings.Contains(strings.ToLower(line), q)
		}
		if in {
			out = append(out, line)
		}
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n"), len(out) > 0
}
