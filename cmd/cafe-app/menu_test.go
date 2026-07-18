package main

import "testing"

func TestComputeMenu_SortsAndSkipsUndecodable(t *testing.T) {
	keys, get := fakeKV(map[string]any{
		"cafe-menu-catalog.b": map[string]any{"menuItemKey": "vtx.menuitem.b", "name": "Latte", "priceCents": 450},
		"cafe-menu-catalog.a": map[string]any{"menuItemKey": "vtx.menuitem.a", "name": "Croissant", "priceCents": 350},
		"cafe-menu-catalog.n": map[string]any{}, // undecodable (no menuItemKey) — skipped
	})
	rows := computeMenu(keys, get)
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d (%+v)", len(rows), rows)
	}
	if rows[0].Name != "Croissant" || rows[1].Name != "Latte" {
		t.Errorf("want sorted by name (Croissant, Latte), got (%s, %s)", rows[0].Name, rows[1].Name)
	}
	if rows[0].MenuItemKey != "vtx.menuitem.a" || rows[0].PriceCents != 350 {
		t.Errorf("unexpected row content: %+v", rows[0])
	}
	if rows[1].MenuItemKey != "vtx.menuitem.b" || rows[1].PriceCents != 450 {
		t.Errorf("unexpected row content: %+v", rows[1])
	}
}
