package handler

import (
	"fmt"
	"strings"
	"time"
)

// jsonTime is a time.Time that accepts multiple JSON formats
type jsonTime struct {
	time.Time
}

func (t *jsonTime) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	if s == "null" || s == "" {
		return nil
	}

	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
	}

	for _, format := range formats {
		if parsed, err := time.Parse(format, s); err == nil {
			t.Time = parsed
			return nil
		}
	}

	return fmt.Errorf("cannot parse %q as a time", s)
}
