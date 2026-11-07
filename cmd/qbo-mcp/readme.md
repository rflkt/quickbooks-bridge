# quickbooks-bridge: how this server works

Read this before your first write. It explains what each tool returns and the one rule that governs writing to QuickBooks.

## The approval rule

Nothing is written to QuickBooks without a person approving a draft first.

1. Call `draft_invoice` with the order as the person wrote it: customer name, item names, quantities, and a price only if the order states one.
2. Show the person the draft: customer, lines, total, and every entry under `problems`.
3. If `ready` is false, ask the person to resolve each problem (for example, which of two matching items they meant), then call `draft_invoice` again with the corrected order.
4. Only when `ready` is true and the person has said yes, call `create_invoice` with the same order and `approved_total` set to the total they saw.

`create_invoice` rebuilds the draft from QuickBooks before writing. If the total changed in between (a price was edited, an item was renamed), it refuses and nothing is created. Show the new draft and ask again. Never guess an approved total, and never reuse one from an earlier draft.

## Matching names

- A name that matches exactly one active record is used.
- A name that matches several records is reported under `problems` with the candidates. Do not pick one yourself.
- A name that matches nothing is reported under `problems`. Suggest `search_customers` or `search_items` to the person.

## Tools

- `readme`: this text. Read-only.
- `search_customers`: active customers whose display name contains the text, with Id, open balance and email. Read-only.
- `search_items`: active products and services whose name contains the text, with Id, type and list price. Read-only.
- `list_open_invoices`: invoices with a balance still due, oldest due date first, optionally for one customer Id. Read-only.
- `draft_invoice`: turns an order into a draft invoice. Writes nothing.
- `create_invoice`: the only write. Creates the invoice from an approved draft, as described above.

## What this server does not do

- It does not send invoices to customers, record payments, or edit or delete existing invoices.
- It does not create customers or items. If one is missing, the person adds it in QuickBooks.
- It connects to one QuickBooks Online company, chosen once with `qbo-connect`.
