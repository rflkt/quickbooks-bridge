// qbo-mcp exposes a QuickBooks Online company to AI assistants as MCP tools.
// Reads are free. The one write, create_invoice, only runs on a draft whose
// total the person has seen: the assistant has to pass that total back.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rflkt/quickbooks-bridge/internal/qbo"
)

type nameArgs struct {
	Name string `json:"name,omitempty" jsonschema:"part of the name to search for; empty lists all"`
}

type openInvoicesArgs struct {
	CustomerID string `json:"customer_id,omitempty" jsonschema:"QuickBooks customer Id; empty lists all open invoices"`
}

type createArgs struct {
	qbo.DraftRequest
	ApprovedTotal float64 `json:"approved_total" jsonschema:"the draft total the person approved; the invoice is refused if the total has changed"`
}

type customersOut struct {
	Customers []qbo.Customer `json:"customers"`
}
type itemsOut struct {
	Items []qbo.Item `json:"items"`
}
type invoicesOut struct {
	Invoices []qbo.Invoice `json:"invoices"`
}
type createdOut struct {
	Invoice qbo.Invoice `json:"invoice"`
}

func main() {
	addr := flag.String("addr", "localhost:8090", "listen address")
	flag.Parse()

	cfg, err := qbo.ConfigFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	client, err := qbo.NewClient(cfg)
	if err != nil {
		log.Fatal(err)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "quickbooks-bridge", Version: "v0.1.0"}, nil)
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_customers",
		Description: "Find active QuickBooks customers by part of their display name. Returns Id, name, open balance and email.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a nameArgs) (*mcp.CallToolResult, customersOut, error) {
		cs, err := client.SearchCustomers(ctx, a.Name)
		return nil, customersOut{Customers: cs}, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_items",
		Description: "Find active products and services by part of their name. Returns Id, name, type and list price.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a nameArgs) (*mcp.CallToolResult, itemsOut, error) {
		items, err := client.SearchItems(ctx, a.Name)
		return nil, itemsOut{Items: items}, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_open_invoices",
		Description: "List invoices with a balance still due, oldest due date first, optionally for one customer.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a openInvoicesArgs) (*mcp.CallToolResult, invoicesOut, error) {
		invs, err := client.OpenInvoices(ctx, a.CustomerID)
		return nil, invoicesOut{Invoices: invs}, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "draft_invoice",
		Description: "Turn an order (customer name, items and quantities as written) into a draft invoice: " +
			"names are matched to QuickBooks records, prices come from the item list unless the order states one, " +
			"and anything ambiguous or missing is listed under problems instead of guessed. Writes nothing. " +
			"Show the draft to the person before calling create_invoice.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a qbo.DraftRequest) (*mcp.CallToolResult, qbo.Draft, error) {
		d, err := qbo.BuildDraft(ctx, client, a)
		return nil, d, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "create_invoice",
		Description: "Create the invoice in QuickBooks from the same order sent to draft_invoice, once the person has approved the draft. " +
			"approved_total must equal the draft total they saw; if anything changed in between, nothing is created.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a createArgs) (*mcp.CallToolResult, createdOut, error) {
		d, err := qbo.BuildDraft(ctx, client, a.DraftRequest)
		if err != nil {
			return nil, createdOut{}, err
		}
		if !d.Ready {
			return nil, createdOut{}, fmt.Errorf("draft is not ready, nothing created: %v", d.Problems)
		}
		if !qbo.SameTotal(d.Total, a.ApprovedTotal) {
			return nil, createdOut{}, fmt.Errorf("total is now %.2f but %.2f was approved; show the new draft first", d.Total, a.ApprovedTotal)
		}
		inv, err := d.Invoice()
		if err != nil {
			return nil, createdOut{}, err
		}
		created, err := client.CreateInvoice(ctx, inv)
		return nil, createdOut{Invoice: created}, err
	})

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	http.Handle("/mcp", handler)
	log.Printf("QuickBooks MCP server on http://%s/mcp (%s)", *addr, cfg.BaseURL())
	log.Fatal(http.ListenAndServe(*addr, nil))
}
