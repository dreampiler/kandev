package sqlite

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kandev/kandev/internal/db"
	"github.com/kandev/kandev/internal/db/dialect"
)

type controlMergeAvailability struct{ github, plugin bool }

func (r *Repository) controlMergeAvailability(ctx context.Context) (controlMergeAvailability, error) {
	var available controlMergeAvailability
	for _, source := range []struct {
		table string
		dest  *bool
	}{{"github_task_prs", &available.github}, {"plugin_task_change_requests", &available.plugin}} {
		rows, err := r.ro.QueryContext(ctx, "SELECT 1 FROM "+source.table+" LIMIT 0")
		if err != nil {
			if db.IsMissingTableError(err) {
				continue
			}
			return available, err
		}
		_ = rows.Close()
		*source.dest = true
	}
	return available, nil
}

// URL identity is used only when the URL itself names the reported number.
// Unknown repository aliases keep their provider/host/repository tuple.
func controlMergeKey(url, number, provider, fallback string) string {
	trimmed := "RTRIM(" + url + ",'/')"
	plain := "CASE WHEN " + trimmed + " LIKE 'https://%' THEN SUBSTR(" + trimmed + ",9) WHEN " + trimmed + " LIKE 'http://%' THEN SUBSTR(" + trimmed + ",8) ELSE " + trimmed + " END"
	plain = "CASE WHEN " + provider + "='github' THEN LOWER(" + plain + ") ELSE " + plain + " END"
	return "CASE WHEN " + trimmed + " LIKE '%/'||CAST(" + number + " AS TEXT) THEN 'url:'||" + plain + " ELSE " + fallback + " END"
}

func controlBuiltinMergeKey(alias string) string {
	fallback := "'github:github.com:'||LOWER(" + alias + ".owner||'/'||" + alias + ".repo)||':'||CAST(" + alias + ".pr_number AS TEXT)"
	return controlMergeKey("COALESCE("+alias+".pr_url,'')", alias+".pr_number", "'github'", fallback)
}

const controlPluginMergeGroup = `SELECT MIN(p.id) AS id,p.workspace_id,MIN(p.task_id) AS task_id,
 p.provider_id,p.provider_host,p.repository_id,p.number,MAX(p.url) AS url,MAX(p.title) AS title,MIN(p.merged_at) AS merged_at
 FROM plugin_task_change_requests p JOIN tasks t ON t.id=p.task_id AND t.workspace_id=p.workspace_id
 WHERE ` + controlTaskVisibility + ` AND p.workspace_id=? AND p.state='merged' AND `

func controlMergeQuery(driver string, available controlMergeAvailability) string {
	var sources []string
	if available.github {
		sources = append(sources, `SELECT 'github_merge' AS family,'github:'||g.id AS id,g.workspace_id,g.task_id,
 g.pr_title AS title,'github' AS provider,g.owner||'/'||g.repo AS repository,g.pr_number AS number,g.merged_at,
 `+controlBuiltinMergeKey("g")+` AS merge_key,0 AS priority
 FROM github_task_prs g JOIN tasks t ON t.id=g.task_id AND t.workspace_id=g.workspace_id
 WHERE `+controlTaskVisibility+` AND g.workspace_id=? AND `+controlMergeWindow(driver, "g.merged_at"))
	}
	if available.plugin {
		key := controlMergeKey("p.url", "p.number", "p.provider_id", "p.provider_id||':'||p.provider_host||':'||p.repository_id||':'||CAST(p.number AS TEXT)")
		mergedAt, join := "p.merged_at", ""
		if available.github {
			join = ` LEFT JOIN github_task_prs b ON b.workspace_id=p.workspace_id AND b.pr_number=p.number AND ` + controlMergePresent(driver, "b.merged_at") + ` AND (
 (` + controlBuiltinMergeKey("b") + `=` + key + `) OR
 (p.provider_id='github' AND (p.repository_id=b.repository_id OR LOWER(p.repository_id)=LOWER(b.owner||'/'||b.repo)) AND (
 (p.provider_host IN ('','github.com') AND (LOWER(b.pr_url) LIKE 'https://github.com/%' OR COALESCE(b.pr_url,'')='')) OR
 (p.provider_host<>'' AND (LOWER(b.pr_url) LIKE 'https://'||p.provider_host||'/%' OR LOWER(b.pr_url) LIKE 'http://'||p.provider_host||'/%')))))`
			key = "CASE WHEN b.id IS NOT NULL THEN " + controlBuiltinMergeKey("b") + " ELSE " + key + " END"
			mergedAt = "COALESCE(b.merged_at,p.merged_at)"
		}
		sources = append(sources, `SELECT 'plugin_merge' AS family,'plugin:'||p.id AS id,p.workspace_id,p.task_id,
 p.title,p.provider_id AS provider,CASE WHEN p.provider_host='' THEN p.repository_id ELSE p.provider_host||'/'||p.repository_id END AS repository,
 p.number,`+mergedAt+` AS merged_at,`+key+` AS merge_key,1 AS priority FROM (`+controlPluginMergeGroup+controlMergeWindow(driver, "p.merged_at")+`
 GROUP BY p.workspace_id,p.provider_id,p.provider_host,p.repository_id,p.number) p`+join)
	}
	if len(sources) == 0 {
		return ""
	}
	return `WITH observations AS (` + strings.Join(sources, " UNION ALL ") + `), ranked AS (
 SELECT observations.*,ROW_NUMBER() OVER (PARTITION BY workspace_id,merge_key ORDER BY priority,id) AS duplicate_position
 FROM observations WHERE ` + controlMergeWindow(driver, "merged_at") + `)
 SELECT * FROM ranked WHERE duplicate_position=1`
}

