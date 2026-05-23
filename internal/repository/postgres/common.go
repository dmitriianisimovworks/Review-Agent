package postgres

import "errors"

var errNotFound = errors.New("record not found")

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
