package services

import (
	"strings"
	"testing"
	"time"
)

func TestFormatCreatedAtBoundaryUsesUTC(t *testing.T) {
	oldLocal := time.Local
	time.Local = time.FixedZone("UTC+8", 8*60*60)
	defer func() {
		time.Local = oldLocal
	}()

	localTime := time.Date(2026, 4, 9, 0, 37, 50, 0, time.Local)
	if got := formatCreatedAtBoundary(localTime); got != "2026-04-08 16:37:50" {
		t.Fatalf("expected UTC boundary 2026-04-08 16:37:50, got %s", got)
	}
}

func TestDayFromTimestampUsesLocalDate(t *testing.T) {
	oldLocal := time.Local
	time.Local = time.FixedZone("UTC+8", 8*60*60)
	defer func() {
		time.Local = oldLocal
	}()

	if got := dayFromTimestamp("2026-04-08 16:37:50"); got != "2026-04-09" {
		t.Fatalf("expected local day 2026-04-09, got %s", got)
	}
}

func TestDashboardBucketExprUsesBeijingOffset(t *testing.T) {
	if !strings.Contains(bucketExpr(seriesBucketHour), "+8 hours") {
		t.Fatalf("expected hour bucket expression to use +8 hours, got %s", bucketExpr(seriesBucketHour))
	}
	if !strings.Contains(bucketExpr(seriesBucketDay), "+8 hours") {
		t.Fatalf("expected day bucket expression to use +8 hours, got %s", bucketExpr(seriesBucketDay))
	}
}
