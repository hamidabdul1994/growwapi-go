package growwapi

import "net/url"

// CLI / REST adapters. Provides stable names and helpers so tool authors
// can call into the SDK without knowing every internal Go identifier.

// OrderStatusRequestShim unifies the two order-status request shapes behind
// a single interface so callers can pass either OrderStatusRequestWith…
// to Client.GetOrderStatus.
type OrderStatusRequestShim interface {
	url() string
	queryParams() url.Values
}

// Ensure interface satisfaction at compile time.
var (
	_ OrderStatusRequestShim = OrderStatusRequestWithGrowwOrderId{}
	_ OrderStatusRequestShim = OrderStatusRequestWithOrderReferenceId{}
)

// Aliases / shims to keep CLI / REST request types simple.
type (
	PlaceSmartOrderRequest    = CreateGttOrderRequest
	ModifySmartOrderRequest   = ModifyGttOrderRequest

	PositionsForTradingSymbolRequest = GetPositionBySymbolRequest

	MarginForOrderRequest = RequiredMarginRequest
)

// GreekRequest is an alias keeping the CLI naming consistent.
type GreekRequest = GetGreeksRequest

// Live-data request aliases.
type (
	GetQuoteRequest        = QuoteRequest
	GetLTPRequest          = LtpRequest
	GetOHLCRequest         = OhlcRequest
	GetOptionChainRequestV = GetOptionChainRequest
)

// Backtesting request aliases.
type (
	ExpiriesRequest              = GetExpiriesRequest
	HistoricalContractRequest    = GetContractsRequest
	HistoricalCandleRequest      = GetHistoricalCandlesRequest
)
