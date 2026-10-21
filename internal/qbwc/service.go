package qbwc

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync"
)

const intuitNS = "http://developer.intuit.com/"

// Order is one invoice waiting to be written into QuickBooks Desktop.
type Order struct {
	Customer  string
	RefNumber string
	TxnDate   string
	Memo      string
	Lines     []OrderLine
}

type OrderLine struct {
	Item     string
	Desc     string
	Quantity float64
	Rate     float64
}

// session is one Web Connector run: a queue of qbXML requests and what came
// back. QBWC drives it by calling sendRequestXML and receiveResponseXML in
// turn until we report 100% or an error.
type session struct {
	steps     []func(major int) (string, error)
	next      int
	lastError string
}

// Service is the SOAP endpoint the Web Connector polls.
type Service struct {
	User, Password string
	// Outbox returns the invoices to write on this run.
	Outbox func() []Order
	// OnCustomers and OnInvoiceAdded receive what QuickBooks sent back.
	OnCustomers    func([]CustomerRet)
	OnInvoiceAdded func(InvoiceRet)

	mu       sync.Mutex
	sessions map[string]*session
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "QBWC posts SOAP to this URL", http.StatusMethodNotAllowed)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	method, params, err := parseCall(raw)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	result := s.dispatch(method, params)
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	_, _ = w.Write(soapResponse(method, result))
}

// dispatch implements the callbacks from the QBWC Programmer's Guide
// (chapter 10). result is either a string or a []string.
func (s *Service) dispatch(method string, p map[string]string) any {
	switch method {
	case "serverVersion":
		return "quickbooks-bridge 0.1"
	case "clientVersion":
		return "" // any QBWC version is fine
	case "authenticate":
		return s.authenticate(p["strUserName"], p["strPassword"])
	case "sendRequestXML":
		major, _ := strconv.Atoi(p["qbXMLMajorVers"])
		return s.sendRequestXML(p["ticket"], major)
	case "receiveResponseXML":
		return strconv.Itoa(s.receiveResponseXML(p["ticket"], p["response"], p["hresult"], p["message"]))
	case "connectionError":
		s.fail(p["ticket"], fmt.Sprintf("connection error %s: %s", p["hresult"], p["message"]))
		return "done"
	case "getLastError":
		return s.lastError(p["ticket"])
	case "closeConnection":
		s.mu.Lock()
		delete(s.sessions, p["ticket"])
		s.mu.Unlock()
		return "OK"
	default:
		log.Printf("qbwc: unknown method %q", method)
		return ""
	}
}

// authenticate returns [ticket, company]. The guide's prose numbers the
// array from the company file, but its sample and every other callback treat
// element 0 as the session ticket: element 1 is "" (use the open company
// file), "none" (nothing to do) or "nvu" (bad credentials).
func (s *Service) authenticate(user, password string) []string {
	ticket := newTicket()
	if subtle.ConstantTimeCompare([]byte(user), []byte(s.User)) != 1 ||
		subtle.ConstantTimeCompare([]byte(password), []byte(s.Password)) != 1 {
		return []string{ticket, "nvu"}
	}
	sess := &session{}
	sess.steps = append(sess.steps, func(major int) (string, error) {
		return Envelope(major, CustomerQueryRq{RequestID: "customers", MaxReturned: 25, ActiveStatus: "ActiveOnly"})
	})
	if s.Outbox != nil {
		for i, o := range s.Outbox() {
			o, id := o, fmt.Sprintf("invoice-%d", i+1)
			sess.steps = append(sess.steps, func(major int) (string, error) {
				return Envelope(major, invoiceAddRq(id, o))
			})
		}
	}
	s.mu.Lock()
	if s.sessions == nil {
		s.sessions = map[string]*session{}
	}
	s.sessions[ticket] = sess
	s.mu.Unlock()
	return []string{ticket, ""}
}

func invoiceAddRq(id string, o Order) InvoiceAddRq {
	add := InvoiceAdd{
		CustomerRef: ref{FullName: o.Customer},
		TxnDate:     o.TxnDate,
		RefNumber:   o.RefNumber,
		Memo:        o.Memo,
	}
	for _, l := range o.Lines {
		add.InvoiceLineAdd = append(add.InvoiceLineAdd, InvoiceLineAdd{
			ItemRef:  ref{FullName: l.Item},
			Desc:     l.Desc,
			Quantity: strconv.FormatFloat(l.Quantity, 'f', -1, 64),
			Rate:     money(l.Rate),
		})
	}
	return InvoiceAddRq{RequestID: id, InvoiceAdd: add}
}

