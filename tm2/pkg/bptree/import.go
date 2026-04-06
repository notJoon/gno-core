package bptree

import "fmt"

// Importer reconstructs a tree from a stream of ExportNodes.
type Importer struct {
	tree    *MutableTree
	version int64
}

// Import creates an Importer that will reconstruct a tree at the given version.
func (t *MutableTree) Import(version int64) (*Importer, error) {
	if t.VersionExists(version) {
		return nil, fmt.Errorf("version %d already exists", version)
	}
	return &Importer{tree: t, version: version}, nil
}

// Add adds an ExportNode to the tree being imported. Nodes must arrive
// in key-sorted order (as produced by the Exporter for leaf nodes).
// Inner node markers (Height > 0) are ignored — the tree structure is
// rebuilt from the key-value insertions.
func (imp *Importer) Add(node *ExportNode) error {
	if node.Height > 0 {
		// Inner node marker — skip. The tree structure is rebuilt
		// by inserting keys in sorted order.
		return nil
	}
	// Leaf node — insert key-value pair
	_, err := imp.tree.Set(node.Key, node.Value)
	return err
}

// Commit finalizes the import by saving the version.
func (imp *Importer) Commit() error {
	// Set version so SaveVersion uses the target version.
	// Clear initialVersion to avoid the WorkingVersion() special case.
	imp.tree.version = imp.version - 1
	imp.tree.initialVersion = 0
	_, _, err := imp.tree.SaveVersion()
	return err
}

// Close is a no-op for cleanup compatibility.
func (imp *Importer) Close() error {
	return nil
}
