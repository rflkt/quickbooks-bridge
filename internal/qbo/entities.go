package qbo

import (
	"context"
	"fmt"
	"net/http"
)

// Only the fields the tools return. QuickBooks sends many more.

type Ref struct {
	Value string `json:"value"`
	Name  string `json:"name,omitempty"`
}

type Customer struct {
	ID          string  `json:"Id"`
	DisplayName string  `json:"DisplayName"`
	Balance     float64 `json:"Balance"`
	Email       *struct {
		Address string `json:"Address"`
	} `json:"PrimaryEmailAddr,omitempty"`
}

type Item struct {
	ID        string  `json:"Id"`
	Name      string  `json:"Name"`
	Type      string  `json:"Type"`
	UnitPrice float64 `json:"UnitPrice"`
	Active    bool    `json:"Active"`
}

type Invoice struct {
	ID          string  `json:"Id,omitempty"`
	DocNumber   string  `json:"DocNumber,omitempty"`
	TxnDate     string  `json:"TxnDate,omitempty"`
	DueDate     string  `json:"DueDate,omitempty"`
	TotalAmt    float64 `json:"TotalAmt,omitempty"`
	Balance     float64 `json:"Balance,omitempty"`
	CustomerRef Ref     `json:"CustomerRef"`
	Line        []Line  `json:"Line"`
}

type Line struct {
	Amount              float64          `json:"Amount"`
	DetailType          string           `json:"DetailType"`
	Description         string           `json:"Description,omitempty"`
	SalesItemLineDetail *SalesItemDetail `json:"SalesItemLineDetail,omitempty"`
}

type SalesItemDetail struct {
	ItemRef   Ref     `json:"ItemRef"`
	Qty       float64 `json:"Qty"`
	UnitPrice float64 `json:"UnitPrice"`
}

func (c *Client) SearchCustomers(ctx context.Context, name string) ([]Customer, error) {
	q := "select * from Customer where Active = true"
	if name != "" {
		q += " and DisplayName like " + Quote("%"+name+"%")
	}
	q += " maxresults 25"
	var r struct {
		QueryResponse struct {
			Customer []Customer `json:"Customer"`
		} `json:"QueryResponse"`
	}
	if err := c.Query(ctx, q, &r); err != nil {
		return nil, err
	}
	return r.QueryResponse.Customer, nil
}

func (c *Client) SearchItems(ctx context.Context, name string) ([]Item, error) {
	q := "select * from Item where Active = true"
	if name != "" {
		q += " and Name like " + Quote("%"+name+"%")
	}
	q += " maxresults 50"
	var r struct {
		QueryResponse struct {
			Item []Item `json:"Item"`
		} `json:"QueryResponse"`
	}
	if err := c.Query(ctx, q, &r); err != nil {
		return nil, err
	}
	return r.QueryResponse.Item, nil
}

func (c *Client) OpenInvoices(ctx context.Context, customerID string) ([]Invoice, error) {
	q := "select * from Invoice where Balance > '0'"
	if customerID != "" {
		q += " and CustomerRef = " + Quote(customerID)
	}
	q += " orderby DueDate maxresults 50"
	var r struct {
		QueryResponse struct {
			Invoice []Invoice `json:"Invoice"`
		} `json:"QueryResponse"`
	}
	if err := c.Query(ctx, q, &r); err != nil {
		return nil, err
	}
	return r.QueryResponse.Invoice, nil
}

func (c *Client) CreateInvoice(ctx context.Context, inv Invoice) (Invoice, error) {
	var r struct {
		Invoice Invoice `json:"Invoice"`
	}
	if err := c.do(ctx, http.MethodPost, "invoice", nil, inv, &r); err != nil {
		return Invoice{}, err
	}
	if r.Invoice.ID == "" {
		return Invoice{}, fmt.Errorf("quickbooks returned no invoice id")
	}
	return r.Invoice, nil
}