// sendRequestXML returns the next qbXML request, or "" when there is none.
func (s *Service) sendRequestXML(ticket string, major int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[ticket]
	if sess == nil || sess.next >= len(sess.steps) {
		return ""
	}
	xmlReq, err := sess.steps[sess.next](major)
	if err != nil {
		sess.lastError = err.Error()
		return ""
	}
	return xmlReq
}

// receiveResponseXML returns the percentage done, or a negative number on
// error, which makes QBWC call getLastError.
func (s *Service) receiveResponseXML(ticket, response, hresult, message string) int {
	if hresult != "" {
		s.fail(ticket, fmt.Sprintf("request processor error %s: %s", hresult, message))
		return -101
	}
	r, err := ParseResponse(response)
	if err != nil {
		s.fail(ticket, err.Error())
		return -102
	}
	for _, st := range r.Statuses {
		if !st.OK() {
			s.fail(ticket, st.Error())
			return -103
		}
	}
	if len(r.Customers) > 0 && s.OnCustomers != nil {
		s.OnCustomers(r.Customers)
	}
	for _, inv := range r.Invoices {
		if s.OnInvoiceAdded != nil {
			s.OnInvoiceAdded(inv)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[ticket]
	if sess == nil {
		return 100
	}
	sess.next++
	if sess.next >= len(sess.steps) {
		return 100
	}
	pct := sess.next * 100 / len(sess.steps)
	if pct < 1 {
		pct = 1
	}
	return pct
}

func (s *Service) fail(ticket, msg string) {
	log.Printf("qbwc: %s", msg)
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess := s.sessions[ticket]; sess != nil {
		sess.lastError = msg
	}
}

func (s *Service) lastError(ticket string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess := s.sessions[ticket]; sess != nil && sess.lastError != "" {
		return sess.lastError
	}
	return "unknown error"
}

func newTicket() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// parseCall reads a SOAP 1.1 request: the first element of the Body is the
// method, its children are the parameters.
func parseCall(raw []byte) (string, map[string]string, error) {
	dec := xml.NewDecoder(bytes.NewReader(raw))
	inBody := false
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", nil, fmt.Errorf("no SOAP body: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local == "Body" {
			inBody = true
			continue
		}
		if !inBody {
			continue
		}
		var call struct {
			Params []struct {
				XMLName xml.Name
				Value   string `xml:",chardata"`
			} `xml:",any"`
		}
		if err := dec.DecodeElement(&call, &se); err != nil {
			return "", nil, err
		}
		params := map[string]string{}
		for _, p := range call.Params {
			params[p.XMLName.Local] = p.Value
		}
		return se.Name.Local, params, nil
	}
}

func soapResponse(method string, result any) []byte {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>`)
	b.WriteString(`<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body>`)
	fmt.Fprintf(&b, `<%sResponse xmlns="%s"><%sResult>`, method, intuitNS, method)
	switch v := result.(type) {
	case []string:
		for _, s := range v {
			b.WriteString("<string>")
			_ = xml.EscapeText(&b, []byte(s))
			b.WriteString("</string>")
		}
	case string:
		_ = xml.EscapeText(&b, []byte(v))
	}
	fmt.Fprintf(&b, `</%sResult></%sResponse>`, method, method)
	b.WriteString(`</soap:Body></soap:Envelope>`)
	return b.Bytes()
}

// QWC returns the .qwc file a QuickBooks user adds to the Web Connector.
// AppURL must be https, except for localhost during development.
func QWC(appName, appURL, user, ownerID, fileID string, everyMinutes int) string {
	esc := func(s string) string {
		var b bytes.Buffer
		_ = xml.EscapeText(&b, []byte(s))
		return b.String()
	}
	return fmt.Sprintf(`<?xml version="1.0"?>
<QBWCXML>
  <AppName>%s</AppName>
  <AppID></AppID>
  <AppURL>%s</AppURL>
  <AppDescription>Syncs customers and writes approved invoices</AppDescription>
  <AppSupport>%s</AppSupport>
  <UserName>%s</UserName>
  <OwnerID>{%s}</OwnerID>
  <FileID>{%s}</FileID>
  <QBType>QBFS</QBType>
  <Scheduler>
    <RunEveryNMinutes>%d</RunEveryNMinutes>
  </Scheduler>
</QBWCXML>
`, esc(appName), esc(appURL), esc(appURL), esc(user), ownerID, fileID, everyMinutes)
}
