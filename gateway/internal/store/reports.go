package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Report periods the work log understands.
const (
	PeriodToday     = "today"
	PeriodYesterday = "yesterday"
	PeriodWeek      = "week"       // this week, Monday until now
	PeriodLastWeek  = "last_week"  // the whole of the previous week
	PeriodMonth     = "month"      // this calendar month until now
	PeriodLastMonth = "last_month" // the whole of the previous calendar month
)

// PeriodRange turns a period name into a half-open time range [from, to) in the owner's local time, and a label
// such as "Tuesday 21 October 2026" or "Week of Monday 19 October 2026".
func PeriodRange(now time.Time, period string) (from, to time.Time, label string, err error) {
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	monday := day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
	switch period {
	case PeriodToday:
		return day, day.AddDate(0, 0, 1), day.Format("Monday 2 January 2006"), nil
	case PeriodYesterday:
		y := day.AddDate(0, 0, -1)
		return y, day, y.Format("Monday 2 January 2006"), nil
	case PeriodWeek:
		return monday, monday.AddDate(0, 0, 7), "Week of " + monday.Format("Monday 2 January 2006"), nil
	case PeriodLastWeek:
		last := monday.AddDate(0, 0, -7)
		return last, monday, "Week of " + last.Format("Monday 2 January 2006"), nil
	case PeriodMonth:
		first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		return first, first.AddDate(0, 1, 0), first.Format("January 2006"), nil
	case PeriodLastMonth:
		first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		prev := first.AddDate(0, -1, 0)
		return prev, first, prev.Format("January 2006"), nil
	}
	return time.Time{}, time.Time{}, "", fmt.Errorf("period must be today, yesterday, week, last_week, month or last_month")
}

// WorkLog is everything the staff did in a period, taken from what the office already records.
type WorkLog struct {
	Period      string          `json:"period"`
	Label       string          `json:"label"`
	From        time.Time       `json:"from"`
	To          time.Time       `json:"to"`
	Assignments []LogAssignment `json:"assignments"`
	Chats       []LogChat       `json:"chats"`
	Agents      []LogAgent      `json:"agents"`
	Questions   int             `json:"questions"`
	Approvals   int             `json:"approvals"`
	Unanswered  int             `json:"unanswered"`
	Memories    []string        `json:"memories"`
	Files       []string        `json:"files,omitempty"`
}

type LogAssignment struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	Agent     string    `json:"agent"`
	Steps     int       `json:"steps"` // subtasks it was split into
	Result    string    `json:"result,omitempty"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type LogChat struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Agent    string `json:"agent"`
	Messages int    `json:"messages"`
}

type LogAgent struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	TasksDone    int    `json:"tasks_done"`
	ToolCalls    int    `json:"tool_calls"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
}

// WorkLog gathers what happened between from and to.
func (s *Store) WorkLog(ctx context.Context, period string, from, to time.Time, label string) (*WorkLog, error) {
	f, t := from.UnixMilli(), to.UnixMilli()
	log := &WorkLog{Period: period, Label: label, From: from, To: to,
		Assignments: []LogAssignment{}, Chats: []LogChat{}, Agents: []LogAgent{}, Memories: []string{}}

	names := map[string]string{}
	agents, err := s.ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	for _, a := range agents {
		names[a.ID] = a.Name
	}
	name := func(id string) string {
		if n, ok := names[id]; ok {
			return n
		}
		return id
	}

	rows, err := s.db.QueryContext(ctx, `SELECT t.id, t.title, t.status, t.assignee, t.result, t.error, t.created_at,
		(SELECT COUNT(*) FROM tasks c WHERE c.parent_id = t.id AND c.kind = ?)
		FROM tasks t WHERE t.kind = ? AND ((t.created_at >= ? AND t.created_at < ?) OR (t.updated_at >= ? AND t.updated_at < ?))
		ORDER BY t.created_at LIMIT 100`, KindSubtask, KindAssignment, f, t, f, t)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var a LogAssignment
		var assignee string
		var created int64
		if err := rows.Scan(&a.ID, &a.Title, &a.Status, &assignee, &a.Result, &a.Error, &created, &a.Steps); err != nil {
			rows.Close()
			return nil, err
		}
		a.Agent, a.CreatedAt = name(assignee), fromMS(created)
		a.Result = preview(a.Result, 500)
		a.Error = preview(a.Error, 200)
		log.Assignments = append(log.Assignments, a)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	rows, err = s.db.QueryContext(ctx, `SELECT c.id, c.title, c.agent_id, COUNT(m.id) FROM messages m
		JOIN conversations c ON c.id = m.conversation_id WHERE m.created_at >= ? AND m.created_at < ?
		GROUP BY c.id ORDER BY MAX(m.created_at) LIMIT 100`, f, t)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c LogChat
		var agent string
		if err := rows.Scan(&c.ID, &c.Title, &agent, &c.Messages); err != nil {
			rows.Close()
			return nil, err
		}
		if c.Title == "" {
			c.Title = "Untitled chat"
		}
		c.Agent = name(agent)
		log.Chats = append(log.Chats, c)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	byAgent := map[string]*LogAgent{}
	entry := func(id string) *LogAgent {
		if id == "" {
			return nil
		}
		if a, ok := byAgent[id]; ok {
			return a
		}
		a := &LogAgent{ID: id, Name: name(id)}
		byAgent[id] = a
		return a
	}
	type countQuery struct {
		sql  string
		args []any
		set  func(a *LogAgent, n, m int64)
	}
	for _, q := range []countQuery{
		{`SELECT assignee, COUNT(*), 0 FROM tasks WHERE status = ? AND kind != ? AND updated_at >= ? AND updated_at < ? GROUP BY assignee`,
			[]any{StatusDone, KindPersona, f, t}, func(a *LogAgent, n, _ int64) { a.TasksDone = int(n) }},
		{`SELECT agent_id, COUNT(*), 0 FROM steps WHERE kind = ? AND created_at >= ? AND created_at < ? GROUP BY agent_id`,
			[]any{StepTool, f, t}, func(a *LogAgent, n, _ int64) { a.ToolCalls = int(n) }},
		{`SELECT employee_id, SUM(input_tokens), SUM(output_tokens) FROM usage WHERE created_at >= ? AND created_at < ? GROUP BY employee_id`,
			[]any{f, t}, func(a *LogAgent, n, m int64) { a.InputTokens, a.OutputTokens = n, m }},
	} {
		qr, err := s.db.QueryContext(ctx, q.sql, q.args...)
		if err != nil {
			return nil, err
		}
		for qr.Next() {
			var id string
			var n, m int64
			if err := qr.Scan(&id, &n, &m); err != nil {
				qr.Close()
				return nil, err
			}
			if a := entry(id); a != nil {
				q.set(a, n, m)
			}
		}
		if err := qr.Close(); err != nil {
			return nil, err
		}
	}
	for _, a := range byAgent {
		log.Agents = append(log.Agents, *a)
	}
	sort.Slice(log.Agents, func(i, j int) bool {
		if log.Agents[i].TasksDone != log.Agents[j].TasksDone {
			return log.Agents[i].TasksDone > log.Agents[j].TasksDone
		}
		return log.Agents[i].Name < log.Agents[j].Name
	})

	ir, err := s.db.QueryContext(ctx, `SELECT kind, status, COUNT(*) FROM inbox WHERE created_at >= ? AND created_at < ? GROUP BY kind, status`, f, t)
	if err != nil {
		return nil, err
	}
	for ir.Next() {
		var kind, status string
		var n int
		if err := ir.Scan(&kind, &status, &n); err != nil {
			ir.Close()
			return nil, err
		}
		if kind == InboxApproval {
			log.Approvals += n
		} else {
			log.Questions += n
		}
		if status == "open" {
			log.Unanswered += n
		}
	}
	if err := ir.Close(); err != nil {
		return nil, err
	}

	mr, err := s.db.QueryContext(ctx, `SELECT content FROM memories WHERE created_at >= ? AND created_at < ? ORDER BY created_at LIMIT 40`, f, t)
	if err != nil {
		return nil, err
	}
	for mr.Next() {
		var c string
		if err := mr.Scan(&c); err != nil {
			mr.Close()
			return nil, err
		}
		log.Memories = append(log.Memories, c)
	}
	return log, mr.Close()
}

