package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	loftspaceledger "github.com/asolgan/lattice/packages/loftspace-ledger"
)

// ledgerEntryProjection is one row of the loftspace-ledger `ledgerHistory` lens,
// read from its NATS-KV read-model bucket (P5 — never Core KV).
type ledgerEntryProjection struct {
	TransactionKey string   `json:"transactionKey"`
	AccountKey     string   `json:"accountKey"`
	LeaseAppKey    string   `json:"leaseAppKey"`
	Type           string   `json:"type"`
	AmountCents    *float64 `json:"amountCents"`
	Memo           string   `json:"memo"`
	PostedAt       string   `json:"postedAt"`
}

// ledgerEntryRow is the payment-history row the FE renders.
type ledgerEntryRow struct {
	TransactionKey string `json:"transactionKey"`
	Type           string `json:"type"`
	AmountCents    int64  `json:"amountCents"`
	Memo           string `json:"memo,omitempty"`
	PostedAt       string `json:"postedAt"`
}

// computeLedgerHistory filters the ledgerHistory lens rows to one lease, sorts
// them chronologically, and derives the running balance in cents (sum debits −
// sum credits) — the ledger itself stores no running total (append-only, D5),
// so the FE-facing balance is always assembled from the full transaction set. A
// row that fails to decode or carries no transactionKey (a tombstoned
// projection entry) is skipped.
func computeLedgerHistory(keys []string, get kvGetter, leaseAppKey string) ([]ledgerEntryRow, int64) {
	rows := make([]ledgerEntryRow, 0)
	for _, k := range keys {
		raw, ok := get(k)
		if !ok {
			continue
		}
		var p ledgerEntryProjection
		if json.Unmarshal(raw, &p) != nil || p.TransactionKey == "" {
			continue
		}
		if p.LeaseAppKey != leaseAppKey {
			continue
		}
		var amount int64
		if p.AmountCents != nil {
			amount = int64(*p.AmountCents)
		}
		rows = append(rows, ledgerEntryRow{
			TransactionKey: p.TransactionKey,
			Type:           p.Type,
			AmountCents:    amount,
			Memo:           p.Memo,
			PostedAt:       p.PostedAt,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].PostedAt != rows[j].PostedAt {
			return rows[i].PostedAt < rows[j].PostedAt
		}
		return rows[i].TransactionKey < rows[j].TransactionKey
	})
	var balance int64
	for _, r := range rows {
		switch r.Type {
		case "debit":
			balance += r.AmountCents
		case "credit":
			balance -= r.AmountCents
		}
	}
	return rows, balance
}

// deriveAccountKey computes a lease's ledger account key without a read:
// CreateAccount mints the account under the SAME bare NanoID as the lease
// (packages/loftspace-ledger/scripts.go), so the FE can address the account —
// to post its first charge — before the account necessarily exists yet.
// Returns "" for a key that isn't a vtx.leaseapp.<NanoID>.
func deriveAccountKey(leaseAppKey string) string {
	const prefix = "vtx.leaseapp."
	if !strings.HasPrefix(leaseAppKey, prefix) || leaseAppKey == prefix {
		return ""
	}
	return "vtx.account." + strings.TrimPrefix(leaseAppKey, prefix)
}

// handleLedger implements GET /api/ledger?leaseAppKey= — the payment-history
// view, served from the `ledgerHistory` lens read model (NOT Core KV, P5). It
// returns the lease's transaction rows, the running balance, and the
// (derived, possibly not-yet-created) ledger account key the FE needs to post
// a new charge or payment.
func (s *server) handleLedger(w http.ResponseWriter, r *http.Request) {
	conn, ok := s.requireConn(w)
	if !ok {
		return
	}
	leaseAppKey := strings.TrimSpace(r.URL.Query().Get("leaseAppKey"))
	if leaseAppKey == "" {
		s.writeError(w, http.StatusBadRequest, "leaseAppKey query param is required")
		return
	}
	accountKey := deriveAccountKey(leaseAppKey)
	if accountKey == "" {
		s.writeError(w, http.StatusBadRequest, "leaseAppKey must be a vtx.leaseapp.<NanoID> key")
		return
	}

	ctx, cancel := s.reqContext(r)
	defer cancel()

	bucket := loftspaceledger.LedgerHistoryBucket
	keys, err := conn.KVListKeys(ctx, bucket)
	if err != nil {
		s.writeError(w, http.StatusBadGateway,
			"list "+bucket+": "+err.Error()+" (is loftspace-ledger installed and the Refractor projecting?)")
		return
	}
	get := func(key string) ([]byte, bool) {
		entry, err := conn.KVGet(ctx, bucket, key)
		if err != nil {
			return nil, false
		}
		return entry.Value, true
	}
	rows, balance := computeLedgerHistory(keys, get, leaseAppKey)
	s.writeJSON(w, http.StatusOK, map[string]any{
		"leaseAppKey":  leaseAppKey,
		"accountKey":   accountKey,
		"transactions": rows,
		"balanceCents": balance,
	})
}
