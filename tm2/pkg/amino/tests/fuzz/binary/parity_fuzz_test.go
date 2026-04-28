package fuzzbinary

// Run on demand:
//
//	go test -fuzz=FuzzCodecParity -fuzztime=15m ./tm2/pkg/amino/tests/fuzz/binary/
//
// Drives both the reflect codec and the genproto2 fast path on the same wire
// input. For every registered target type that successfully decodes the
// input, AssertCodecParity verifies four invariants:
//
//	(1) MarshalReflect(v) == MarshalBinary2(v)
//	(2) SizeBinary2(v) == len(MarshalBinary2(v))
//	(3) UnmarshalReflect and UnmarshalBinary2 produce DeepEqual values
//	(4) Round-trip fidelity: decoded value DeepEquals the input
//
// This is the auto-discovery counterpart to TestCodecParity_AminoFixtures —
// hand-curated fixtures cover known-tricky cases; the fuzzer explores the
// rest of the wire-format space looking for cases where the two codecs drift.

import (
	"math"
	"reflect"
	"testing"

	amino "github.com/gnolang/gno/tm2/pkg/amino"
	"github.com/gnolang/gno/tm2/pkg/amino/aminotest"
	"github.com/gnolang/gno/tm2/pkg/amino/tests"
)

func FuzzCodecParity(f *testing.F) {
	cdc := amino.NewCodec()
	cdc.RegisterPackage(tests.Package)
	cdc.Seal()

	seed := func(pbm amino.PBMessager2) {
		bz, err := cdc.MarshalBinary2(pbm)
		if err != nil {
			f.Fatalf("seed marshal failed: %v", err)
		}
		f.Add(bz)
	}

	// Seeds covering the recent regression surface (#5590).
	seed(&tests.InterfaceHeavy{
		Field1: tests.Concrete1{},
		Field2: tests.Concrete2{},
		Items:  []tests.Interface1{tests.Concrete1{}, tests.Concrete2{}},
		Name:   "fuzz",
	})
	seed(&tests.GnoVMTypedValue{
		T: tests.Concrete1{},
		V: tests.Concrete2{},
		N: [8]byte{1, 2, 3, 4, 5, 6, 7, 8},
	})
	seed(&tests.AminoMarshalerStruct1{A: 7, B: -3})
	seed(&tests.FuzzFixedInt{I64: math.MinInt64, U64: math.MaxUint64})
	seed(&tests.FuzzFixedInt{I64: -1, U64: 1})
	seed(&tests.FuzzNilElements{
		Entries: []*tests.FuzzFieldInfo{
			{Name: "first", Embedded: true, Tag: "x:1", Index: 1},
			nil,
			{Name: "third", Index: 3},
		},
		Poses: []*tests.GnoVMPos{
			{Line: 7, Column: 9},
			nil,
		},
	})

	// Decode targets: every PBMessager2-registered type the fuzzer may try
	// to interpret the wire bytes as. AssertCodecParity is invoked for each
	// target that decodes successfully, so a single mutated input can exercise
	// parity for several typed shapes at once.
	//
	// FuzzNilElements is intentionally excluded: with amino:"nil_elements",
	// nil and non-nil-zero pointers share the same wire encoding, so strict
	// round-trip fidelity (AssertCodecParity invariant 4) cannot hold and
	// would produce false positives.
	targets := []reflect.Type{
		reflect.TypeFor[tests.InterfaceHeavy](),
		reflect.TypeFor[tests.GnoVMTypedValue](),
		reflect.TypeFor[tests.GnoVMBlock](),
		reflect.TypeFor[tests.GnoVMNode](),
		reflect.TypeFor[tests.AminoMarshalerStruct1](),
		reflect.TypeFor[tests.FuzzFixedInt](),
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		for _, target := range targets {
			ptr := reflect.New(target).Interface()
			pbm, ok := ptr.(amino.PBMessager2)
			if !ok {
				continue
			}
			if err := pbm.UnmarshalBinary2(cdc, data, 0); err != nil {
				continue
			}
			aminotest.AssertCodecParity(t, cdc, ptr)
		}
	})
}
