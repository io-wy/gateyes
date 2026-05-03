package db

import "testing"

func TestRebind_MySQL(t *testing.T) {
	db := &DB{driver: "mysql"}
	query := "SELECT * FROM users WHERE id = ? AND name = ?"
	got := db.Rebind(query)
	if got != query {
		t.Errorf("Rebind(mysql) = %q, want %q", got, query)
	}
}

func TestRebind_EdgeCases(t *testing.T) {
	pg := &DB{driver: "postgres"}

	// zero args
	if got, want := pg.Rebind("SELECT 1"), "SELECT 1"; got != want {
		t.Errorf("Rebind(no args) = %q, want %q", got, want)
	}

	// mixed placeholders with literal question marks in string
	if got, want := pg.Rebind("SELECT ? FROM t WHERE a = ? AND b = '?'"), "SELECT $1 FROM t WHERE a = $2 AND b = '$3'"; got != want {
		t.Errorf("Rebind(mixed) = %q, want %q", got, want)
	}
}
