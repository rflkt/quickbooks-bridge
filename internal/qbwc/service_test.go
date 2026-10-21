package qbwc

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// call posts a SOAP request the way the Web Connector does and returns the
// result strings from the response.
func call(t *testing.T, url, method string, params map[string]string, order ...string) []string {
	t.Helper()
	var p strings.Builder
	for _, k := range order {
		p.WriteString("<" + k + ">")
		_ = xml.EscapeText(&p, []byte(params[k]))
		p.WriteString("</" + k + ">")
	}
	body := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xmlns:xsd="http://www.w3.org/2001/XMLSchema">
<soap:Body><%s xmlns="http://developer.intuit.com/">%s</%s></soap:Body></soap:Envelope>`, method, p.String(), method)
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("SOAPAction", `"http://developer.intuit.com/`+method+`"`)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var env struct {
		Body struct {
			Inner struct {
				Result struct {
					Text    string   `xml:",chardata"`
					Strings []string `xml:"string"`
				} `xml:",any"`
			} `xml:",any"`
		} `xml:"Body"`
	}
	if err := xml.Unmarshal(raw, &env); err != nil {
		t.Fatalf("%s: bad SOAP response %s: %v", method, raw, err)
	}
	if s := env.Body.Inner.Result.Strings; len(s) > 0 {
		return s
	}
	return []string{env.Body.Inner.Result.Text}
}

const customerQueryRs = `<?xml version="1.0" ?>
<QBXML><QBXMLMsgsRs>
<CustomerQueryRs requestID="customers" statusCode="0" statusSeverity="Info" statusMessage="Status OK">
<CustomerRet><ListID>80000001-1193075473</ListID><Name>Kristy Abercrombie</Name><FullName>Abercrombie, Kristy</FullName><TotalBalance>1584.38</TotalBalance></CustomerRet>
<CustomerRet><ListID>80000002-1193075473</ListID><Name>Cook, Brian</Name><FullName>Cook, Brian</FullName><TotalBalance>0.00</TotalBalance></CustomerRet>
</CustomerQueryRs></QBXMLMsgsRs></QBXML>`

const invoiceAddRsOK = `<?xml version="1.0" ?>
<QBXML><QBXMLMsgsRs>
<InvoiceAddRs requestID="invoice-1" statusCode="0" statusSeverity="Info" statusMessage="Status OK">
<InvoiceRet><TxnID>2A9-1193086434</TxnID><RefNumber>1042</RefNumber><Subtotal>250.00</Subtotal></InvoiceRet>
</InvoiceAddRs></QBXMLMsgsRs></QBXML>`

const invoiceAddRsBadRef = `<?xml version="1.0" ?>
<QBXML><QBXMLMsgsRs>
<InvoiceAddRs requestID="invoice-1" statusCode="3140" statusSeverity="Error" statusMessage="There is an invalid reference to QuickBooks Item &quot;Garden gnome&quot; in the Invoice line." />
</QBXMLMsgsRs></QBXML>`

func newServer(t *testing.T, orders []Order) (*httptest.Server, *[]CustomerRet, *[]InvoiceRet) {
	var customers []CustomerRet
	var invoices []InvoiceRet
	svc := &Service{
		User: "bridge", Password: "s3cret",
		Outbox:         func() []Order { return orders },
		OnCustomers:    func(c []CustomerRet) { customers = append(customers, c...) },
		OnInvoiceAdded: func(i InvoiceRet) { invoices = append(invoices, i) },
	}
	srv := httptest.NewServer(svc)
	t.Cleanup(srv.Close)
	return srv, &customers, &invoices
}

var order = Order{
	Customer: "Abercrombie, Kristy", RefNumber: "1042", TxnDate: "2026-10-21",
	Lines: []OrderLine{{Item: "Installation", Desc: "On-site install", Quantity: 2, Rate: 125}},
}

func TestWebConnectorSession(t *testing.T) {
	srv, customers, invoices := newServer(t, []Order{order})

	auth := call(t, srv.URL, "authenticate", map[string]string{"strUserName": "bridge", "strPassword": "s3cret"}, "strUserName", "strPassword")
	if len(auth) != 2 || auth[1] != "" {
		t.Fatalf("authenticate: %q", auth)
	}
	ticket := auth[0]
	send := func() string {
		return call(t, srv.URL, "sendRequestXML", map[string]string{
			"ticket": ticket, "strHCPResponse": "", "strCompanyFileName": `C:\Company.qbw`,
			"qbXMLCountry": "US", "qbXMLMajorVers": "16", "qbXMLMinorVers": "0",
		}, "ticket", "strHCPResponse", "strCompanyFileName", "qbXMLCountry", "qbXMLMajorVers", "qbXMLMinorVers")[0]
	}
	receive := func(resp string) string {
		return call(t, srv.URL, "receiveResponseXML", map[string]string{
			"ticket": ticket, "response": resp, "hresult": "", "message": "",
		}, "ticket", "response", "hresult", "message")[0]
	}

	rq := send()
	if !strings.Contains(rq, `<?qbxml version="13.0"?>`) || !strings.Contains(rq, `<CustomerQueryRq requestID="customers"><MaxReturned>25</MaxReturned><ActiveStatus>ActiveOnly</ActiveStatus></CustomerQueryRq>`) {
		t.Fatalf("first request: %s", rq)
	}
	if pct := receive(customerQueryRs); pct != "50" {
		t.Fatalf("after customers: %s%%", pct)
	}
	if len(*customers) != 2 || (*customers)[0].FullName != "Abercrombie, Kristy" {
		t.Fatalf("customers: %+v", *customers)
	}

	rq = send()
	want := `<InvoiceAddRq requestID="invoice-1"><InvoiceAdd><CustomerRef><FullName>Abercrombie, Kristy</FullName></CustomerRef><TxnDate>2026-10-21</TxnDate><RefNumber>1042</RefNumber><InvoiceLineAdd><ItemRef><FullName>Installation</FullName></ItemRef><Desc>On-site install</Desc><Quantity>2</Quantity><Rate>125.00</Rate></InvoiceLineAdd></InvoiceAdd></InvoiceAddRq>`
	if !strings.Contains(rq, want) {
		t.Fatalf("invoice request:\n%s\nwant:\n%s", rq, want)
	}
	if pct := receive(invoiceAddRsOK); pct != "100" {
		t.Fatalf("after invoice: %s%%", pct)
	}
	if len(*invoices) != 1 || (*invoices)[0].TxnID != "2A9-1193086434" {
		t.Fatalf("invoices: %+v", *invoices)
	}
	if got := call(t, srv.URL, "closeConnection", map[string]string{"ticket": ticket}, "ticket")[0]; got != "OK" {
		t.Fatalf("closeConnection: %q", got)
	}
}

func TestWebConnectorRejectsBadPassword(t *testing.T) {
	srv, _, _ := newServer(t, nil)
	auth := call(t, srv.URL, "authenticate", map[string]string{"strUserName": "bridge", "strPassword": "wrong"}, "strUserName", "strPassword")
	if len(auth) != 2 || auth[1] != "nvu" {
		t.Fatalf("authenticate with a bad password: %q", auth)
	}
}

func TestWebConnectorReportsQuickBooksError(t *testing.T) {
	srv, _, invoices := newServer(t, []Order{order})
	ticket := call(t, srv.URL, "authenticate", map[string]string{"strUserName": "bridge", "strPassword": "s3cret"}, "strUserName", "strPassword")[0]
	resp := func(r string) string {
		return call(t, srv.URL, "receiveResponseXML", map[string]string{"ticket": ticket, "response": r}, "ticket", "response", "hresult", "message")[0]
	}
	resp(customerQueryRs)
	if pct := resp(invoiceAddRsBadRef); pct != "-103" {
		t.Fatalf("a QuickBooks error must return a negative value, got %s", pct)
	}
	msg := call(t, srv.URL, "getLastError", map[string]string{"ticket": ticket}, "ticket")[0]
	if !strings.Contains(msg, "3140") || !strings.Contains(msg, "Garden gnome") {
		t.Fatalf("getLastError: %q", msg)
	}
	if len(*invoices) != 0 {
		t.Fatal("no invoice should be recorded")
	}
}

func TestQWC(t *testing.T) {
	q := QWC("Bridge", "https://bridge.example.com/qbwc", "bridge", "57F3B9B1-86F1-4FCC-B1EE-566DE1813D20", "90A44FB5-33D9-4815-AC85-BC87A7E7D1EB", 15)
	var v struct {
		AppURL   string `xml:"AppURL"`
		UserName string `xml:"UserName"`
		OwnerID  string `xml:"OwnerID"`
	}
	if err := xml.Unmarshal([]byte(q), &v); err != nil || v.AppURL != "https://bridge.example.com/qbwc" || v.OwnerID != "{57F3B9B1-86F1-4FCC-B1EE-566DE1813D20}" {
		t.Fatalf("qwc: %+v %v\n%s", v, err, q)
	}
}
