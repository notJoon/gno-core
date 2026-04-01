package types

import (
	"math"

	"github.com/gnolang/gno/tm2/pkg/overflow"
)

// Gas consumption descriptors.
const (
	GasIterNextCostFlatDesc = "IterNextFlat"
	GasValuePerByteDesc     = "ValuePerByte"
	GasWritePerByteDesc     = "WritePerByte"
	GasReadPerByteDesc      = "ReadPerByte"
	GasWriteCostFlatDesc    = "WriteFlat"
	GasReadCostFlatDesc     = "ReadFlat"
	GasHasDesc              = "Has"
	GasDeleteDesc           = "Delete"
)

// GasContext carries a gas meter and config through the store stack.
// All methods are nil-safe: if gctx is nil, they are no-ops (returning
// 0 for methods that return Gas).
type GasContext struct {
	Meter  GasMeter
	Config GasConfig
}

// WillGet charges ReadCostFlat. Used by non-depth stores only;
// depth stores use ConsumeGas directly.
func (gctx *GasContext) WillGet() {
	if gctx == nil {
		return
	}
	gctx.Meter.ConsumeGas(gctx.Config.ReadCostFlat, GasReadCostFlatDesc)
}

// DidGet charges ReadCostPerByte * len(bz).
func (gctx *GasContext) DidGet(bz []byte) {
	if gctx == nil {
		return
	}
	gas := overflow.Mulp(gctx.Config.ReadCostPerByte, Gas(len(bz)))
	gctx.Meter.ConsumeGas(gas, GasReadPerByteDesc)
}

// WillSet charges WriteCostFlat + WriteCostPerByte * len(bz).
// Returns the total amount charged.
func (gctx *GasContext) WillSet(bz []byte) Gas {
	if gctx == nil {
		return 0
	}
	flat := gctx.Config.WriteCostFlat
	perByte := overflow.Mulp(gctx.Config.WriteCostPerByte, Gas(len(bz)))
	total := overflow.Addp(flat, perByte)
	gctx.Meter.ConsumeGas(total, GasWriteCostFlatDesc)
	return total
}

// WillDelete charges DeleteCost. Returns the amount charged.
func (gctx *GasContext) WillDelete() Gas {
	if gctx == nil {
		return 0
	}
	gctx.Meter.ConsumeGas(gctx.Config.DeleteCost, GasDeleteDesc)
	return gctx.Config.DeleteCost
}

// RefundGas refunds previously charged gas.
func (gctx *GasContext) RefundGas(amount Gas) {
	if gctx == nil {
		return
	}
	gctx.Meter.RefundGas(amount, "Refund")
}

// ConsumeGas charges gas directly.
func (gctx *GasContext) ConsumeGas(amount Gas, descriptor string) {
	if gctx == nil {
		return
	}
	gctx.Meter.ConsumeGas(amount, descriptor)
}

// WillIterator charges flat seek cost for iterator creation.
func (gctx *GasContext) WillIterator() {
	if gctx == nil {
		return
	}
	gctx.Meter.ConsumeGas(gctx.Config.IterNextCostFlat, GasIterNextCostFlatDesc)
}

// WillIterNext charges flat cost per iteration step.
func (gctx *GasContext) WillIterNext() {
	if gctx == nil {
		return
	}
	gctx.Meter.ConsumeGas(gctx.Config.IterNextCostFlat, GasIterNextCostFlatDesc)
}

// DepthEstimator is implemented by stores that have depth-dependent
// I/O cost (e.g., IAVL trees). The expected depth is used by
// cache.Store to estimate gas for reads/writes.
type DepthEstimator interface {
	ExpectedDepth() int64
}

// Gas measured by the SDK
type Gas = int64

// OutOfGasError defines an error thrown when an action results in out of gas.
type OutOfGasError struct {
	Descriptor string
}

func (oog OutOfGasError) Error() string {
	return "out of gas in location: " + oog.Descriptor
}

// GasOverflowError defines an error thrown when an action results gas consumption
// unsigned integer overflow.
type GasOverflowError struct {
	Descriptor string
}

func (oog GasOverflowError) Error() string {
	return "gas overflow in location: " + oog.Descriptor
}

// GasMeter interface to track gas consumption
type GasMeter interface {
	GasConsumed() Gas
	GasConsumedToLimit() Gas
	Limit() Gas
	Remaining() Gas
	ConsumeGas(amount Gas, descriptor string)
	RefundGas(amount Gas, descriptor string)
	IsPastLimit() bool
	IsOutOfGas() bool
}

//----------------------------------------
// basicGasMeter

type basicGasMeter struct {
	limit    Gas
	consumed Gas
}

// NewGasMeter returns a reference to a new basicGasMeter.
func NewGasMeter(limit Gas) *basicGasMeter {
	if limit < 0 {
		panic("gas must not be negative")
	}
	return &basicGasMeter{
		limit:    limit,
		consumed: 0,
	}
}

func (g *basicGasMeter) GasConsumed() Gas {
	return g.consumed
}

func (g *basicGasMeter) Limit() Gas {
	return g.limit
}

func (g *basicGasMeter) Remaining() Gas {
	return overflow.Subp(g.Limit(), g.GasConsumedToLimit())
}

