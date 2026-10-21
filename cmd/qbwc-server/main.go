// qbwc-server is the endpoint the QuickBooks Web Connector polls on a
// Windows machine running QuickBooks Desktop. It reads customers and writes
// the invoices waiting in an outbox file (JSON).
package main

import (
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/rflkt/quickbooks-bridge/internal/qbwc"
)

func main() {
	addr := flag.String("addr", "localhost:8095", "listen address")
	outbox := flag.String("outbox", "outbox.json", "JSON file of orders to write as invoices")
	qwcOut := flag.String("qwc", "", "write a .qwc file for this URL (e.g. https://bridge.example.com/qbwc) and exit")
	flag.Parse()

	user := envOr("QBWC_USER", "bridge")
	password := os.Getenv("QBWC_PASSWORD")

	if *qwcOut != "" {
		fmt.Print(qbwc.QWC("quickbooks-bridge", *qwcOut, user, guid(), guid(), 15))
		return
	}
	if password == "" {
		log.Fatal("QBWC_PASSWORD must be set: the user types the same password into the Web Connector")
	}

	svc := &qbwc.Service{
		User: user, Password: password,
		Outbox: func() []qbwc.Order {
			var orders []qbwc.Order
			b, err := os.ReadFile(*outbox)
			if err != nil {
				return nil
			}
			if err := json.Unmarshal(b, &orders); err != nil {
				log.Printf("outbox: %v", err)
				return nil
			}
			return orders
		},
		OnCustomers: func(cs []qbwc.CustomerRet) {
			log.Printf("read %d customers from QuickBooks", len(cs))
		},
		OnInvoiceAdded: func(inv qbwc.InvoiceRet) {
			log.Printf("invoice %s created (TxnID %s, subtotal %s)", inv.RefNumber, inv.TxnID, inv.Subtotal)
		},
	}
	http.Handle("/qbwc", svc)
	log.Printf("Web Connector endpoint on http://%s/qbwc", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func guid() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return strings.ToUpper(fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]))
}
