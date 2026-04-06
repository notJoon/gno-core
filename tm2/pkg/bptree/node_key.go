package bptree

import "encoding/binary"

// NodeKey identifies a node in the database: (version, nonce).
// Version is the tree version when the node was created.
// Nonce is a per-version counter distinguishing nodes within a version.
type NodeKey struct {
	Version int64
	Nonce   uint32
}

// GetKey serializes the NodeKey to a 12-byte slice (big-endian).
func (nk *NodeKey) GetKey() []byte {
	b := make([]byte, NodeKeySize)
	binary.BigEndian.PutUint64(b[:8], uint64(nk.Version))
	binary.BigEndian.PutUint32(b[8:], nk.Nonce)
	return b
}

// GetNodeKey deserializes a NodeKey from a 12-byte slice.
func GetNodeKey(key []byte) *NodeKey {
	if len(key) != NodeKeySize {
		return nil
	}
	return &NodeKey{
		Version: int64(binary.BigEndian.Uint64(key[:8])),
		Nonce:   binary.BigEndian.Uint32(key[8:]),
	}
}

// GetRootKey returns the NodeKey used to store the root reference
// for a given version. By convention, the root uses nonce=1.
func GetRootKey(version int64) []byte {
	nk := &NodeKey{Version: version, Nonce: 1}
	return nk.GetKey()
}
