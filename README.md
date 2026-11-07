# quickbooks-bridge

A QuickBooks MCP server in Go that lets Claude, ChatGPT or any MCP client read QuickBooks Online and draft invoices, with one rule: **nothing is written until a person approves the draft total.** It also ships a QuickBooks Desktop Web Connector (qbXML) endpoint.

It is a reference implementation, small enough to read in one sitting. The patterns come from MCP servers we run in production for clients; see [what changes in production](#what-changes-in-production).

| Part | QuickBooks | Interface | What it does |
|---|---|---|---|
| `cmd/qbo-mcp` | Online | MCP over Streamable HTTP | Search customers and items, list open invoices, draft an invoice from an order as written, create it once approved |
| `cmd/qbwc-server` | Desktop | Web Connector, SOAP, qbXML | The endpoint the Web Connector polls: reads customers, writes the invoices waiting in `outbox.json` |
| `cmd/qbo-connect` | Online | OAuth 2.0 | One-time handshake that saves `tokens.json` |

## Tools

| Tool | Writes? | What it does |
|---|---|---|
| `readme` | no | How the server works and the approval rule, so the assistant reads it before its first write. Also served as the resource `quickbooks-bridge://readme` |
| `search_customers` | no | Active customers by part of their name, with open balance and email |
| `search_items` | no | Active products and services by part of their name, with list price |
| `list_open_invoices` | no | Invoices with a balance due, oldest first, optionally for one customer |
| `draft_invoice` | no | Turns an order as written into a draft; anything ambiguous or missing is listed under `problems`, never guessed |
| `create_invoice` | **yes** | Creates the invoice, only if the rebuilt draft is ready and its total equals the `approved_total` the person saw |

Read-only tools carry `readOnlyHint`, so clients can call them without asking. `create_invoice` is marked not destructive but not idempotent, so clients ask before each call.

## The approval rule

`draft_invoice` matches names to QuickBooks records. A name that matches several records (two items called "sprinkler...") is reported with the candidates, not resolved. `create_invoice` rebuilds the draft from QuickBooks and refuses unless it is ready and its total equals `approved_total`. If a price changed between the draft and the approval, nothing is created.

The rule is enforced by the server, not by the prompt. It is also sent to the assistant three ways, because clients differ in what they read: the `initialize` instructions, the `readme` tool, and each tool's description. Every created invoice is logged with the approved and actual totals.

A worked run against a sandbox company, including the refused stale total, is in [QuickBooks MCP: Intuit's connector or your own?](https://reflektlab.ai/blog/quickbooks-mcp/).

## Run it (QuickBooks Online sandbox)

1. Create an app at developer.intuit.com (QuickBooks Online and Payments, Accounting scope). Intuit provisions a sandbox company.
2. In the app's development keys, add the redirect URI `http://localhost:8765/callback`.
3. `cp .env.example .env`, fill in the client ID and secret.
4. `set -a; . ./.env; set +a; go run ./cmd/qbo-connect` and approve the sandbox company.
5. `go run ./cmd/qbo-mcp`, then point an MCP client at `http://localhost:8090/mcp`. With Claude Code: `claude mcp add --transport http quickbooks http://localhost:8090/mcp`.

The server listens on localhost only. To listen on another address, set `QBO_MCP_TOKEN`; every request must then send `Authorization: Bearer <token>`. The server refuses to start on a non-local address without one.

## What changes in production

This server serves one person against one company. The MCP servers we run for clients (for example [Artnet Pro's field-service and sales platform](https://reflektlab.ai/case-studies/field-service-shipping-platform/), where completing the last work order creates the QuickBooks invoice) keep the same shape and add:

- **Per-user OAuth instead of a static token.** An OAuth 2.0 authorization server with PKCE in front of `/mcp`, discovery documents (RFC 9728 and RFC 8414), dynamic client registration for Claude, and a fixed confidential client for Gemini Enterprise, which cannot register itself.
- **A per-call user check.** Every call re-reads the user from the database instead of trusting a role in the token, so revoking access takes effect on the next call.
- **Attribution.** Every write made through an assistant is recorded in the record's history as made by that person, from Claude or Gemini.
- **Approval in the business process.** The draft step can be an object the team already approves, such as a quote or a work order, rather than a total passed back to the server.
- **Stateless serving.** `Stateless: true` and JSON responses (already set here), so the endpoint survives deploys and runs on several pods without sticky sessions.

## QuickBooks Desktop (Web Connector)

```bash
QBWC_PASSWORD=... go run ./cmd/qbwc-server -qwc https://your-host/qbwc > bridge.qwc
QBWC_PASSWORD=... go run ./cmd/qbwc-server -outbox outbox.json
```

Add `bridge.qwc` in the Web Connector on the Windows machine running QuickBooks, type the password, and run it. The Web Connector accepts plain http only for `localhost`.

Tested against the protocol in Intuit's QBWC Programmer's Guide (version 2.0) with simulated Web Connector calls and qbXML responses, including QuickBooks error 3140; not yet run against a live Desktop company file. Background: [QuickBooks Desktop is going away: what to do](https://reflektlab.ai/blog/quickbooks-desktop-going-away/).

## Tests

```bash
go test ./...
```

## Licence

Apache 2.0. Built by [Reflekt Lab](https://reflektlab.ai), who build and run MCP servers and QuickBooks integrations for small and mid-size companies. More: [QuickBooks API integration](https://reflektlab.ai/blog/quickbooks-api-integration/), [QuickBooks MCP](https://reflektlab.ai/blog/quickbooks-mcp/).
