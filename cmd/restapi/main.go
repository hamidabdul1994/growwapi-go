// Command growwapi-rest exposes every Groww Trade API as an HTTP REST
// service backed by the growwapi-go SDK. Configuration (access token,
// listen addr, etc.) can come from flags, environment variables, or a
// YAML config file — see Config struct for the full list.
//
// Once running, the routes mirror the SDK module names:
//
//   GET    /healthz
//   GET    /configz
//   GET    /v1/user/profile
//   POST   /v1/orders                     (JSON body = PlaceOrderRequest)
//   PUT    /v1/orders/:growwOrderId
//   DELETE /v1/orders/:growwOrderId?segment=CASH
//   GET    /v1/orders                     (?segment=&page=&page_size=)
//   GET    /v1/orders/:growwOrderId       (?segment=)
//   GET    /v1/orders/:growwOrderId/trades
//   POST   /v1/orders/status              (body picks groww vs reference id)
//   GET    /v1/portfolio/holdings
//   GET    /v1/portfolio/positions        (?segment=)
//   GET    /v1/portfolio/positions/:tradingSymbol
//   GET    /v1/margin/detail
//   POST   /v1/margin/orders              (?segment=&orders=[...])
//   GET    /v1/livedata/quote             (?exchange=&segment=&trading_symbol=)
//   GET    /v1/livedata/ltp               (?segment=&exchange_symbols=A,B)
//   GET    /v1/livedata/ohlc              (?segment=&exchange_symbols=A,B)
//   GET    /v1/livedata/greeks
//   GET    /v1/livedata/option-chain
//   GET    /v1/backtesting/expiries
//   GET    /v1/backtesting/contracts
//   GET    /v1/backtesting/candles
//   GET    /v1/instruments
//   POST   /v1/smart-orders
//   PUT    /v1/smart-orders/:id
//   DELETE /v1/smart-orders/:id
//   GET    /v1/smart-orders/:id
//   GET    /v1/smart-orders
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	groww "github.com/rctrj/growwapi-go"
)

func main() {
	cfg := groww.NewConfig()
	if err := cfg.Load(groww.LoadOptions{}); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}

	addr := cfg.RESTListenAddr
	if v := os.Getenv("PORT"); v != "" && cfg.RESTListenAddr == ":8080" {
		addr = ":" + v
	}

	client := groww.NewClientFromConfig(cfg)
	srv := &server{cfg: cfg, client: &client}

	mux := http.NewServeMux()
	srv.register(mux)

	src, _ := srv.cfg.SourceOf("access_token")
	log.Printf("growwapi-rest listening on %s (token source=%s)", addr, src)
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           withLogging(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}
	if err := httpSrv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

type server struct {
	cfg    *groww.Config
	client *groww.Client
}

func (s *server) register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/configz", s.handleConfig)

	mux.HandleFunc("/v1/user/profile", s.handleUserProfile)

	mux.HandleFunc("/v1/orders", s.handleOrdersCollection)
	mux.HandleFunc("/v1/orders/", s.handleOrdersItem)

	mux.HandleFunc("/v1/orders/status", s.handleOrderStatus)

	mux.HandleFunc("/v1/portfolio/holdings", s.handleHoldings)
	mux.HandleFunc("/v1/portfolio/positions", s.handlePositions)
	mux.HandleFunc("/v1/portfolio/positions/", s.handlePositionBySymbol)

	mux.HandleFunc("/v1/margin/detail", s.handleUserMargin)
	mux.HandleFunc("/v1/margin/orders", s.handleMarginOrders)

	mux.HandleFunc("/v1/livedata/quote", s.handleQuote)
	mux.HandleFunc("/v1/livedata/ltp", s.handleLtp)
	mux.HandleFunc("/v1/livedata/ohlc", s.handleOhlc)
	mux.HandleFunc("/v1/livedata/greeks", s.handleGreeks)
	mux.HandleFunc("/v1/livedata/option-chain", s.handleOptionChain)

	mux.HandleFunc("/v1/backtesting/expiries", s.handleExpiries)
	mux.HandleFunc("/v1/backtesting/contracts", s.handleContracts)
	mux.HandleFunc("/v1/backtesting/candles", s.handleCandles)

	mux.HandleFunc("/v1/instruments", s.handleInstruments)

	mux.HandleFunc("/v1/smart-orders", s.handleSmartOrdersCollection)
	mux.HandleFunc("/v1/smart-orders/", s.handleSmartOrdersItem)
}

