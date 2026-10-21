package qbo

import (
	"context"
	"fmt"
	"math"
	"strings"
)

// Resolver is the part of the client that drafting needs. Tests use a fake.
type Resolver interface {
	SearchCustomers(ctx context.Context, name string) ([]Customer, error)
	SearchItems(ctx context.Context, name string) ([]Item, error)
}

type DraftLineIn struct {
	Item      string   `json:"item" jsonschema:"product or service name as the customer wrote it"`
	Qty       float64  `json:"qty" jsonschema:"quantity, greater than zero"`
	UnitPrice *float64 `json:"unit_price,omitempty" jsonschema:"only if the order states a price; otherwise the item's list price is used"`
}

type DraftRequest struct {
	Customer string        `json:"customer" jsonschema:"customer name as written in the order"`
	Lines    []DraftLineIn `json:"lines" jsonschema:"one entry per ordered product or service"`
}

type DraftLineOut struct {
	Requested   string  `json:"requested"`
	Item        *Ref    `json:"item,omitempty"`
	Qty         float64 `json:"qty"`
	UnitPrice   float64 `json:"unit_price"`
	Amount      float64 `json:"amount"`
	PriceSource string  `json:"price_source,omitempty"`
}

// Draft is what a person reviews before anything is written. Ready is false
// whenever something had to be guessed; the problems say what.
type Draft struct {
	Ready    bool           `json:"ready"`
	Customer *Ref           `json:"customer,omitempty"`
	Lines    []DraftLineOut `json:"lines"`
	Total    float64        `json:"total"`
	Problems []string       `json:"problems,omitempty"`
	Notes    []string       `json:"notes,omitempty"`
}

// BuildDraft resolves names to QuickBooks records. It never picks between two
// plausible matches: ambiguity is reported, not decided.
func BuildDraft(ctx context.Context, r Resolver, req DraftRequest) (Draft, error) {
	d := Draft{}
	if strings.TrimSpace(req.Customer) == "" {
		d.Problems = append(d.Problems, "no customer given")
	} else {
		cs, err := r.SearchCustomers(ctx, req.Customer)
		if err != nil {
			return d, err
		}
		names := make([]string, len(cs))
		for i, c := range cs {
			names[i] = c.DisplayName
		}
		if i, problem := pick(req.Customer, names, "customer"); problem != "" {
			d.Problems = append(d.Problems, problem)
		} else {
			d.Customer = &Ref{Value: cs[i].ID, Name: cs[i].DisplayName}
		}
	}

	if len(req.Lines) == 0 {
		d.Problems = append(d.Problems, "no lines given")
	}
	for _, in := range req.Lines {
		out := DraftLineOut{Requested: in.Item, Qty: in.Qty}
		if in.Qty <= 0 {
			d.Problems = append(d.Problems, fmt.Sprintf("%q: quantity must be greater than zero", in.Item))
		}
		items, err := r.SearchItems(ctx, in.Item)
		if err != nil {
			return d, err
		}
		names := make([]string, len(items))
		for i, it := range items {
			names[i] = it.Name
		}
		i, problem := pick(in.Item, names, "item")
		if problem != "" {
			d.Problems = append(d.Problems, problem)
			d.Lines = append(d.Lines, out)
			continue
		}
		it := items[i]
		out.Item = &Ref{Value: it.ID, Name: it.Name}
		out.UnitPrice = it.UnitPrice
		out.PriceSource = "item list price"
		if in.UnitPrice != nil {
			out.UnitPrice = *in.UnitPrice
			out.PriceSource = "price stated in the order"
			if !sameMoney(*in.UnitPrice, it.UnitPrice) {
				d.Notes = append(d.Notes, fmt.Sprintf("%s: order says %.2f, list price is %.2f", it.Name, *in.UnitPrice, it.UnitPrice))
			}
		}
		if out.UnitPrice == 0 {
			d.Problems = append(d.Problems, fmt.Sprintf("%s has no list price and the order states none", it.Name))
		}
		out.Amount = round2(out.Qty * out.UnitPrice)
		d.Total = round2(d.Total + out.Amount)
		d.Lines = append(d.Lines, out)
	}
	d.Ready = len(d.Problems) == 0
	return d, nil
}

// pick returns the index of the single acceptable match: an exact
// (case-insensitive) name, or the only candidate.
func pick(asked string, candidates []string, kind string) (int, string) {
	for i, c := range candidates {
		if strings.EqualFold(strings.TrimSpace(c), strings.TrimSpace(asked)) {
			return i, ""
		}
	}
	switch len(candidates) {
	case 0:
		return -1, fmt.Sprintf("no %s matches %q", kind, asked)
	case 1:
		return 0, ""
	default:
		show := candidates
		if len(show) > 5 {
			show = show[:5]
		}
		return -1, fmt.Sprintf("%q matches %d %ss (%s): which one?", asked, len(candidates), kind, strings.Join(show, ", "))
	}
}

// Invoice turns a ready draft into the QuickBooks payload.
func (d Draft) Invoice() (Invoice, error) {
	if !d.Ready || d.Customer == nil {
		return Invoice{}, fmt.Errorf("draft is not ready: %s", strings.Join(d.Problems, "; "))
	}
	inv := Invoice{CustomerRef: Ref{Value: d.Customer.Value}}
	for _, l := range d.Lines {
		inv.Line = append(inv.Line, Line{
			Amount:     l.Amount,
			DetailType: "SalesItemLineDetail",
			SalesItemLineDetail: &SalesItemDetail{
				ItemRef:   Ref{Value: l.Item.Value},
				Qty:       l.Qty,
				UnitPrice: l.UnitPrice,
			},
		})
	}
	return inv, nil
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }

func sameMoney(a, b float64) bool { return math.Abs(a-b) < 0.005 }

// SameTotal reports whether the total a person approved still matches.
func SameTotal(a, b float64) bool { return sameMoney(a, b) }
