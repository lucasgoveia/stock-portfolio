// Package money provides decimal helpers stored as TEXT in SQLite.
package money

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"

	"github.com/shopspring/decimal"
)

// Decimal wraps decimal.Decimal; it is stored as TEXT in SQL and encoded
// as a string in JSON.
type Decimal struct {
	decimal.Decimal
}

// New wraps d.
func New(d decimal.Decimal) Decimal { return Decimal{d} }

// NewFromString parses s.
func NewFromString(s string) (Decimal, error) {
	d, err := decimal.NewFromString(s)
	return Decimal{d}, err
}

// Value implements driver.Valuer, returning the decimal as a string.
func (d Decimal) Value() (driver.Value, error) { return d.String(), nil }

// Scan implements sql.Scanner.
func (d *Decimal) Scan(src any) error {
	switch v := src.(type) {
	case string:
		return d.set(v)
	case []byte:
		return d.set(string(v))
	case nil:
		return fmt.Errorf("money: cannot scan NULL into Decimal")
	default:
		return fmt.Errorf("money: cannot scan %T into Decimal", src)
	}
}

func (d *Decimal) set(s string) error {
	p, err := decimal.NewFromString(s)
	if err != nil {
		return fmt.Errorf("money: %w", err)
	}
	d.Decimal = p
	return nil
}

// MarshalJSON encodes the decimal as a JSON string.
func (d Decimal) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

// UnmarshalJSON decodes a JSON string.
func (d *Decimal) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("money: decimal must be a JSON string: %w", err)
	}
	return d.set(s)
}