// ---------- helpers ----------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

func writeSDKError(w http.ResponseWriter, err error) {
	var ge groww.Error
	if errors.As(err, &ge) {
		writeJSON(w, http.StatusBadRequest, ge)
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
}

func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return nil
	}
	return json.NewDecoder(bytes.NewReader(body)).Decode(dst)
}

func withLogging(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &statusRecorder{ResponseWriter: w, status: 200}
		h.ServeHTTP(ww, r)
		log.Printf("%-7s %s -> %d (%s)", r.Method, r.URL.Path, ww.status, time.Since(start))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(c int) { s.status = c; s.ResponseWriter.WriteHeader(c) }

// ---------- handlers ----------

func (s *server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{"status": "ok"})
}

func (s *server) handleConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{
		"values":  s.cfg.Snapshot(),
		"sources": s.cfg.Sources(),
	})
}

func (s *server) handleUserProfile(w http.ResponseWriter, r *http.Request) {
	p, err := s.client.GetUserProfile(r.Context())
	if err != nil {
		writeSDKError(w, err)
		return
	}
	writeJSON(w, 200, p)
}

func (s *server) handleOrdersCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req groww.PlaceOrderRequest
		if err := decodeJSON(r, &req); err != nil {
			writeSDKError(w, err)
			return
		}
		out, err := s.client.PlaceOrder(r.Context(), req)
		if err != nil {
			writeSDKError(w, err)
			return
		}
		writeJSON(w, 201, out)
	case http.MethodGet:
		q := r.URL.Query()
		out, err := s.client.ListOrders(r.Context(), groww.ListOrdersRequest{
			Segment:  groww.Segment(q.Get("segment")),
			Page:     atoiDefault(q.Get("page"), 0),
			PageSize: atoiDefault(q.Get("page_size"), 0),
		})
		if err != nil {
			writeSDKError(w, err)
			return
		}
		writeJSON(w, 200, out)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// /v1/orders/{growwOrderId}[/trades]
