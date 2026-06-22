package server

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseDateRange_Valid(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader("start_date=2026-07-01&end_date=2026-07-14"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	start, end, err := parseDateRange(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if start.Format("2006-01-02") != "2026-07-01" {
		t.Errorf("start: want 2026-07-01, got %s", start.Format("2006-01-02"))
	}
	if end.Format("2006-01-02") != "2026-07-14" {
		t.Errorf("end: want 2026-07-14, got %s", end.Format("2006-01-02"))
	}
}

func TestParseDateRange_SingleDay(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader("start_date=2026-07-01&end_date=2026-07-01"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	start, end, err := parseDateRange(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !start.Equal(end) {
		t.Errorf("single-day: start and end should be equal")
	}
}

func TestParseDateRange_InvalidStart(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader("start_date=notadate&end_date=2026-07-14"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, _, err := parseDateRange(req)
	if err == nil {
		t.Fatal("expected error for invalid start date")
	}
}

func TestParseDateRange_InvalidEnd(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader("start_date=2026-07-01&end_date=notadate"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, _, err := parseDateRange(req)
	if err == nil {
		t.Fatal("expected error for invalid end date")
	}
}

func TestParseDateRange_EndBeforeStart(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader("start_date=2026-07-14&end_date=2026-07-01"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, _, err := parseDateRange(req)
	if err == nil {
		t.Fatal("expected error when end < start")
	}
}

func TestParseDateRange_MissingStart(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader("end_date=2026-07-14"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, _, err := parseDateRange(req)
	if err == nil {
		t.Fatal("expected error when start_date is missing")
	}
}

func TestParseDateRange_MalformedBody(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader("%invalid%body"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, _, err := parseDateRange(req)
	if err == nil {
		t.Fatal("expected error for malformed body")
	}
}
