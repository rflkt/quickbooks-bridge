// Package qbwc implements the web-service side of the QuickBooks Web
// Connector (QBWC): the SOAP callbacks it polls, and the qbXML requests and
// responses that travel through them to QuickBooks Desktop.
package qbwc

import (
	"encoding/xml"
	"fmt"
	"strconv"
)

// qbXML is order-sensitive: QuickBooks rejects a request whose elements are
// not in schema order, so requests are built from structs, never by
// concatenating strings.

// MaxQBXMLMajor is the highest qbXML spec we write. If the request processor
// reports an older version in sendRequestXML, we write that one instead.
const MaxQBXMLMajor = 13

type ref struct {
	ListID   string `xml:"ListID,omitempty"`
	FullName string `xml:"FullName,omitempty"`
}

type CustomerQueryRq struct {
	XMLName      xml.Name `xml:"CustomerQueryRq"`
	RequestID    string   `xml:"requestID,attr"`
	MaxReturned  int      `xml:"MaxReturned,omitempty"`
	ActiveStatus string   `xml:"ActiveStatus,omitempty"`
}

type InvoiceLineAdd struct {
	ItemRef  ref    `xml:"ItemRef"`
	Desc     string `xml:"Desc,omitempty"`
	Quantity string `xml:"Quantity,omitempty"`
	Rate     string `xml:"Rate,omitempty"`
}

type InvoiceAdd struct {
	CustomerRef    ref              `xml:"CustomerRef"`
	TxnDate        string           `xml:"TxnDate,omitempty"`
	RefNumber      string           `xml:"RefNumber,omitempty"`
	Memo           string           `xml:"Memo,omitempty"`
	InvoiceLineAdd []InvoiceLineAdd `xml:"InvoiceLineAdd"`
}

type InvoiceAddRq struct {
	XMLName    xml.Name   `xml:"InvoiceAddRq"`
	RequestID  string     `xml:"requestID,attr"`
	InvoiceAdd InvoiceAdd `xml:"InvoiceAdd"`
}

// Envelope wraps one request in the qbXML document QuickBooks expects.
func Envelope(major int, rq any) (string, error) {
	if major <= 0 || major > MaxQBXMLMajor {
		major = MaxQBXMLMajor
	}
	body, err := xml.Marshal(rq)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>`+"\n"+
		`<?qbxml version="%d.0"?>`+"\n"+
		`<QBXML><QBXMLMsgsRq onError="stopOnError">%s</QBXMLMsgsRq></QBXML>`, major, body), nil
}

// Status is the result every qbXML response carries. statusCode 0 is
// success, 1 means "no matching objects" on a query, anything else is an
// error with a human-readable statusMessage.
type Status struct {
	RequestID string `xml:"requestID,attr"`
	Code      string `xml:"statusCode,attr"`
	Severity  string `xml:"statusSeverity,attr"`
	Message   string `xml:"statusMessage,attr"`
}

func (s Status) OK() bool { return s.Code == "0" || s.Code == "1" }

func (s Status) Error() string {
	return fmt.Sprintf("QuickBooks statusCode %s (%s): %s", s.Code, s.Severity, s.Message)
}

type CustomerRet struct {
	ListID       string `xml:"ListID"`
	FullName     string `xml:"FullName"`
	TotalBalance string `xml:"TotalBalance"`
}

type InvoiceRet struct {
	TxnID     string `xml:"TxnID"`
	RefNumber string `xml:"RefNumber"`
	Subtotal  string `xml:"Subtotal"`
}

type msgsRs struct {
	CustomerQueryRs []struct {
		Status
		Customers []CustomerRet `xml:"CustomerRet"`
	} `xml:"QBXMLMsgsRs>CustomerQueryRs"`
	InvoiceAddRs []struct {
		Status
		Invoice InvoiceRet `xml:"InvoiceRet"`
	} `xml:"QBXMLMsgsRs>InvoiceAddRs"`
}

// Response is the decoded part of a qbXML response we act on.
type Response struct {
	Statuses  []Status
	Customers []CustomerRet
	Invoices  []InvoiceRet
}

func ParseResponse(raw string) (Response, error) {
	var m msgsRs
	if err := xml.Unmarshal([]byte(raw), &m); err != nil {
		return Response{}, fmt.Errorf("unreadable qbXML response: %w", err)
	}
	var r Response
	for _, q := range m.CustomerQueryRs {
		r.Statuses = append(r.Statuses, q.Status)
		r.Customers = append(r.Customers, q.Customers...)
	}
	for _, a := range m.InvoiceAddRs {
		r.Statuses = append(r.Statuses, a.Status)
		if a.Invoice.TxnID != "" {
			r.Invoices = append(r.Invoices, a.Invoice)
		}
	}
	return r, nil
}

func money(f float64) string { return strconv.FormatFloat(f, 'f', 2, 64) }
