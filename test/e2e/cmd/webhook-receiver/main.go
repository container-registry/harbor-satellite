// Command webhook-receiver is the E2E receiver of Ground Control webhooks.
// It answers POST /hook with 204, verifies the HMAC signature with
// WEBHOOK_SECRET and records every delivery, duplicates included, so the
// check can count deliveries per event ID. GET /deliveries returns them.
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

type delivery struct {
	ID             string    `json:"id"`
	Type           string    `json:"type"`
	Subject        string    `json:"subject"`
	IdempotencyKey string    `json:"idempotency_key"`
	SignatureValid bool      `json:"signature_valid"`
	ReceivedAt     time.Time `json:"received_at"`
}

type receiver struct {
	secret []byte
	mu     sync.Mutex
	seen   []delivery
}

func main() {
	addr := flag.String("addr", "127.0.0.1:9999", "listen address")
	flag.Parse()
	r := &receiver{secret: []byte(os.Getenv("WEBHOOK_SECRET"))}
	if len(r.secret) == 0 {
		log.Fatal("WEBHOOK_SECRET is required")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /hook", r.hook)
	mux.HandleFunc("GET /deliveries", r.deliveries)
	srv := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	log.Printf("webhook receiver listening on %s", *addr)
	log.Fatal(srv.ListenAndServe())
}

func (r *receiver) hook(w http.ResponseWriter, req *http.Request) {
	body, err := io.ReadAll(io.LimitReader(req.Body, 1<<20))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var event struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Subject string `json:"subject"`
	}
	if err := json.Unmarshal(body, &event); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	d := delivery{
		ID: event.ID, Type: event.Type, Subject: event.Subject,
		IdempotencyKey: req.Header.Get("Idempotency-Key"),
		SignatureValid: r.valid(req.Header.Get("X-Ground-Control-Timestamp"), req.Header.Get("X-Ground-Control-Signature-256"), body),
		ReceivedAt:     time.Now().UTC(),
	}
	r.mu.Lock()
	r.seen = append(r.seen, d)
	r.mu.Unlock()
	log.Printf("delivery id=%q type=%q subject=%q signature_valid=%t", d.ID, d.Type, d.Subject, d.SignatureValid) //nolint:gosec // %q escapes the received fields
	w.WriteHeader(http.StatusNoContent)
}

// valid recomputes "sha256=" + HMAC-SHA256(timestamp + "." + body).
func (r *receiver) valid(timestamp, signature string, body []byte) bool {
	if _, err := strconv.ParseInt(timestamp, 10, 64); err != nil {
		return false
	}
	mac := hmac.New(sha256.New, r.secret)
	mac.Write([]byte(timestamp + "."))
	mac.Write(body)
	return hmac.Equal([]byte(signature), []byte("sha256="+hex.EncodeToString(mac.Sum(nil))))
}

func (r *receiver) deliveries(w http.ResponseWriter, _ *http.Request) {
	r.mu.Lock()
	out := append([]delivery(nil), r.seen...)
	r.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		log.Printf("encode deliveries: %v", err)
	}
}
