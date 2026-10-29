package store

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// The few places where PostgreSQL and SQLite differ. PostgreSQL keeps its
// native text[], jsonb and timestamptz; SQLite stores the same values as
// JSON text and UTC time text, which sort the same way.

var pgTypes = pgtype.NewMap()

func (s *Store) now() string {
	if s.sqlite {
		return `strftime('%Y-%m-%d %H:%M:%f', 'now')`
	}
	return `now()`
}

func (s *Store) timeType() string {
	if s.sqlite {
		return `text`
	}
	return `timestamptz`
}

// list is a []string query argument for a text[] column.
func (s *Store) list(v []string) any {
	if v == nil {
		v = []string{}
	}
	if s.sqlite {
		raw, _ := json.Marshal(v)
		return string(raw)
	}
	return v
}

// listInto scans a text[] column.
func (s *Store) listInto(dst *[]string) any {
	if s.sqlite {
		return jsonList{dst}
	}
	return pgTypes.SQLScanner(dst)
}

// object is a JSON document query argument for a jsonb column.
func (s *Store) object(raw []byte) any {
	if s.sqlite {
		return string(raw)
	}
	return raw
}

// in is "IN (...)" for a list of ids, written the way each database likes.
func (s *Store) in(ids []int64) (string, []any) {
	if !s.sqlite {
		return "= ANY($1)", []any{ids}
	}
	marks := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		marks[i], args[i] = "$"+strconv.Itoa(i+1), id
	}
	return "IN (" + strings.Join(marks, ", ") + ")", args
}

type jsonList struct{ dst *[]string }

func (j jsonList) Scan(v any) error {
	var raw []byte
	switch x := v.(type) {
	case string:
		raw = []byte(x)
	case []byte:
		raw = x
	case nil:
		*j.dst = []string{}
		return nil
	default:
		return fmt.Errorf("store: cannot read a list from %T", v)
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	if out == nil {
		out = []string{}
	}
	*j.dst = out
	return nil
}

// timeValue reads a timestamptz, or SQLite's UTC time text.
type timeValue struct{ time.Time }

var _ sql.Scanner = (*timeValue)(nil)

func (t *timeValue) Scan(v any) error {
	switch x := v.(type) {
	case time.Time:
		t.Time = x
		return nil
	case string:
		return t.parse(x)
	case []byte:
		return t.parse(string(x))
	case nil:
		t.Time = time.Time{}
		return nil
	}
	return fmt.Errorf("store: cannot read a time from %T", v)
}

func (t *timeValue) parse(text string) error {
	for _, layout := range []string{"2006-01-02 15:04:05.999999999", time.RFC3339Nano} {
		if at, err := time.ParseInLocation(layout, text, time.UTC); err == nil {
			t.Time = at
			return nil
		}
	}
	return fmt.Errorf("store: unknown time format %q", text)
}

var _ driver.Valuer = timeValue{}

func (t timeValue) Value() (driver.Value, error) { return t.Time, nil }
