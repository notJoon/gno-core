package gnolang

import (
	"fmt"
	"io"
	"os"
	"reflect"
	"sync"
)

// RealmStats captures per-realm object mutation statistics
// during FinalizeRealmTransaction.
type RealmStats struct {
	Path      string
	Created   int   // new objects persisted
	Updated   int   // directly modified existing objects
	Ancestors int   // additional objects re-serialized via dirty propagation
	Deleted   int   // removed objects
	BytesDiff int64 // net storage byte difference
}

// realmStatsLogger manages writing realm stats to a destination.
type realmStatsLogger struct {
	mu     sync.Mutex
	w      io.Writer
	closer io.Closer // non-nil if we opened the file
}

// newRealmStatsLogger creates a logger from a file path.
// If path is empty, returns nil (disabled).
// Special values: "stdout" → os.Stdout, "stderr" → os.Stderr.
func NewRealmStatsLogger(path string) *realmStatsLogger {
	if path == "" {
		return nil
	}
	switch path {
	case "stdout":
		return &realmStatsLogger{w: os.Stdout}
	case "stderr":
		return &realmStatsLogger{w: os.Stderr}
	default:
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARN: cannot open realm stats log %q: %v\n", path, err)
			return nil
		}
		return &realmStatsLogger{w: f, closer: f}
	}
}

func (l *realmStatsLogger) Close() {
	if l != nil && l.closer != nil {
		l.closer.Close()
	}
}

// LogStats writes a single realm's stats as a structured line.
func (l *realmStatsLogger) LogStats(s RealmStats) {
	if l == nil || l.w == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.w,
		"[realm-stats] path=%-50s created=%3d updated=%3d ancestors=%3d deleted=%3d bytes=%+d\n",
		s.Path, s.Created, s.Updated, s.Ancestors, s.Deleted, s.BytesDiff)
}

// LogSeparator writes a transaction boundary marker.
func (l *realmStatsLogger) LogSeparator(label string) {
	if l == nil || l.w == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.w, "--- %s ---\n", label)
}

// LogFinalizeTrigger writes which function return triggered a FinalizeRealmTransaction
// and why (explicit cross, implicit realm switch, or machine exit).
func (l *realmStatsLogger) LogFinalizeTrigger(realmPath, funcName, reason, prevRealm string) {
	if l == nil || l.w == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.w, "  [finalize-trigger] realm=%-40s func=%-50s reason=%-15s prev_realm=%s\n",
		realmPath, funcName, reason, prevRealm)
}

// objectTypeName returns a short type name for an Object.
func objectTypeName(oo Object) string {
	return reflect.TypeOf(oo).Elem().Name()
}

// collectObjectTypeCounts groups objects by type and returns counts.
func collectObjectTypeCounts(objects []Object) map[string]int {
	counts := make(map[string]int)
	for _, oo := range objects {
		counts[objectTypeName(oo)]++
	}
	return counts
}

// LogDetailedStats writes per-type breakdown for a realm.
// Skips realms with no activity (all counts zero).
func (l *realmStatsLogger) LogDetailedStats(s RealmStats, created, updated, deleted []Object) {
	if l == nil || l.w == nil {
		return
	}
	// Skip zero-activity entries to reduce noise.
	if s.Created == 0 && s.Updated == 0 && s.Ancestors == 0 && s.Deleted == 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	fmt.Fprintf(l.w,
		"[realm-stats] path=%-50s created=%3d updated=%3d ancestors=%3d deleted=%3d bytes=%+d\n",
		s.Path, s.Created, s.Updated, s.Ancestors, s.Deleted, s.BytesDiff)

	if len(created) > 0 {
		for typeName, count := range collectObjectTypeCounts(created) {
			fmt.Fprintf(l.w, "  [created] %-25s x%d\n", typeName, count)
		}
	}
	if len(updated) > 0 {
		for typeName, count := range collectObjectTypeCounts(updated) {
			fmt.Fprintf(l.w, "  [updated] %-25s x%d\n", typeName, count)
		}
	}
	if len(deleted) > 0 {
		for typeName, count := range collectObjectTypeCounts(deleted) {
			fmt.Fprintf(l.w, "  [deleted] %-25s x%d\n", typeName, count)
		}
	}
}

// LogObjectSizes writes per-object size breakdown for a realm.
// Groups by (category, typeName) and shows aggregate + individual entries.
func (l *realmStatsLogger) LogObjectSizes(realmPath string, entries []ObjectSizeEntry) {
	if l == nil || l.w == nil || len(entries) == 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	// aggregate by category+type
	type key struct {
		category string
		typeName string
	}
	type agg struct {
		count     int
		totalDiff int64
		totalSize int64
	}
	groups := make(map[key]*agg)
	for _, e := range entries {
		k := key{e.Category, e.TypeName}
		g, ok := groups[k]
		if !ok {
			g = &agg{}
			groups[k] = g
		}
		g.count++
		g.totalDiff += e.Diff
		g.totalSize += e.TotalSize
	}

	fmt.Fprintf(l.w, "  [object-sizes] realm=%s (%d objects)\n", realmPath, len(entries))
	for k, g := range groups {
		fmt.Fprintf(l.w, "    %-10s %-20s count=%-4d diff=%+8d  total_kv_size=%8d  avg_size=%d\n",
			k.category, k.typeName, g.count, g.totalDiff, g.totalSize, g.totalSize/int64(g.count))
	}

	// individual entries for top contributors (diff > 100 bytes or total > 500 bytes)
	for _, e := range entries {
		if e.Diff > 100 || e.Diff < -100 || e.TotalSize > 500 {
			fmt.Fprintf(l.w, "    [detail] %-10s oid=%-30s type=%-20s diff=%+6d  kv_size=%6d\n",
				e.Category, e.OID.String(), e.TypeName, e.Diff, e.TotalSize)
		}
	}
}
