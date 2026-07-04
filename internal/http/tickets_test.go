package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/JustARandomBadDev/captive-portal-admin/internal/tickets"
)

func TestParseTicketListFilters(t *testing.T) {
	req := httptest.NewRequest("GET", "/tickets?search=%20A01%20&status=active&duration=7d&created=today", nil)

	filters := parseTicketListFilters(req)

	if filters.Search != "A01" || filters.Filters.Search != "A01" {
		t.Fatalf("search = %q / %q, want A01", filters.Search, filters.Filters.Search)
	}
	if filters.Status != "active" || filters.Filters.Status != tickets.TicketStatusActive {
		t.Fatalf("status = %q / %q, want active", filters.Status, filters.Filters.Status)
	}
	if filters.Duration != "7d" || filters.Filters.Duration != 7*24*time.Hour {
		t.Fatalf("duration = %q / %s, want 7d", filters.Duration, filters.Filters.Duration)
	}
	if filters.Created != "today" || filters.Filters.CreatedSince == nil {
		t.Fatalf("created = %q / %v, want today with CreatedSince", filters.Created, filters.Filters.CreatedSince)
	}
}

func TestParseTicketListFiltersIgnoresUnknownValues(t *testing.T) {
	req := httptest.NewRequest("GET", "/tickets?status=bad&duration=bad&created=bad", nil)

	filters := parseTicketListFilters(req)

	if filters.Status != "" || filters.Filters.Status != "" {
		t.Fatalf("status = %q / %q, want empty", filters.Status, filters.Filters.Status)
	}
	if filters.Duration != "" || filters.Filters.Duration != 0 {
		t.Fatalf("duration = %q / %s, want empty", filters.Duration, filters.Filters.Duration)
	}
	if filters.Created != "" || filters.Filters.CreatedSince != nil {
		t.Fatalf("created = %q / %v, want empty", filters.Created, filters.Filters.CreatedSince)
	}
}

func TestSelectTicketRowsByIDKeepsActiveRowsOrder(t *testing.T) {
	rows := []ticketRow{
		{ID: "ticket-1", Username: "first"},
		{ID: "ticket-2", Username: "second"},
		{ID: "ticket-3", Username: "third"},
	}

	selected := selectTicketRowsByID(rows, []string{"ticket-3", "unknown", "ticket-1"})

	if len(selected) != 2 {
		t.Fatalf("len(selected) = %d, want 2", len(selected))
	}
	if selected[0].ID != "ticket-1" || selected[1].ID != "ticket-3" {
		t.Fatalf("selected IDs = %q, %q, want ticket-1, ticket-3", selected[0].ID, selected[1].ID)
	}
}

func TestParseTicketDurationPreset(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/tickets", strings.NewReader("duration_hours=24"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := req.ParseForm(); err != nil {
		t.Fatalf("ParseForm: %v", err)
	}

	duration, custom, useCustom, err := parseTicketDuration(req)
	if err != nil {
		t.Fatalf("parseTicketDuration() error = %v", err)
	}
	if duration != 24 || custom != 0 || useCustom {
		t.Fatalf("duration/custom/useCustom = %d/%d/%v, want 24/0/false", duration, custom, useCustom)
	}
}

func TestParseTicketDurationCustom(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/tickets", strings.NewReader("duration_hours=custom&custom_duration_days=5"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := req.ParseForm(); err != nil {
		t.Fatalf("ParseForm: %v", err)
	}

	duration, custom, useCustom, err := parseTicketDuration(req)
	if err != nil {
		t.Fatalf("parseTicketDuration() error = %v", err)
	}
	if duration != 120 || custom != 5 || !useCustom {
		t.Fatalf("duration/custom/useCustom = %d/%d/%v, want 120/5/true", duration, custom, useCustom)
	}
}

func TestParseTicketDurationRejectsInvalidCustom(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/tickets", strings.NewReader("duration_hours=custom&custom_duration_days=0"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := req.ParseForm(); err != nil {
		t.Fatalf("ParseForm: %v", err)
	}

	_, _, useCustom, err := parseTicketDuration(req)
	if err == nil {
		t.Fatal("parseTicketDuration() error = nil, want error")
	}
	if !useCustom {
		t.Fatal("useCustom = false, want true")
	}
}
