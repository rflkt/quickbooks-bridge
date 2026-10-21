package qbo

import (
	"context"
	"strings"
	"testing"
)

// fake mimics QuickBooks' LIKE '%name%' search over a small catalog.
type fake struct {
	customers []Customer
	items     []Item
}

func (f fake) SearchCustomers(_ context.Context, name string) ([]Customer, error) {
	var out []Customer
	for _, c := range f.customers {
		if strings.Contains(strings.ToLower(c.DisplayName), strings.ToLower(name)) {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f fake) SearchItems(_ context.Context, name string) ([]Item, error) {
	var out []Item
	for _, it := range f.items {
		if strings.Contains(strings.ToLower(it.Name), strings.ToLower(name)) {
			out = append(out, it)
		}
	}
	return out, nil
}

var catalog = fake{
	customers: []Customer{
		{ID: "1", DisplayName: "Amy's Bird Sanctuary"},
		{ID: "2", DisplayName: "Bill's Windsurf Shop"},
		{ID: "3", DisplayName: "Cool Cars"},
	},
	items: []Item{
		{ID: "5", Name: "Rock Fountain", UnitPrice: 275},
		{ID: "6", Name: "Sprinkler Heads", UnitPrice: 2},
		{ID: "7", Name: "Sprinkler Pipes", UnitPrice: 4},
		{ID: "8", Name: "Design", UnitPrice: 75},
	},
}

func price(f float64) *float64 { return &f }

func TestDraftReady(t *testing.T) {
	d, err := BuildDraft(context.Background(), catalog, DraftRequest{
		Customer: "cool cars",
		Lines: []DraftLineIn{
			{Item: "rock fountain", Qty: 1},
			{Item: "Sprinkler Heads", Qty: 20},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !d.Ready || d.Customer.Value != "3" || d.Total != 315 {
		t.Fatalf("unexpected draft: %+v", d)
	}
	inv, err := d.Invoice()
	if err != nil || len(inv.Line) != 2 || inv.Line[1].SalesItemLineDetail.ItemRef.Value != "6" {
		t.Fatalf("invoice: %+v %v", inv, err)
	}
}

func TestDraftRefusesToGuess(t *testing.T) {
	d, _ := BuildDraft(context.Background(), catalog, DraftRequest{
		Customer: "Bill",
		Lines:    []DraftLineIn{{Item: "sprinkler", Qty: 3}, {Item: "Garden gnome", Qty: 1}},
	})
	if d.Ready {
		t.Fatal("draft with an ambiguous item and an unknown item must not be ready")
	}
	joined := strings.Join(d.Problems, " | ")
	for _, want := range []string{`"sprinkler" matches 2 items`, `no item matches "Garden gnome"`} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing problem %q in %q", want, joined)
		}
	}
	if _, err := d.Invoice(); err == nil {
		t.Fatal("Invoice() must refuse a draft that is not ready")
	}
}

func TestDraftNotesPriceOverride(t *testing.T) {
	d, _ := BuildDraft(context.Background(), catalog, DraftRequest{
		Customer: "Amy's Bird Sanctuary",
		Lines:    []DraftLineIn{{Item: "Design", Qty: 2, UnitPrice: price(60)}},
	})
	if !d.Ready || d.Total != 120 || len(d.Notes) != 1 {
		t.Fatalf("unexpected draft: %+v", d)
	}
}
