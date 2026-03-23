# Gno Storage Architecture Deep Dive

This document provides the full technical details behind the storage optimization rules in the main SKILL.md. Read this when you need to understand *why* a particular optimization works.

## Storage Architecture

```
Gno contract code (realm)
       |
   Store Interface (gnovm/pkg/gnolang/store.go:42-86)
       |
   defaultStore (in-memory caches + gas metering)
       |
   +-------+-------+
   |               |
baseStore       iavlStore
(objects/types)  (escaped hashes, MemPackage)
   |               |
 cache.Store    iavl.Store
   |               |
 gas.Store      gas.Store
   |               |
 dbadapter      IAVL merkle tree
   |
  LevelDB/RocksDB
```

### Key source files

| File | Role |
|------|------|
| `gnovm/pkg/gnolang/store.go` | Store interface, GasConfig, SetObject/GetObject |
| `gnovm/pkg/gnolang/realm.go` | Realm transaction lifecycle, dirty tracking, copyValueWithRefs |
| `gnovm/pkg/gnolang/ownership.go` | ObjectID, ObjectInfo, Object interface |
| `tm2/pkg/store/types/gas.go` | KV Store gas config (DefaultGasConfig) |
| `tm2/pkg/store/gas/store.go` | Gas metering wrapper |
| `tm2/pkg/store/iavl/store.go` | IAVL merkle tree storage |
| `gno.land/pkg/sdk/vm/params.go` | Storage deposit price (100 ugnot/byte) |

## Object persistence pipeline

```
1. Contract creates/modifies a variable
       |
2. Realm.DidUpdate() — marks objects dirty
       |
3. OpReturn / FinalizeRealmTransaction()
   |-- processNewCreatedMarks(): assign ObjectIDs
   |-- markDirtyAncestors(): propagate dirty up owner chain
   |-- saveUnsavedObjects():
   |   |-- copyValueWithRefs(): convert child Objects -> RefValue
   |   |-- amino.MustMarshalAny(): binary serialization
   |   |-- HashBytes(): SHA256 hash (20 bytes)
   |   |-- baseStore.Set(key, hash+bytes): actual storage
   |   '-- gas consumed: GasSetObject * len(bytes)
   |-- removeDeletedObjects(): delete processing
   '-- clearMarks(): reset state
       |
4. Storage deposit settlement (100 ugnot/byte)
```

### Key format

```
Objects:     "oid:{PkgID_hex}:{NewTime}"       -> [hash(20b) + amino_bytes]
Realm meta:  "oid:{PkgID_hex}:{NewTime}#realm" -> amino(Realm)
Types:       "tid:{TypeID}"                     -> amino(Type)
BlockNodes:  "node:{Location}"                  -> amino(BlockNode)
MemPackage:  "pkg:{path}"                       -> amino(MemPackage) (in iavlStore)
```

### Serialization transform (copyValueWithRefs)

Location: `gnovm/pkg/gnolang/realm.go:1324-1482`

```
In-memory state:
  ArrayValue{ List: [StructObj1, StructObj2, StructObj3] }

After serialization transform:
  ArrayValue{ List: [RefValue{oid1}, RefValue{oid2}, RefValue{oid3}] }
```

- Primitive values (string, int, bigint): copied inline
- Objects (Array, Struct, Map, Func, Block, etc.): converted to RefValue and stored independently
- Each Object becomes a separate key-value pair

## Gas cost structure (dual charging)

### VM-level gas (gnolang Store)

Location: `gnovm/pkg/gnolang/store.go:126-138`

| Operation | Gas cost | Unit |
|-----------|----------|------|
| GetObject | 16 | per byte |
| SetObject | 16 | per byte |
| GetType | 5 | per byte |
| SetType | 52 | per byte |
| GetPackageRealm | 524 | per byte |
| SetPackageRealm | 524 | per byte |
| AddMemPackage | 8 | per byte |
| GetMemPackage | 8 | per byte |
| DeleteObject | 3,715 | flat |

Formula: `gas = cost_per_byte * len(amino_serialized_bytes)`

### KV Store-level gas (tm2 Store)

Location: `tm2/pkg/store/types/gas.go:229-238`

| Operation | Gas cost | Unit |
|-----------|----------|------|
| Has | 1,000 | flat |
| Delete | 1,000 | flat |
| Read (Get) | 1,000 + 3/byte | flat + per byte |
| Write (Set) | 2,000 + 30/byte | flat + per byte |
| Iterator.Next | 30 + 3/byte | flat + per byte |

### Cost comparison table

| Component | Read (Get) | Write (Set) | Ratio |
|-----------|-----------|-------------|-------|
| VM gas (per byte) | 16 | 16 | 1:1 |
| KV flat gas | 1,000 | 2,000 | 1:2 |
| KV per-byte gas | 3 | 30 | 1:10 |
| Storage deposit | none | 100 ugnot/byte | - |

### Storage deposit

Location: `gno.land/pkg/sdk/vm/params.go`

```
100 ugnot per byte (= 1 GNOT per 10KB)
```

- GNOT locked on storage, refunded on deletion
- Tracked per realm, not per user

## Why storage is expensive: root causes

### 1. Dirty ancestor propagation

Location: `gnovm/pkg/gnolang/realm.go` — `markDirtyAncestors()`

Modifying a single field causes the **entire owner chain** to be re-serialized:

```
PackageValue (root owner)
  '-- Block (package block)
      '-- StructValue (variable A)
          '-- ArrayValue (field X) <-- only this modified
              '-- StructValue (element 1)

Result: element1 + ArrayValue + StructValue + Block + PackageValue all re-serialized
```

This happens because each owner's hash depends on its children's hashes (merkle tree property).

### 2. Amino serialization overhead

- Type information included with every object (`amino.MustMarshalAny`)
- RefValue contains ObjectID as string: `"{32byte_hex}:{uint64}"` = ~70+ bytes per reference
- Deterministic encoding prevents compression optimizations

### 3. Per-Object storage inefficiency

Each Object is an independent KV pair:
- KV Write flat cost (2,000 gas) per Object
- Independent hash computation per Object
- Small structs have high overhead ratio

### 4. Dual gas charging

VM-level gas (SetObject per-byte) and KV Store-level gas (Write flat + per-byte) are both charged for every storage operation.

## Measurement tools

### Gas simulation

```bash
gnokey maketx call \
  -pkgpath gno.land/r/your/realm \
  -func YourFunction \
  -args "arg1" \
  -gas-wanted 10000000 \
  -gas-fee 1000000ugnot \
  -remote https://rpc.gno.land:443 \
  -broadcast \
  -simulate only \
  YOUR_KEY
```

### Storage query

```bash
gnokey query vm/qstorage -data "gno.land/r/your/realm" -remote https://rpc.gno.land:443
```

### Store operation log (opslog)

Location: `gnovm/pkg/gnolang/store.go:163`

Setting a writer on `defaultStore.opslog` logs all store operations:
- `c[oid](diff)=...` — new object created
- `u[oid](diff)=...` — object updated (with diff)

### Benchops framework

Location: `gnovm/pkg/benchops/`

Build-time flags `bm.OpsEnabled` / `bm.StorageEnabled` enable per-operation timing and size measurement.

### Direct serialization size check

```go
bz := amino.MustMarshalAny(yourObject)
fmt.Printf("serialized size: %d bytes\n", len(bz))
fmt.Printf("estimated gas (SetObject): %d\n", 16*len(bz))
fmt.Printf("estimated gas (KV Write): %d\n", 2000+30*(len(bz)+20))
fmt.Printf("storage deposit: %d ugnot\n", 100*(len(bz)+20))
```
