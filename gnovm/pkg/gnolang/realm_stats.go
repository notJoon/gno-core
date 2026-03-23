package gnolang

import (
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
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

// LogObjectCost writes per-object storage cost detail.
// op: "create", "update", "ancestor", or "delete"
// diff: bytes added (positive) or freed (negative) by this single object
// runningTotal: cumulative sumDiff for this realm finalization so far
func (l *realmStatsLogger) LogObjectCost(realmPath, op string, store Store, oo Object, diff int64, runningTotal int64) {
	if l == nil || l.w == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	oi := oo.GetObjectInfo()
	typeName := objectTypeName(oo)
	newSize := oi.LastObjectSize
	label := objectLabel(store, oo)
	rootVar := objectRootVar(store, oo)
	if rootVar != "" {
		label = fmt.Sprintf("root=%s %s", rootVar, label)
	}
	fieldPath := objectFieldPath(store, oo)
	if fieldPath != "" {
		label = fmt.Sprintf("field_path=%s %s", fieldPath, label)
	}
	fmt.Fprintf(l.w,
		"    [obj-cost] op=%-10s type=%-20s oid=%-30s owner=%-30s size=%6d diff=%+6d running=%+d %s\n",
		op, typeName, oi.ID.String(), oi.OwnerID.String(), newSize, diff, runningTotal, label)
}

// objectFieldPath walks the ownership chain upward from an object, collecting
// struct field names along the way. This identifies which specific field within
// a struct hierarchy the object belongs to.
// Example output: "ticks.leftNode" or "positions" or "observationState.observations"
func objectFieldPath(store Store, oo Object) string {
	defer func() { recover() }() //nolint:errcheck

	var fields []string
	current := oo

	for depth := 0; depth < 30; depth++ {
		owner := current.GetOwner()
		if owner == nil {
			ownerID := current.GetOwnerID()
			if ownerID.IsZero() {
				break
			}
			owner = store.GetObject(ownerID)
			if owner == nil {
				break
			}
		}

		// If owner is a StructValue, find which field the current object is in
		if sv, ok := owner.(*StructValue); ok {
			fieldName := resolveStructFieldName(store, sv, current)
			if fieldName != "" {
				fields = append(fields, fieldName)
			}
		}

		// Stop at Block (package level reached)
		if _, ok := owner.(*Block); ok {
			break
		}

		current = owner
	}

	if len(fields) == 0 {
		return ""
	}
	// Reverse: fields are collected bottom-up, we want top-down
	for i, j := 0, len(fields)-1; i < j; i, j = i+1, j-1 {
		fields[i], fields[j] = fields[j], fields[i]
	}
	return strings.Join(fields, ".")
}

// resolveStructFieldName finds which field in a StructValue contains the child object.
// It matches the child's OID against each field's value, then looks up the field name
// from the StructType. Falls back to "[index]" if the type can't be resolved.
func resolveStructFieldName(store Store, sv *StructValue, child Object) string {
	childOID := child.GetObjectID()
	if childOID.IsZero() {
		return ""
	}

	// First, try to resolve the StructType for this StructValue.
	st := getStructTypeFromValue(store, sv)

	for i, fv := range sv.Fields {
		var fieldOID ObjectID
		switch cv := fv.V.(type) {
		case Object:
			fieldOID = cv.GetObjectID()
		case RefValue:
			fieldOID = cv.ObjectID
		default:
			continue
		}
		if fieldOID == childOID {
			if st != nil && i < len(st.Fields) {
				return string(st.Fields[i].Name)
			}
			// Fallback: use index with type hint
			typeName := ""
			if fv.T != nil {
				typeName = fv.T.String()
			}
			if typeName != "" {
				return fmt.Sprintf("[%d](%s)", i, typeName)
			}
			return fmt.Sprintf("[%d]", i)
		}
	}
	return ""
}

// getStructTypeFromValue resolves the StructType for a StructValue by checking
// how it's referenced in the ownership chain.
func getStructTypeFromValue(store Store, sv *StructValue) *StructType {
	// Strategy 1: Check if the StructValue's owner is a HeapItemValue
	// whose TypedValue.T is a StructType or *StructType.
	owner := sv.GetOwner()
	if owner == nil {
		ownerID := sv.GetOwnerID()
		if !ownerID.IsZero() {
			owner = store.GetObject(ownerID)
		}
	}
	if owner == nil {
		return nil
	}

	if hiv, ok := owner.(*HeapItemValue); ok {
		return extractStructType(hiv.Value.T)
	}

	// Strategy 2: If owner is a StructValue, scan its Fields to find
	// the TypedValue that references our StructValue.
	if parentSV, ok := owner.(*StructValue); ok {
		svOID := sv.GetObjectID()
		for _, fv := range parentSV.Fields {
			var fieldOID ObjectID
			switch cv := fv.V.(type) {
			case Object:
				fieldOID = cv.GetObjectID()
			case RefValue:
				fieldOID = cv.ObjectID
			default:
				continue
			}
			if fieldOID == svOID {
				return extractStructType(fv.T)
			}
		}
	}

	return nil
}

// extractStructType unwraps a Type to find the underlying *StructType,
// handling pointer types.
func extractStructType(t Type) *StructType {
	if t == nil {
		return nil
	}
	if st, ok := t.(*StructType); ok {
		return st
	}
	if pt, ok := t.(*PointerType); ok {
		if st, ok := pt.Elt.(*StructType); ok {
			return st
		}
	}
	return nil
}

// objectRootVar walks the ownership chain upward to find the package-level
// variable name that this object ultimately belongs to.
// The chain is: object → owner → ... → HeapItemValue → Block(pkg-level).
// Returns "varName@file:line" or "" if not resolvable.
func objectRootVar(store Store, oo Object) string {
	current := oo
	for depth := 0; depth < 30; depth++ {
		owner := current.GetOwner()
		if owner == nil {
			ownerID := current.GetOwnerID()
			if ownerID.IsZero() {
				// Escaped object (refcount>1) has no owner.
				// Reverse lookup: find the package block and search its values.
				return findVarInPkgBlock(store, current)
			}
			owner = store.GetObject(ownerID)
			if owner == nil {
				return ""
			}
		}

		if blk, ok := owner.(*Block); ok {
			src := blk.GetSource(store)
			if src == nil {
				return ""
			}
			names := src.GetBlockNames()
			targetOID := current.GetObjectID()
			for j, tv := range blk.Values {
				var childOID ObjectID
				switch cv := tv.V.(type) {
				case Object:
					childOID = cv.GetObjectID()
				case RefValue:
					childOID = cv.ObjectID
				default:
					continue
				}
				if childOID == targetOID && j < len(names) {
					return formatVarWithLocation(src, j, names[j])
				}
			}
			return ""
		}

		current = owner
	}
	return ""
}

// formatVarWithLocation returns "varName@file:line" using the BlockNode's
// NameSource and (for package-level blocks) FileSet to find the exact file.
func formatVarWithLocation(src BlockNode, index int, name Name) string {
	// For package-level blocks, use GetDeclFor to find the declaring file.
	if pn, ok := src.(*PackageNode); ok && pn.FileSet != nil {
		fn, _, found := pn.FileSet.GetDeclForSafe(name)
		if found && fn != nil {
			loc := fn.GetLocation()
			// Find the line from NameSource's Origin.
			line := 0
			nss := src.GetNameSources()
			if index < len(nss) && nss[index].Origin != nil {
				line = nss[index].Origin.GetSpan().Pos.Line
			}
			file := loc.File
			if file == "" {
				file = loc.PkgPath
			}
			if line > 0 {
				return fmt.Sprintf("%s@%s:%d", name, file, line)
			}
			return fmt.Sprintf("%s@%s", name, file)
		}
	}

	// Fallback: use the NameSource's Origin span + block location.
	nss := src.GetNameSources()
	if index < len(nss) {
		ns := nss[index]
		if ns.Origin != nil {
			span := ns.Origin.GetSpan()
			loc := src.GetLocation()
			file := loc.File
			if file == "" {
				file = loc.PkgPath
			}
			if span.Pos.Line > 0 {
				return fmt.Sprintf("%s@%s:%d", name, file, span.Pos.Line)
			}
			return fmt.Sprintf("%s@%s", name, file)
		}
	}
	return string(name)
}

// findVarInPkgBlock handles escaped objects (OwnerID zero, refcount>1).
// It loads the package block using the object's PkgID and searches each
// HeapItemValue slot to see if the target object is reachable.
func findVarInPkgBlock(store Store, target Object) (result string) {
	defer func() { recover() }() //nolint:errcheck

	targetOID := target.GetObjectID()
	if targetOID.IsZero() {
		return ""
	}
	// Load the package block (oid = PkgID:2, conventionally the package block).
	pkgOID := ObjectIDFromPkgID(targetOID.PkgID)
	pkgObj := store.GetObject(pkgOID)
	if pkgObj == nil {
		return ""
	}
	pv, ok := pkgObj.(*PackageValue)
	if !ok {
		return ""
	}
	// Get the package block.
	var blk *Block
	switch bv := pv.Block.(type) {
	case *Block:
		blk = bv
	case RefValue:
		obj := store.GetObject(bv.ObjectID)
		if obj == nil {
			return ""
		}
		blk, ok = obj.(*Block)
		if !ok {
			return ""
		}
	default:
		return ""
	}
	src := blk.GetSource(store)
	if src == nil {
		return ""
	}
	names := src.GetBlockNames()

	// For each HeapItemValue in the block, check if target is reachable.
	for j, tv := range blk.Values {
		hiv, isHeap := tv.V.(*HeapItemValue)
		if !isHeap || j >= len(names) {
			continue
		}
		if containsObject(store, hiv, targetOID, 10) {
			return formatVarWithLocation(src, j, names[j])
		}
	}
	return ""
}

// containsObject checks if the target OID is reachable from the given object
// within maxDepth levels of child traversal.
func containsObject(store Store, oo Object, targetOID ObjectID, maxDepth int) bool {
	oid := oo.GetObjectID()
	if oid.IsZero() {
		return false
	}
	if oid == targetOID {
		return true
	}
	if maxDepth <= 0 {
		return false
	}

	// Use a recover to handle any panics from store operations during traversal.
	defer func() { recover() }() //nolint:errcheck

	children := getChildObjects2(store, oo)
	for _, child := range children {
		childOID := child.GetObjectID()
		if childOID.IsZero() {
			continue
		}
		if childOID == targetOID {
			return true
		}
		if containsObject(store, child, targetOID, maxDepth-1) {
			return true
		}
	}
	return false
}

// objectLabel extracts a human-readable label from an Object.
// For Block: source location and variable names declared in the scope.
// For FuncValue: function name and source file.
// For PackageValue: package path.
// For HeapItemValue: the variable name from the owning Block (if resolvable).
// For BoundMethodValue: method name.
func objectLabel(store Store, oo Object) string {
	switch v := oo.(type) {
	case *Block:
		src := v.GetSource(store)
		if src == nil {
			return ""
		}
		loc := src.GetLocation()
		names := src.GetBlockNames()
		if len(names) == 0 {
			return fmt.Sprintf("loc=%s", loc.String())
		}
		nameStrs := make([]string, len(names))
		for i, n := range names {
			nameStrs[i] = string(n)
		}
		return fmt.Sprintf("loc=%s vars=[%s]", loc.String(), strings.Join(nameStrs, ","))

	case *FuncValue:
		loc := ""
		if v.Source != nil {
			loc = v.Source.GetLocation().String()
		}
		if loc != "" {
			return fmt.Sprintf("func=%s loc=%s", v.Name, loc)
		}
		return fmt.Sprintf("func=%s pkg=%s file=%s", v.Name, v.PkgPath, v.FileName)

	case *PackageValue:
		return fmt.Sprintf("pkg=%s", v.PkgPath)

	case *HeapItemValue:
		owner := v.GetOwner()
		if owner == nil {
			return ""
		}
		blk, ok := owner.(*Block)
		if !ok {
			return ""
		}
		src := blk.GetSource(store)
		if src == nil {
			return ""
		}
		names := src.GetBlockNames()
		// Find which slot this HeapItemValue occupies.
		for i, tv := range blk.Values {
			if tv.V == v {
				if i < len(names) {
					loc := src.GetLocation()
					return fmt.Sprintf("var=%s loc=%s", names[i], loc.String())
				}
				break
			}
		}
		return ""

	case *BoundMethodValue:
		if v.Func != nil {
			return fmt.Sprintf("method=%s", v.Func.Name)
		}
		return ""

	default:
		return ""
	}
}
