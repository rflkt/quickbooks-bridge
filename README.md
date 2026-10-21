# quickbooks-bridge

A working proof of the two ways software talks to QuickBooks, with a person approving every write.

| Part | QuickBooks | Interface | What it does |
|---|---|---|---|
| `cmd/qbo-mcp` | Online | REST/JSON, OAuth 2.0 | MCP server: search customers and items, list open invoices, draft an invoice from an order as written, create it once approved |
| `cmd/qbwc-server` | Desktop | Web Connector, SOAP, qbXML | The endpoint the Web Connector polls: reads customers, writes the invoices waiting in `outbox.json` |
| `cmd/qbo-connect` | Online | OAuth 2.0 | One-time handshake that saves `tokens.json` |

## The approval rule

`draft_invoice` matches names to QuickBooks records and lists anything ambiguous or missing instead of guessing. `create_invoice` rebuilds the draft and refuses unless it is ready and its total equals the `approved_total` the person saw.

## QuickBooks Online (sandbox)

1. Create an app at developer.intuit.com (QuickBooks Online and Payments, Accounting scope). Intuit provisions a sandbox company.
2. In the app's development keys, add the redirect URI `http://localhost:8765/callback`.
3. `cp .env.example .env`, fill in the client ID and secret.
4. `set -a; . ./.env; set +a; go run ./cmd/qbo-connect` and approve the sandbox company.
5. `go run ./cmd/qbo-mcp`, then point an MCP client at `http://localhost:8090/mcp`.

## QuickBooks Desktop (Web Connector)

```bash
QBWC_PASSWORD=... go run ./cmd/qbwc-server -qwc https://your-host/qbwc > bridge.qwc
QBWC_PASSWORD=... go run ./cmd/qbwc-server -outbox outbox.json
```

Add `bridge.qwc` in the Web Connector on the Windows machine running QuickBooks, type the password, and run it. The Web Connector accepts plain http only for `localhost`.

Tested against the protocol in Intuit's QBWC Programmer's Guide (version 2.0) with simulated Web Connector calls and qbXML responses, including QuickBooks error 3140; not yet run against a live Desktop company file.

## Tests

```bash
go test ./...
```