// /v1/orders/{growwOrderId}?segment=CASH        -> detail
// /v1/orders/{growwOrderId} PUT                 -> modify
// /v1/orders/{growwOrderId} DELETE              -> cancel
// /v1/orders/{growwOrderId}/trades              -> trades
func (s *server) handleOrdersItem(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/v1/orders/")
	if id == "" {
		http.NotFound(w, r)
		return
	}
	if strings.HasSuffix(id, "/trades") {
		id = strings.TrimSuffix(id, "/trades")
		q := r.URL.Query()
		page := atoiDefault(q.Get("page"), 0)
		pageSize := atoiDefault(q.Get("page_size"), 0)
		out, err := s.client.GetTradesForOrder(r.Context(), groww.TradesForOrderRequest{
			GrowwOrderId: id,
			Segment:      groww.Segment(q.Get("segment")),
			Page:         page, PageSize: pageSize,
		})
		if err != nil {
			writeSDKError(w, err)
			return
		}
		writeJSON(w, 200, out)
		return
	}

	switch r.Method {
	case http.MethodGet:
		q := r.URL.Query()
		out, err := s.client.GetOrderDetails(r.Context(), groww.GetOrderDetailsRequest{
			GrowwOrderId: id, Segment: groww.Segment(q.Get("segment")),
		})
		if err != nil {
			writeSDKError(w, err)
			return
		}
		writeJSON(w, 200, out)
	case http.MethodPut:
		var req groww.ModifyOrderRequest
		req.GrowwOrderId = id
		if err := decodeJSON(r, &req); err != nil {
			writeSDKError(w, err)
			return
		}
		out, err := s.client.ModifyOrder(r.Context(), req)
		if err != nil {
			writeSDKError(w, err)
			return
		}
		writeJSON(w, 200, out)
	case http.MethodDelete:
		q := r.URL.Query()
		out, err := s.client.CancelOrder(r.Context(), groww.CancelOrderRequest{
			GrowwOrderId: id, Segment: groww.Segment(q.Get("segment")),
		})
		if err != nil {
			writeSDKError(w, err)
			return
		}
		writeJSON(w, 200, out)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *server) handleOrderStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeSDKError(w, err)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	var probe struct {
		GrowwOrderId     string `json:"groww_order_id"`
		OrderReferenceId string `json:"order_reference_id"`
		Segment          string `json:"segment"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		writeSDKError(w, err)
		return
	}
	var (
		out groww.OrderStatusResponse
		gerr error
	)
	if probe.GrowwOrderId != "" {
		out, gerr = s.client.GetOrderStatusByGrowwOrderId(r.Context(), probe.GrowwOrderId, groww.Segment(probe.Segment))
	} else if probe.OrderReferenceId != "" {
		out, gerr = s.client.GetOrderStatusByOrderReferenceId(r.Context(), probe.OrderReferenceId, groww.Segment(probe.Segment))
	} else {
		writeSDKError(w, errors.New("either groww_order_id or order_reference_id required"))
		return
	}
	if gerr != nil {
		writeSDKError(w, gerr)
		return
	}
	writeJSON(w, 200, out)
}

func (s *server) handleHoldings(w http.ResponseWriter, r *http.Request) {
	out, err := s.client.GetHoldings(r.Context())
	if err != nil {
		writeSDKError(w, err)
		return
	}
	writeJSON(w, 200, out)
}

func (s *server) handlePositions(w http.ResponseWriter, r *http.Request) {
	out, err := s.client.GetPositions(r.Context(), groww.GetPositionsRequest{Segment: groww.Segment(r.URL.Query().Get("segment"))})
	if err != nil {
		writeSDKError(w, err)
		return
	}
	writeJSON(w, 200, out)
}

func (s *server) handlePositionBySymbol(w http.ResponseWriter, r *http.Request) {
	sym := strings.TrimPrefix(r.URL.Path, "/v1/portfolio/positions/")
	if sym == "" {
		http.Error(w, "trading symbol required", http.StatusBadRequest)
		return
	}
	out, err := s.client.GetPositionBySymbol(r.Context(), groww.GetPositionBySymbolRequest{TradingSymbol: sym})
	if err != nil {
		writeSDKError(w, err)
		return
	}
	writeJSON(w, 200, out)
}

func (s *server) handleUserMargin(w http.ResponseWriter, r *http.Request) {
	out, err := s.client.GetUserMargin(r.Context())
	if err != nil {
		writeSDKError(w, err)
		return
	}
	writeJSON(w, 200, out)
}

func (s *server) handleMarginOrders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var orders []groww.OrderMarginRequest
	if err := decodeJSON(r, &orders); err != nil {
		writeSDKError(w, err)
		return
	}
	out, err := s.client.GetRequiredMargin(r.Context(), groww.RequiredMarginRequest{
		Segment: groww.Segment(r.URL.Query().Get("segment")),
		Orders:  orders,
	})
	if err != nil {
		writeSDKError(w, err)
		return
	}
	writeJSON(w, 200, out)
}

func (s *server) handleQuote(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	out, err := s.client.GetQuote(r.Context(), groww.QuoteRequest{
		Exchange:      groww.Exchange(q.Get("exchange")),
		Segment:       groww.Segment(q.Get("segment")),
		TradingSymbol: q.Get("trading_symbol"),
	})
	if err != nil {
		writeSDKError(w, err)
		return
	}
	writeJSON(w, 200, out)
}

func (s *server) handleLtp(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	syms := split(q.Get("exchange_symbols"), ",")
	out, err := s.client.GetLtp(r.Context(), groww.LtpRequest{Segment: groww.Segment(q.Get("segment")), ExchangeSymbols: syms})
	if err != nil {
		writeSDKError(w, err)
		return
	}
	writeJSON(w, 200, out)
}

func (s *server) handleOhlc(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	syms := split(q.Get("exchange_symbols"), ",")
	out, err := s.client.GetOhlc(r.Context(), groww.OhlcRequest{Segment: groww.Segment(q.Get("segment")), ExchangeSymbols: syms})
	if err != nil {
		writeSDKError(w, err)
		return
	}
	writeJSON(w, 200, out)
}

func (s *server) handleGreeks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	exp, err := time.Parse("2006-01-02", q.Get("expiry"))
	if err != nil {
		writeSDKError(w, fmt.Errorf("--expiry YYYY-MM-DD required: %w", err))
		return
	}
	out, err := s.client.GetGreeks(r.Context(), groww.GetGreeksRequest{
		Exchange:      q.Get("exchange"),
		Underlying:    q.Get("underlying"),
		TradingSymbol: q.Get("trading_symbol"),
		Expiry:        exp,
	})
	if err != nil {
		writeSDKError(w, err)
		return
	}
	writeJSON(w, 200, out)
}

func (s *server) handleOptionChain(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	exp, err := time.Parse("2006-01-02", q.Get("expiry_date"))
	if err != nil {
		writeSDKError(w, fmt.Errorf("--expiry_date YYYY-MM-DD required: %w", err))
		return
	}
	out, err := s.client.GetOptionChain(r.Context(), groww.GetOptionChainRequest{
		Exchange: groww.Exchange(q.Get("exchange")),
		Underlying: q.Get("underlying"),
		ExpiryDate: exp,
	})
	if err != nil {
		writeSDKError(w, err)
		return
	}
	writeJSON(w, 200, out)
}

func (s *server) handleExpiries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	out, err := s.client.GetExpiries(r.Context(), groww.GetExpiriesRequest{
		Exchange:         groww.Exchange(q.Get("exchange")),
		UnderlyingSymbol: q.Get("underlying_symbol"),
		Year:             atoiDefault(q.Get("year"), 0),
		Month:            atoiDefault(q.Get("month"), 0),
	})
	if err != nil {
		writeSDKError(w, err)
		return
	}
	writeJSON(w, 200, out)
}

func (s *server) handleContracts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	exp, err := time.Parse("2006-01-02", q.Get("expiry_date"))
	if err != nil {
		writeSDKError(w, fmt.Errorf("--expiry_date YYYY-MM-DD required: %w", err))
		return
	}
	out, err := s.client.GetContracts(r.Context(), groww.GetContractsRequest{
		Exchange:         groww.Exchange(q.Get("exchange")),
		UnderlyingSymbol: q.Get("underlying_symbol"),
		ExpiryDate:       exp,
	})
	if err != nil {
		writeSDKError(w, err)
		return
	}
	writeJSON(w, 200, out)
}

func (s *server) handleCandles(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	start, err := time.Parse("2006-01-02 15:04:05", q.Get("start_time"))
	if err != nil {
		writeSDKError(w, fmt.Errorf("--start_time YYYY-MM-DD HH:MM:SS required: %w", err))
		return
	}
	end, err := time.Parse("2006-01-02 15:04:05", q.Get("end_time"))
	if err != nil {
		writeSDKError(w, fmt.Errorf("--end_time YYYY-MM-DD HH:MM:SS required: %w", err))
		return
	}
	out, err := s.client.GetHistoricalCandles(r.Context(), groww.GetHistoricalCandlesRequest{
		Exchange:       groww.Exchange(q.Get("exchange")),
		Segment:        groww.Segment(q.Get("segment")),
		GrowwSymbol:    q.Get("groww_symbol"),
		StartTime:      start,
		EndTime:        end,
		CandleInterval: groww.CandleInterval(q.Get("candle_interval")),
	})
	if err != nil {
		writeSDKError(w, err)
		return
	}
	writeJSON(w, 200, out)
}

func (s *server) handleInstruments(w http.ResponseWriter, r *http.Request) {
	out, err := s.client.Instruments(r.Context())
	if err != nil {
		writeSDKError(w, err)
		return
	}
	writeJSON(w, 200, out)
}

func (s *server) handleSmartOrdersCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeSDKError(w, err)
			return
		}
		out, err := dispatchSmartOrderCreate(r.Context(), s.client, body)
		if err != nil {
			writeSDKError(w, err)
			return
		}
		writeJSON(w, 201, out)
	case http.MethodGet:
		q := r.URL.Query()
		out, err := s.client.ListSmartOrders(r.Context(), groww.ListSmartOrdersRequest{
			Segment:        groww.Segment(q.Get("segment")),
			SmartOrderType: groww.SmartOrderType(q.Get("smart_order_type")),
			Status:         groww.SmartOrderStatus(q.Get("status")),
			Page:           atoiDefault(q.Get("page"), 0),
			PageSize:       atoiDefault(q.Get("page_size"), 0),
			StartDateTime:  q.Get("start_date_time"),
			EndDateTime:    q.Get("end_date_time"),
		})
		if err != nil {
			writeSDKError(w, err)
			return
		}
		writeJSON(w, 200, out)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// /v1/smart-orders/{id}
func (s *server) handleSmartOrdersItem(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/v1/smart-orders/")
	if id == "" {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		q := r.URL.Query()
		out, err := s.client.GetSmartOrder(r.Context(), groww.GetSmartOrderRequest{
			Segment:        groww.Segment(q.Get("segment")),
			SmartOrderType: groww.SmartOrderType(q.Get("smart_order_type")),
			SmartOrderId:   id,
		})
		if err != nil {
			writeSDKError(w, err)
			return
		}
		writeJSON(w, 200, out)
	case http.MethodPut:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeSDKError(w, err)
			return
		}
		out, err := dispatchSmartOrderModify(r.Context(), s.client, id, body)
		if err != nil {
			writeSDKError(w, err)
			return
		}
		writeJSON(w, 200, out)
	case http.MethodDelete:
		q := r.URL.Query()
		out, err := s.client.CancelSmartOrder(r.Context(), groww.CancelSmartOrderRequest{
			Segment:        groww.Segment(q.Get("segment")),
			SmartOrderType: groww.SmartOrderType(q.Get("smart_order_type")),
			SmartOrderId:   id,
		})
		if err != nil {
			writeSDKError(w, err)
			return
		}
		writeJSON(w, 200, out)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func dispatchSmartOrderCreate(ctx context.Context, c *groww.Client, body []byte) (groww.SmartOrder, error) {
	var probe struct {
		SmartOrderType groww.SmartOrderType `json:"smart_order_type"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return groww.SmartOrder{}, err
	}
	switch probe.SmartOrderType {
	case groww.SmartOrderTypeGtt, "":
		var req groww.CreateGttOrderRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return groww.SmartOrder{}, err
		}
		return c.CreateSmartOrder(ctx, req)
	case groww.SmartOrderTypeOco:
		var req groww.CreateOcoOrderRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return groww.SmartOrder{}, err
		}
		return c.CreateSmartOrder(ctx, req)
	}
	return groww.SmartOrder{}, fmt.Errorf("unknown smart_order_type %q", probe.SmartOrderType)
}

func dispatchSmartOrderModify(ctx context.Context, c *groww.Client, id string, body []byte) (groww.SmartOrder, error) {
	var probe struct {
		Target *groww.OcoTargetOrder `json:"target"`
		StopLoss *groww.OcoStopLossOrder `json:"stop_loss"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return groww.SmartOrder{}, err
	}
	if probe.Target != nil && probe.StopLoss != nil {
		var req groww.ModifyOcoOrderRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return groww.SmartOrder{}, err
		}
		return c.ModifySmartOrder(ctx, id, req)
	}
	var req groww.ModifyGttOrderRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return groww.SmartOrder{}, err
	}
	return c.ModifySmartOrder(ctx, id, req)
}

// ---------- math helpers ----------

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func split(s, sep string) []string {
	if s == "" {
		return nil
	}
	out := []string{}
	cur := ""
	for _, c := range s {
		if string(c) == sep {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		cur += string(c)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
