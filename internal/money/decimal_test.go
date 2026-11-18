package money_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
)

func TestSQLiteRoundTrip(t *testing.T) {
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Exec(`CREATE TABLE d (id INTEGER PRIMARY KEY, v TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for i, s := range []string{"0.1", "12345.678901", "-3"} {
		in, err := money.NewFromString(s)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(`INSERT INTO d (id, v) VALUES (?, ?)`, i, in); err != nil {
			t.Fatal(err)
		}
		var out money.Decimal
		if err := conn.QueryRow(`SELECT v FROM d WHERE id = ?`, i).Scan(&out); err != nil {
			t.Fatal(err)
		}
		if out.String() != s {
			t.Errorf("round trip %q -> %q", s, out.String())
		}
		var raw string
		_ = conn.QueryRow(`SELECT v FROM d WHERE id = ?`, i).Scan(&raw)
		if raw != s {
			t.Errorf("stored %q, want %q", raw, s)
		}
	}
}

func TestJSON(t *testing.T) {
	in, _ := money.NewFromString("12345.678901")
	b, err := json.Marshal(in)
	if err != nil || string(b) != `"12345.678901"` {
		t.Fatalf("%s %v", b, err)
	}
	var out money.Decimal
	if err := json.Unmarshal(b, &out); err != nil || !out.Equal(in.Decimal) {
		t.Fatalf("%v %v", out, err)
	}
}
