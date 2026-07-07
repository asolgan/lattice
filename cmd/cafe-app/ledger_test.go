package main

import "testing"

func TestComputeLedgerHistory_FiltersSumsAndOrders(t *testing.T) {
	keys, get := fakeKV(map[string]any{
		"vtx.cafetransaction.1": map[string]any{"transactionKey": "vtx.cafetransaction.1", "accountKey": "vtx.cafeaccount.aaa", "leaseAppKey": "vtx.leaseapp.aaa", "type": "debit", "amountCents": 1200, "memo": "House tab", "postedAt": "2026-07-06T00:00:00Z"},
		"vtx.cafetransaction.2": map[string]any{"transactionKey": "vtx.cafetransaction.2", "accountKey": "vtx.cafeaccount.aaa", "leaseAppKey": "vtx.leaseapp.aaa", "type": "credit", "amountCents": 500, "memo": "Payment", "postedAt": "2026-07-07T00:00:00Z"},
		// a different lease's transaction — must not leak into this lease's rows/balance
		"vtx.cafetransaction.3": map[string]any{"transactionKey": "vtx.cafetransaction.3", "accountKey": "vtx.cafeaccount.other", "leaseAppKey": "vtx.leaseapp.other", "type": "debit", "amountCents": 99999, "postedAt": "2026-07-06T00:00:00Z"},
		// a tombstoned / undecodable projection entry — skipped
		"vtx.cafetransaction.4": map[string]any{},
	})

	rows, balance := computeLedgerHistory(keys, get, "vtx.leaseapp.aaa")
	if len(rows) != 2 {
		t.Fatalf("want 2 rows for the lease, got %d (%+v)", len(rows), rows)
	}
	if rows[0].TransactionKey != "vtx.cafetransaction.1" || rows[1].TransactionKey != "vtx.cafetransaction.2" {
		t.Errorf("want chronological order (1, 2), got (%s, %s)", rows[0].TransactionKey, rows[1].TransactionKey)
	}
	if balance != 700 {
		t.Errorf("balance: want 1200-500=700, got %d", balance)
	}
}

func TestComputeLedgerHistory_NoTransactionsZeroBalance(t *testing.T) {
	rows, balance := computeLedgerHistory(nil, func(string) ([]byte, bool) { return nil, false }, "vtx.leaseapp.fresh")
	if len(rows) != 0 || balance != 0 {
		t.Errorf("want no rows / zero balance, got %d rows, balance=%d", len(rows), balance)
	}
}

func TestResolveLeaseAccount_FindsMatchOrEmpty(t *testing.T) {
	keys, get := fakeKV(map[string]any{
		"vtx.leaseapp.aaa":   map[string]any{"leaseAppKey": "vtx.leaseapp.aaa", "accountKey": "vtx.cafeaccount.xyz"},
		"vtx.leaseapp.other": map[string]any{"leaseAppKey": "vtx.leaseapp.other", "accountKey": ""},
		"vtx.leaseapp.bad":   map[string]any{},
	})

	if got := resolveLeaseAccount(keys, get, "vtx.leaseapp.aaa"); got != "vtx.cafeaccount.xyz" {
		t.Errorf("resolveLeaseAccount(aaa) = %q, want vtx.cafeaccount.xyz", got)
	}
	if got := resolveLeaseAccount(keys, get, "vtx.leaseapp.other"); got != "" {
		t.Errorf("resolveLeaseAccount(other) = %q, want empty (no account opened yet)", got)
	}
	if got := resolveLeaseAccount(keys, get, "vtx.leaseapp.unprojected"); got != "" {
		t.Errorf("resolveLeaseAccount(unprojected) = %q, want empty (no row at all)", got)
	}
}
