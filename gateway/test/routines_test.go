package test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"
)

func TestScheduleNext(t *testing.T) {
	loc := time.UTC
	fri := time.Date(2026, 10, 23, 10, 0, 0, 0, loc) // Friday 10:00
	cases := []struct {
		spec string
		from time.Time
		want time.Time
	}{
		{"daily@09:00", fri, time.Date(2026, 10, 24, 9, 0, 0, 0, loc)},
		{"daily@11:30", fri, time.Date(2026, 10, 23, 11, 30, 0, 0, loc)},
		{"weekdays@09:00", fri, time.Date(2026, 10, 26, 9, 0, 0, 0, loc)}, // skips the weekend
		{"every:2h", fri, fri.Add(2 * time.Hour)},
	}
	for _, c := range cases {
		sc, err := store.ParseSchedule(c.spec)
		if err != nil {
			t.Fatalf("%s: %v", c.spec, err)
		}
		if got := sc.Next(c.from); !got.Equal(c.want) {
			t.Errorf("%s: got %v want %v", c.spec, got, c.want)
		}
	}
}

func TestParseScheduleRejectsBadInput(t *testing.T) {
	for _, spec := range []string{"", "hourly", "daily@25:00", "daily@9", "every:1m", "every:soon"} {
		if _, err := store.ParseSchedule(spec); err == nil {
			t.Errorf("%q: expected error", spec)
		}
	}
}

func TestFireDueRoutinesCollapsesMissedRuns(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "r.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	start := time.Date(2026, 10, 20, 8, 0, 0, 0, time.Local)
	st.SetClock(func() time.Time { return start })
	if _, err := st.CreateRoutine(ctx, "Morning brief", "Summarise my day", "daily@09:00"); err != nil {
		t.Fatal(err)
	}

	// The PC was asleep for three days: expect one run, not three.
	st.SetClock(func() time.Time { return start.Add(72 * time.Hour) })
	tasks, err := st.FireDueRoutines(ctx, "manager")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Kind != store.KindAssignment || tasks[0].Assignee != "manager" {
		t.Fatalf("want exactly one assignment, got %+v", tasks)
	}
	if again, _ := st.FireDueRoutines(ctx, "manager"); len(again) != 0 {
		t.Fatalf("routine fired twice")
	}
}

func TestDisabledRoutineDoesNotFire(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "r.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	start := time.Date(2026, 10, 20, 8, 0, 0, 0, time.Local)
	st.SetClock(func() time.Time { return start })
	rt, err := st.CreateRoutine(ctx, "Brief", "", "daily@09:00")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetRoutineEnabled(ctx, rt.ID, false); err != nil {
		t.Fatal(err)
	}
	st.SetClock(func() time.Time { return start.Add(48 * time.Hour) })
	if tasks, _ := st.FireDueRoutines(ctx, "manager"); len(tasks) != 0 {
		t.Fatalf("disabled routine fired")
	}
}
