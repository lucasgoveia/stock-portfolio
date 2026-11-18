package marketdata

import (
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T, parts ...string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(append([]string{"testdata"}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func dec(t *testing.T, got interface{ String() string }, want string) {
	t.Helper()
	if got.String() != want {
		t.Errorf("got %s, want %s", got.String(), want)
	}
}
