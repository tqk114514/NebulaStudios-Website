package utils

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
)

func TestTruncateIdentifier(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "(empty)"},
		{"abc", "abc"},
		{"abcdefgh", "abcdefgh"},
		{"abcdefghijklmno", "abcdefgh***"}, // 保留前 8 位
	}
	for _, tt := range tests {
		got := TruncateIdentifier(tt.input)
		if got != tt.want {
			t.Errorf("TruncateIdentifier(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestHandleDatabaseErrorNotFound(t *testing.T) {
	err := HandleDatabaseError("TEST", "Find", sql.ErrNoRows, "id-1")
	if err == nil {
		t.Fatal("HandleDatabaseError should return error")
	}
	var dbErr *DatabaseError
	if !errors.As(err, &dbErr) {
		t.Fatalf("expected *DatabaseError, got %T", err)
	}
	if !dbErr.NotFound {
		t.Error("sql.ErrNoRows should map to NotFound=true")
	}
	if !IsDatabaseNotFound(err) {
		t.Error("IsDatabaseNotFound should be true")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error message %q should contain 'not found'", err.Error())
	}
}

func TestHandleDatabaseErrorOther(t *testing.T) {
	genericErr := errors.New("connection refused")
	err := HandleDatabaseError("TEST", "Create", genericErr, "id-1")
	var dbErr *DatabaseError
	if !errors.As(err, &dbErr) {
		t.Fatalf("expected *DatabaseError, got %T", err)
	}
	if dbErr.NotFound {
		t.Error("generic error should map to NotFound=false")
	}
	if IsDatabaseNotFound(err) {
		t.Error("IsDatabaseNotFound should be false")
	}
	if !errors.Is(err, genericErr) {
		t.Error("Unwrap should preserve original error")
	}
}

func TestHandleDatabaseErrorNil(t *testing.T) {
	if err := HandleDatabaseError("TEST", "Op", nil, "id-1"); err != nil {
		t.Errorf("nil error should return nil, got %v", err)
	}
}

func TestLogError(t *testing.T) {
	if err := LogError("TEST", "Op", nil); err != nil {
		t.Errorf("nil error should return nil, got %v", err)
	}

	orig := errors.New("boom")
	err := LogError("TEST", "DoThing", orig)
	if err == nil {
		t.Fatal("LogError with non-nil error should return wrapped error")
	}
	if !errors.Is(err, orig) {
		t.Error("wrapped error should unwrap to original")
	}
	if !strings.Contains(err.Error(), "DoThing failed") {
		t.Errorf("error %q should contain operation name", err.Error())
	}
}

func TestDatabaseErrorString(t *testing.T) {
	nf := &DatabaseError{Operation: "Find", Err: sql.ErrNoRows, NotFound: true}
	if nf.Error() != "Find: not found" {
		t.Errorf("not-found message = %q, want %q", nf.Error(), "Find: not found")
	}
	other := &DatabaseError{Operation: "Create", Err: errors.New("boom"), NotFound: false}
	if other.Error() != "Create failed: boom" {
		t.Errorf("generic message = %q", other.Error())
	}
}