func controlMergePresent(driver, column string) string {
	if dialect.IsPostgres(driver) {
		return column + " IS NOT NULL"
	}
	return "(" + column + " IS NOT NULL AND " + column + "<>'')"
}

// SQLite's Julian value only selects a slightly wider candidate window. The
// cursor reader applies the exact UTC/nanosecond boundaries before counting.
// Malformed nonempty values must reach the scanner and fail, rather than vanish.
func controlMergeWindow(driver, column string) string {
	if dialect.IsPostgres(driver) {
		return column + ">=? AND " + column + "<=?"
	}
	value := "julianday(" + column + ")"
	invalidShape := column + " NOT GLOB '????-??-??[ T]??:??:??*'"
	return controlMergePresent(driver, column) + " AND (" + value + " IS NULL OR " + invalidShape + " OR (" + value + ">=julianday(?)-(1.0/86400) AND " + value + "<=julianday(?)+(1.0/86400)))"
}

func controlMergeArgs(available controlMergeAvailability, ws string, since, now time.Time) []any {
	var args []any
	if available.github {
		args = append(args, ws, since, now)
	}
	if available.plugin {
		args = append(args, ws, since, now)
	}
	return append(args, since, now)
}

func (r *Repository) controlMergedCount(ctx context.Context, ws string, since, now time.Time) *int {
	available, err := r.controlMergeAvailability(ctx)
	if err != nil || !available.github || !available.plugin {
		return nil
	}
	merged, err := r.readControlMerges(ctx, available, ws, since, now)
	if err != nil {
		return nil
	}
	count := merged.totals["github_merge"] + merged.totals["plugin_merge"]
	return &count
}

type controlMergeEventRow struct {
	Family     string                `db:"family"`
	ID         string                `db:"id"`
	TaskID     string                `db:"task_id"`
	Title      string                `db:"title"`
	Provider   string                `db:"provider"`
	Repository string                `db:"repository"`
	Number     int64                 `db:"number"`
	MergedAt   controlMergeTimestamp `db:"merged_at"`
}

type controlMergeTimestamp struct{ time.Time }

func (t *controlMergeTimestamp) Scan(value any) error {
	switch value := value.(type) {
	case time.Time:
		t.Time = value.UTC()
	case string:
		t.Time = parseSqliteTime(value)
	case []byte:
		t.Time = parseSqliteTime(string(value))
	default:
		return fmt.Errorf("invalid merged change request timestamp type")
	}
	if t.IsZero() {
		return fmt.Errorf("invalid merged change request timestamp")
	}
	return nil
}

func (r *Repository) appendControlMergeHistory(ctx context.Context, out *ControlHistory) error {
	available, err := r.controlMergeAvailability(ctx)
	for _, source := range []string{"github_merge", "plugin_merge"} {
		out.Sources[source] = ControlCoverage{State: "unavailable", Reason: "source_read_failed"}
	}
	if err != nil {
		return err
	}
	if available.github {
		zero := 0
		out.Sources["github_merge"] = ControlCoverage{State: "known", Total: &zero}
	}
	if available.plugin {
		zero := 0
		out.Sources["plugin_merge"] = ControlCoverage{State: "known", Total: &zero}
	}
	merged, err := r.readControlMerges(ctx, available, out.WorkspaceID, out.Since, out.GeneratedAt)
	if err != nil {
		return err
	}
	for family, events := range merged.events {
		coverage := out.Sources[family]
		total := merged.totals[family]
		coverage.Total = &total
		coverage.Returned = len(events)
		coverage.Truncated = coverage.Returned < total
		out.Sources[family] = coverage
		out.Events = append(out.Events, events...)
	}
	return nil
}

type controlMergeSummary struct {
	totals map[string]int
	events map[string][]ControlEvent
}

func (r *Repository) readControlMerges(ctx context.Context, available controlMergeAvailability, ws string, since, now time.Time) (controlMergeSummary, error) {
	merged := controlMergeSummary{totals: map[string]int{}, events: map[string][]ControlEvent{}}
	base := controlMergeQuery(r.ro.DriverName(), available)
	if base == "" {
		return merged, nil
	}
	query := `SELECT family,id,task_id,title,provider,repository,number,merged_at FROM (` + base + `) unique_merges`
	rows, err := r.ro.QueryxContext(ctx, r.ro.Rebind(query), controlMergeArgs(available, ws, since, now)...)
	if err != nil {
		return merged, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var row controlMergeEventRow
		if err := rows.StructScan(&row); err != nil {
			return controlMergeSummary{}, err
		}
		if row.MergedAt.Before(since) || row.MergedAt.After(now) {
			continue
		}
		merged.totals[row.Family]++
		merged.keepLatest(row)
	}
	return merged, rows.Err()
}

func (merged *controlMergeSummary) keepLatest(row controlMergeEventRow) {
	events := merged.events[row.Family]
	events = append(events, ControlEvent{ID: row.ID, Kind: row.Family + "d", TaskID: row.TaskID, Title: row.Title, Provider: row.Provider, Repository: row.Repository, Number: row.Number, OccurredAt: row.MergedAt.Time})
	sort.Slice(events, func(i, j int) bool {
		if events[i].OccurredAt.Equal(events[j].OccurredAt) {
			return events[i].ID < events[j].ID
		}
		return events[i].OccurredAt.After(events[j].OccurredAt)
	})
	if len(events) > 50 {
		events = events[:50]
	}
	merged.events[row.Family] = events
}
