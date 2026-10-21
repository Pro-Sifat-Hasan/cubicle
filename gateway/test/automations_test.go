package test

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"
)

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

func TestWeeklyAndOnceSchedules(t *testing.T) {
	loc := time.UTC
	fri := time.Date(2026, 10, 23, 10, 0, 0, 0, loc) // Friday 10:00
	cases := []struct {
		spec string
		want time.Time
	}{
		{"weekly@mon,wed@08:30", time.Date(2026, 10, 26, 8, 30, 0, 0, loc)},
		{"weekly@fri@11:00", time.Date(2026, 10, 23, 11, 0, 0, 0, loc)},
		{"weekly@fri@09:00", time.Date(2026, 10, 30, 9, 0, 0, 0, loc)},
		{"once@2026-10-24T07:15", time.Date(2026, 10, 24, 7, 15, 0, 0, loc)},
		{"once@2026-10-01T07:15", time.Time{}}, // in the past: never again
	}
	for _, c := range cases {
		sc, err := store.ParseSchedule(c.spec)
		if err != nil {
			t.Fatalf("%s: %v", c.spec, err)
		}
		if got := sc.Next(fri); !got.Equal(c.want) {
			t.Errorf("%s: got %v want %v", c.spec, got, c.want)
		}
	}
	for _, bad := range []string{"weekly@@09:00", "weekly@funday@09:00", "once@tomorrow", "daily@9:5"} {
		if _, err := store.ParseSchedule(bad); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
}

func TestOnceAutomationTurnsItselfOff(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "r.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	start := time.Date(2026, 10, 20, 8, 0, 0, 0, time.Local)
	st.SetClock(func() time.Time { return start })
	if _, err := st.CreateRoutine(ctx, store.RoutineInput{Title: "Past", Schedule: "once@2026-10-19T09:00"}); err == nil {
		t.Fatal("a one-off in the past should be rejected")
	}
	rt, err := st.CreateRoutine(ctx, store.RoutineInput{Title: "Call the bank", Schedule: "once@2026-10-20T09:00"})
	if err != nil {
		t.Fatal(err)
	}
	st.SetClock(func() time.Time { return start.Add(2 * time.Hour) })
	if tasks, _ := st.FireDueRoutines(ctx, "manager"); len(tasks) != 1 || tasks[0].RoutineID != rt.ID {
		t.Fatalf("one-off should fire once, got %+v", tasks)
	}
	got, _ := st.GetRoutine(ctx, rt.ID)
	if got.Enabled {
		t.Fatal("one-off automation should turn itself off after running")
	}
	if tasks, _ := st.FireDueRoutines(ctx, "manager"); len(tasks) != 0 {
		t.Fatal("one-off fired twice")
	}
}

func TestAutomationTargetsAgentAndRunsNow(t *testing.T) {
	h := newHarness(t)
	var rt store.Routine
	h.do("POST", "/api/v1/routines", adminTok, map[string]string{"title": "x", "schedule": "daily@09:00", "agent_id": "ghost"}, 400, nil)
	h.do("POST", "/api/v1/routines", adminTok, map[string]string{
		"title": "Inbox triage", "schedule": "weekly@mon,thu@08:00", "agent_id": "writer",
	}, 201, &rt)
	if rt.AgentID != "writer" {
		t.Fatalf("agent not stored: %+v", rt)
	}
	var run store.Task
	h.do("POST", "/api/v1/routines/"+rt.ID+"/run", adminTok, nil, 201, &run)
	if run.Assignee != "writer" || run.RoutineID != rt.ID {
		t.Fatalf("run now should go straight to the chosen agent: %+v", run)
	}
	var runs []store.Task
	h.do("GET", "/api/v1/routines/"+rt.ID+"/runs", adminTok, nil, 200, &runs)
	if len(runs) != 1 {
		t.Fatalf("runs = %d", len(runs))
	}
	var list []store.Routine
	h.do("GET", "/api/v1/routines", adminTok, nil, 200, &list)
	if list[0].LastStatus != store.StatusQueued || list[0].LastTaskID != run.ID {
		t.Fatalf("last run not reported: %+v", list[0])
	}

	// Editing keeps the clock unless the schedule changes.
	var upd store.Routine
	h.do("PUT", "/api/v1/routines/"+rt.ID, adminTok, map[string]string{
		"title": "Inbox triage", "description": "Draft replies", "schedule": "weekly@mon,thu@08:00",
	}, 200, &upd)
	if !upd.NextRunAt.Equal(rt.NextRunAt) || upd.AgentID != "" {
		t.Fatalf("edit changed the wrong things: %+v", upd)
	}
	h.do("PUT", "/api/v1/routines/"+rt.ID, adminTok, map[string]string{"title": "x", "schedule": "every:1m"}, 400, nil)
	h.do("POST", "/api/v1/routines/nope/run", adminTok, nil, 404, nil)
}
