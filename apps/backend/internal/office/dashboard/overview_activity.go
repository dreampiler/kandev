package dashboard

import (
	"context"
	"sort"
	"time"

	"github.com/kandev/kandev/internal/office/repository/sqlite"
)

// overviewFailureBucketOrder is the order the four failure buckets are always
// reported in, so the expanded breakdown reads the same for every workspace and
// a zero bucket is still named rather than silently dropped.
var overviewFailureBucketOrder = []string{"no_response", "limit", "start_failed", "other"}

// overviewFailureSampleLimit bounds how many verbatim error kinds a broken-down
// bucket lists. The buckets carry the exact totals; the samples are the agent's
// own words behind them, so a long tail is truncated rather than the bucket
// count.
const overviewFailureSampleLimit = 5

// fillWorkspaceActivity reads the period totals and failure buckets and hangs
// them on each workspace's metrics. The period is the snapshot's window, so a
// 7-day card reports 7-day figures; the rest of the overview keeps its own
// fixed window.
func (s *DashboardService) fillWorkspaceActivity(
	ctx context.Context, snap *overviewSnapshot, ids []string,
) error {
	since := snap.now.Add(-time.Duration(snap.windowHours) * time.Hour)
	counts, err := s.overviewReader.ListOverviewWorkspaceActivity(ctx, ids, since)
	if err != nil {
		return err
	}
	rows, err := s.overviewReader.ListOverviewFailureBuckets(ctx, ids, since)
	if err != nil {
		return err
	}
	buckets, samples := summarizeFailureBuckets(rows)
	for i := range snap.resp.Workspaces {
		entry := &snap.resp.Workspaces[i]
		if entry.Metrics == nil {
			continue
		}
		c := counts[entry.WorkspaceID]
		entry.Metrics.Activity = &OverviewWorkspaceActivity{
			WindowHours:     snap.windowHours,
			Completed:       c.Completed,
			SessionsStarted: c.SessionsStarted,
			SessionsFailed:  c.SessionsFailed,
			AgentTurns:      c.AgentTurns,
			StepMoves:       c.StepMoves,
			FailureBuckets:  buckets[entry.WorkspaceID],
			FailureSamples:  samples[entry.WorkspaceID],
		}
	}
	return nil
}

// summarizeFailureBuckets folds the grouped rows into the four-bucket list a
// workspace shows and the verbatim kinds behind it. A workspace with no failed
// session gets no buckets at all, so the client renders no breakdown rather
// than four zeros.
func summarizeFailureBuckets(
	rows []*sqlite.OverviewFailureBucketRow,
) (map[string][]OverviewFailureBucket, map[string][]OverviewErrorKind) {
	sums := map[string]map[string]int{}
	kinds := map[string]map[string]int{}
	for _, row := range rows {
		if sums[row.WorkspaceID] == nil {
			sums[row.WorkspaceID] = map[string]int{}
		}
		sums[row.WorkspaceID][row.Bucket] += row.Count
		if kind := failureSampleKind(row.ErrorMessage); kind != "" {
			if kinds[row.WorkspaceID] == nil {
				kinds[row.WorkspaceID] = map[string]int{}
			}
			kinds[row.WorkspaceID][kind] += row.Count
		}
	}
	return bucketLists(sums), sampleLists(kinds)
}

func bucketLists(sums map[string]map[string]int) map[string][]OverviewFailureBucket {
	out := make(map[string][]OverviewFailureBucket, len(sums))
	for workspaceID, byBucket := range sums {
		list := make([]OverviewFailureBucket, 0, len(overviewFailureBucketOrder))
		for _, code := range overviewFailureBucketOrder {
			list = append(list, OverviewFailureBucket{Code: code, Count: byBucket[code]})
		}
		out[workspaceID] = list
	}
	return out
}

func sampleLists(kinds map[string]map[string]int) map[string][]OverviewErrorKind {
	out := make(map[string][]OverviewErrorKind, len(kinds))
	for workspaceID, byKind := range kinds {
		list := make([]OverviewErrorKind, 0, len(byKind))
		for kind, count := range byKind {
			list = append(list, OverviewErrorKind{Kind: kind, Count: count})
		}
		sort.SliceStable(list, func(i, j int) bool {
			if list[i].Count != list[j].Count {
				return list[i].Count > list[j].Count
			}
			return list[i].Kind < list[j].Kind
		})
		if len(list) > overviewFailureSampleLimit {
			list = list[:overviewFailureSampleLimit]
		}
		out[workspaceID] = list
	}
	return out
}
