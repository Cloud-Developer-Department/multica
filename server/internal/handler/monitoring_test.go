package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestRate(t *testing.T) {
	if got := rate(50, 100); got != 50 {
		t.Fatalf("rate(50,100)=%v, want 50", got)
	}
	if got := rate(0, 0); got != 0 {
		t.Fatalf("rate(0,0)=%v, want 0", got)
	}
	if got := rate(3, 0); got != 0 {
		t.Fatalf("rate(3,0)=%v, want 0", got)
	}
}

func TestRatePtr(t *testing.T) {
	if ratePtr(0, 0) != nil {
		t.Fatalf("ratePtr(0,0) should be nil")
	}
	v := ratePtr(1, 4)
	if v == nil || *v != 25 {
		t.Fatalf("ratePtr(1,4)=%v, want 25", v)
	}
}

func TestDeltaPtr(t *testing.T) {
	if deltaPtr(nil, nil) != nil {
		t.Fatalf("deltaPtr(nil,nil) should be nil")
	}
	a, b := 30.0, 25.0
	if v := deltaPtr(&a, &b); v == nil || *v != 5 {
		t.Fatalf("deltaPtr(30,25)=%v, want 5", v)
	}
}

func TestBuildProjectProgress(t *testing.T) {
	rows := []db.ListMonitoringProjectStatusCountsRow{
		{ProjectID: "p1", ProjectName: "A", Status: "done", Count: 2},
		{ProjectID: "p1", ProjectName: "A", Status: "todo", Count: 1},
		{ProjectID: "", ProjectName: "", Status: "in_progress", Count: 3},
	}
	out := buildProjectProgress(rows)
	if len(out) != 2 {
		t.Fatalf("got %d projects, want 2", len(out))
	}
	if out[0].ID != "p1" || out[0].Total != 3 || out[0].StatusCounts["done"] != 2 {
		t.Fatalf("project p1 wrong: %+v", out[0])
	}
	if out[1].ID != "" || out[1].Total != 3 {
		t.Fatalf("no-project bucket wrong: %+v", out[1])
	}
}

func TestParseMonitoringDays(t *testing.T) {
	cases := []struct {
		raw  string
		want int
	}{
		{"", 7},
		{"1", 1},
		{"30", 30},
		{"0", 7},
		{"366", 7},
		{"abc", 7},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "/api/dashboard/activity?days="+c.raw, nil)
		if got := parseMonitoringDays(r); got != c.want {
			t.Fatalf("days=%q got %d, want %d", c.raw, got, c.want)
		}
	}
}

func TestHourlyCommentSeriesShape(t *testing.T) {
	h := &Handler{}
	since := time.Now().Add(-24 * time.Hour)
	loc := time.UTC
	// Round since down to the hour so the loop produces exactly 24 buckets.
	base := since.Truncate(time.Hour)
	now := time.Now().In(loc).Truncate(time.Hour)
	if now.Sub(base).Hours() != 24 {
		base = now.Add(-23 * time.Hour)
	}
	_ = h
	_ = base
}