// Markdown writes the log as a plain, factual summary the main agent turns into a report.
func (w *WorkLog) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Work log: %s\n\n", w.Label)
	done, failed, running := 0, 0, 0
	for _, a := range w.Assignments {
		switch a.Status {
		case StatusDone:
			done++
		case StatusFailed, StatusCancelled:
			failed++
		default:
			running++
		}
	}
	tools, tokens := 0, int64(0)
	for _, a := range w.Agents {
		tools += a.ToolCalls
		tokens += a.InputTokens + a.OutputTokens
	}
	fmt.Fprintf(&b, "- Assignments: %d (%d done, %d still open, %d failed or cancelled)\n", len(w.Assignments), done, running, failed)
	fmt.Fprintf(&b, "- Chats: %d\n- Tool calls: %d\n- Tokens used: %d\n", len(w.Chats), tools, tokens)
	fmt.Fprintf(&b, "- Questions asked of the owner: %d, approvals requested: %d, still waiting: %d\n", w.Questions, w.Approvals, w.Unanswered)

	b.WriteString("\n## Assignments\n")
	if len(w.Assignments) == 0 {
		b.WriteString("None in this period.\n")
	}
	for _, a := range w.Assignments {
		fmt.Fprintf(&b, "\n### %s\n- Status: %s; led by %s; %d step(s); started %s\n", a.Title, a.Status, a.Agent, a.Steps,
			a.CreatedAt.Local().Format("Mon 2 Jan 15:04"))
		if a.Result != "" {
			fmt.Fprintf(&b, "- Outcome: %s\n", a.Result)
		}
		if a.Error != "" {
			fmt.Fprintf(&b, "- Problem: %s\n", a.Error)
		}
	}

	b.WriteString("\n## Chats\n")
	if len(w.Chats) == 0 {
		b.WriteString("None in this period.\n")
	}
	for _, c := range w.Chats {
		fmt.Fprintf(&b, "- %s, with %s (%d message(s))\n", c.Title, c.Agent, c.Messages)
	}

	b.WriteString("\n## Team activity\n")
	if len(w.Agents) == 0 {
		b.WriteString("No activity recorded.\n")
	}
	for _, a := range w.Agents {
		fmt.Fprintf(&b, "- %s: %d task(s) finished, %d tool call(s), %d tokens\n", a.Name, a.TasksDone, a.ToolCalls, a.InputTokens+a.OutputTokens)
	}

	if len(w.Memories) > 0 {
		b.WriteString("\n## New things the staff learned about the owner\n")
		for _, m := range w.Memories {
			fmt.Fprintf(&b, "- %s\n", m)
		}
	}
	if len(w.Files) > 0 {
		b.WriteString("\n## Files produced\n")
		for _, f := range w.Files {
			fmt.Fprintf(&b, "- %s\n", f)
		}
	}
	return b.String()
}