func (g *basicGasMeter) GasConsumedToLimit() Gas {
	if g.IsPastLimit() {
		return g.limit
	}
	return g.consumed
}

// TODO rename to DidConsumeGas.
func (g *basicGasMeter) ConsumeGas(amount Gas, descriptor string) {
	if amount < 0 {
		panic("gas must not be negative")
	}
	consumed, ok := overflow.Add(g.consumed, amount)
	if !ok {
		panic(GasOverflowError{descriptor})
	}
	// consume gas even if out of gas.
	// corollary, call (Did)ConsumeGas after consumption.
	g.consumed = consumed
	if consumed > g.limit {
		panic(OutOfGasError{descriptor})
	}
}

func (g *basicGasMeter) RefundGas(amount Gas, descriptor string) {
	if amount < 0 {
		panic("gas must not be negative")
	}
	g.consumed -= amount
	if g.consumed < 0 {
		g.consumed = 0
	}
}

func (g *basicGasMeter) IsPastLimit() bool {
	return g.consumed > g.limit
}

func (g *basicGasMeter) IsOutOfGas() bool {
	return g.consumed >= g.limit
}

//----------------------------------------
// infiniteGasMeter

type infiniteGasMeter struct {
	consumed Gas
}

// NewInfiniteGasMeter returns a reference to a new infiniteGasMeter.
func NewInfiniteGasMeter() GasMeter {
	return &infiniteGasMeter{
		consumed: 0,
	}
}

func (g *infiniteGasMeter) GasConsumed() Gas {
	return g.consumed
}

func (g *infiniteGasMeter) GasConsumedToLimit() Gas {
	return g.consumed
}

func (g *infiniteGasMeter) Limit() Gas {
	return 0
}

func (g *infiniteGasMeter) Remaining() Gas {
	return math.MaxInt64
}

func (g *infiniteGasMeter) ConsumeGas(amount Gas, descriptor string) {
	consumed, ok := overflow.Add(g.consumed, amount)
	if !ok {
		panic(GasOverflowError{descriptor})
	}
	g.consumed = consumed
}

func (g *infiniteGasMeter) RefundGas(amount Gas, descriptor string) {
	if amount < 0 {
		panic("gas must not be negative")
	}
	g.consumed -= amount
	if g.consumed < 0 {
		g.consumed = 0
	}
}

func (g *infiniteGasMeter) IsPastLimit() bool {
	return false
}

func (g *infiniteGasMeter) IsOutOfGas() bool {
	return false
}

//----------------------------------------
// passthroughGasMeter

type passthroughGasMeter struct {
	Base GasMeter
	Head *basicGasMeter
}

// NewPassthroughGasMeter has a head basicGasMeter, but also passes through
// consumption to a base basicGasMeter.  Limit must be less than
// base.Remaining().
func NewPassthroughGasMeter(base GasMeter, limit int64) passthroughGasMeter {
	if limit < 0 {
		panic("gas must not be negative")
	}
	// limit > base.Remaining() is not checked; so that a panic happens when
	// gas is actually consumed.
	return passthroughGasMeter{
		Base: base,
		Head: NewGasMeter(limit),
	}
}

func (g passthroughGasMeter) GasConsumed() Gas {
	return g.Head.GasConsumed()
}

func (g passthroughGasMeter) Limit() Gas {
	return g.Head.Limit()
}

func (g passthroughGasMeter) Remaining() Gas {
	return g.Head.Remaining()
}

func (g passthroughGasMeter) GasConsumedToLimit() Gas {
	return g.Head.GasConsumedToLimit()
}

func (g passthroughGasMeter) ConsumeGas(amount Gas, descriptor string) {
	g.Base.ConsumeGas(amount, descriptor)
	g.Head.ConsumeGas(amount, descriptor)
}

func (g passthroughGasMeter) RefundGas(amount Gas, descriptor string) {
	g.Base.RefundGas(amount, descriptor)
	g.Head.RefundGas(amount, descriptor)
}

func (g passthroughGasMeter) IsPastLimit() bool {
	return g.Head.IsPastLimit()
}

func (g passthroughGasMeter) IsOutOfGas() bool {
	return g.Head.IsOutOfGas()
}

//----------------------------------------

// GasConfig defines gas cost for each operation on KVStores.
type GasConfig struct {
	HasCost          Gas
	DeleteCost       Gas
	ReadCostFlat     Gas
	ReadCostPerByte  Gas
	WriteCostFlat    Gas
	WriteCostPerByte Gas
	IterNextCostFlat Gas
	MinDepth         Gas // floor for DepthEstimator (0 = no floor)
}

// DefaultGasConfig returns a default gas config for KVStores.
// These are the tm2 defaults (uncalibrated legacy values from Cosmos SDK).
func DefaultGasConfig() GasConfig {
	return GasConfig{
		HasCost:          1000,
		DeleteCost:       1000,
		ReadCostFlat:     1000,
		ReadCostPerByte:  3,
		WriteCostFlat:    2000,
		WriteCostPerByte: 30,
		IterNextCostFlat: 30,
		MinDepth:         0,
	}
}
