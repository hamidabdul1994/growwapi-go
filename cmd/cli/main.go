// Command growwapi operates every Groww Trade API as a CLI.
//
// Authentication and runtime knobs are loaded from CLI flags, environment
// variables, or a YAML config file (any of the three, with flag > env > file
// precedence). The Config struct records where every value came from.
//
// Examples:
//
//   # Place an order using a config file
//   growwapi --config ./groww.yaml orders place \
//     --trading-symbol SBIN --quantity 1 --exchange NSE --segment CASH \
//     --product CNC --order-type MARKET --transaction-type BUY --validity DAY \
//     --order-reference-id cli-001
//
//   # Toggle only via env
//   GROWW_ACCESS_TOKEN=... growwapi user profile
//
//   # Inspect where each config value came from
//   growwapi config show
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	groww "github.com/rctrj/growwapi-go"
)

func main() {
	if err := run(os.Stdout, os.Stderr, os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
			return
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(stdout, stderr io.Writer, args []string) error {
	if len(args) == 0 {
		printUsage(stdout)
		return nil
	}

	cfg := groww.NewConfig()
	fs := flag.NewFlagSet("growwapi", flag.ContinueOnError)
	fs.SetOutput(stderr)

	// Peek through flags to find the first non-flag token, which is the
	// sub-command. Anything after is its args.
	subCmd, subArgs := splitCommand(args)

	// Register CLI-owned top-level flags (only the path / config file is also
	// exposed via Config; rest comes from env / file / defaults).
	if err := cfg.Load(groww.LoadOptions{
		Args:    args,
		FlagSet: fs,
	}); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		if isFlagParseErr(err) {
			printUsage(stderr)
			return err
		}
		return err
	}

	if subCmd == "" {
		printUsage(stdout)
		return nil
	}

	client := groww.NewClientFromConfig(cfg)
	ctx := context.Background()

	switch subCmd {
	case "config":
		return runConfigShow(cfg, subArgs, stdout)
	case "user":
		return runUser(&client, subArgs, stdout)
	case "orders":
		return runOrders(ctx, &client, subArgs, stdout)
	case "portfolio":
		return runPortfolio(ctx, &client, subArgs, stdout)
	case "margin":
		return runMargin(ctx, &client, subArgs, stdout)
	case "live-data":
		return runLiveData(ctx, &client, subArgs, stdout)
	case "backtesting":
		return runBacktesting(ctx, &client, subArgs, stdout)
	case "instruments":
		return runInstruments(ctx, &client, subArgs, stdout)
	case "smart-orders":
		return runSmartOrders(ctx, &client, subArgs, stdout)
	case "-h", "--help", "help":
		printUsage(stdout)
		return nil
	default:
		return fmt.Errorf("unknown command %q", subCmd)
	}
}

func splitCommand(args []string) (cmd string, rest []string) {
	// Walk through tokens treating "key value" pairs (when value doesn't start
	// with "-") as part of flag arguments.
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			return a, args[i+1:]
		}
		// --key=value form
		if strings.Contains(a, "=") {
			continue
		}
		// Boolean flags consume no value; for other types we'd need to
		// inspect the binding, but since we don't know here we conservatively
		// assume the next token is the value if it exists and isn't a flag.
		// All CLI-owned top-level flags take values.
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			i++ // skip value
		}
	}
	return "", nil
}

func isFlagParseErr(err error) bool {
	return strings.HasPrefix(err.Error(), "parse flags:")
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `growwapi — Groww Trade API CLI / SDK tool

USAGE
  growwapi [global flags] <module> <action> [flags]

MODULES
  config          show resolved configuration and per-field source
  user profile    get user profile
  orders place|cancel|modify|status|list|details|trades
  portfolio holdings|positions|positions-by-symbol
  margin detail|orders
  live-data quote|ltp|ohlc|greeks|option-chain
  backtesting expiries|contracts|candles
  instruments     list all instruments
  smart-orders place|modify|cancel|status|list

GLOBAL FLAGS (also readable from env / config.yaml)
  --config PATH        YAML config file (env: GROWW_CONFIG)
  --access-token TOK   API access token (env: GROWW_ACCESS_TOKEN)
  --api-base-url URL   base URL (env: GROWW_API_BASE_URL, default: https://api.groww.in)
  --http-timeout N     HTTP timeout seconds (env: GROWW_HTTP_TIMEOUT, default: 30)
  --rest-addr ADDR     REST API listen addr (env: GROWW_REST_ADDR, default: :8080)

Run 'growwapi <module> --help' for module-specific flags.`)
}

// ---------------- config show ----------------

func runConfigShow(cfg *groww.Config, args []string, w io.Writer) error {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	fs.SetOutput(w)
	jsonOut := fs.Bool("json", false, "output as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *jsonOut {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"values":  cfg.Snapshot(),
			"sources": cfg.Sources(),
		})
	}

	fmt.Fprintln(w, "Resolved configuration:")
	for k, f := range cfg.Fields() {
		fmt.Fprintf(w, "  %-22s = %v   [source=%s]\n", k, f.Value, f.Source)
		if f.EnvKey != "" {
			fmt.Fprintf(w, "      env : %s\n", f.EnvKey)
		}
		if f.Flag != "" {
			fmt.Fprintf(w, "      flag: --%s\n", f.Flag)
		}
	}
	return nil
}

// ---------------- user ----------------

func runUser(c *groww.Client, args []string, w io.Writer) error {
	fs := flag.NewFlagSet("user", flag.ContinueOnError)
	fs.SetOutput(w)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 || fs.Arg(0) == "profile" {
		p, err := c.GetUserProfile(context.Background())
		if err != nil {
			return err
		}
		return writeJSON(w, p)
	}
	return fmt.Errorf("user: unknown action %q", fs.Arg(0))
}

// ---------------- orders ----------------

func runOrders(ctx context.Context, c *groww.Client, args []string, w io.Writer) error {
	if len(args) == 0 {
		return errors.New("orders: missing action")
	}
	switch args[0] {
	case "place":
		return orderPlace(ctx, c, args[1:], w)
	case "modify":
		return orderModify(ctx, c, args[1:], w)
	case "cancel":
		return orderCancel(ctx, c, args[1:], w)
	case "status":
		return orderStatus(ctx, c, args[1:], w)
	case "list":
		return orderList(ctx, c, args[1:], w)
	case "details":
		return orderDetails(ctx, c, args[1:], w)
	case "trades":
		return orderTrades(ctx, c, args[1:], w)
	default:
		return fmt.Errorf("orders: unknown action %q", args[0])
	}
}

func orderPlace(ctx context.Context, c *groww.Client, args []string, w io.Writer) error {
	fs := flag.NewFlagSet("orders place", flag.ContinueOnError)
	fs.SetOutput(w)
	req := groww.PlaceOrderRequest{}
	bindStructFlags(fs, &req)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if req.OrderReferenceId == "" {
		return errors.New("--order-reference-id is required")
	}
	out, err := c.PlaceOrder(ctx, req)
	if err != nil {
		return err
	}
	return writeJSON(w, out)
}

func orderModify(ctx context.Context, c *groww.Client, args []string, w io.Writer) error {
	fs := flag.NewFlagSet("orders modify", flag.ContinueOnError)
	fs.SetOutput(w)
	req := groww.ModifyOrderRequest{}
	bindStructFlags(fs, &req)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if req.GrowwOrderId == "" {
		return errors.New("--groww-order-id is required")
	}
	out, err := c.ModifyOrder(ctx, req)
	if err != nil {
		return err
	}
	return writeJSON(w, out)
}

func orderCancel(ctx context.Context, c *groww.Client, args []string, w io.Writer) error {
	fs := flag.NewFlagSet("orders cancel", flag.ContinueOnError)
	fs.SetOutput(w)
	req := groww.CancelOrderRequest{}
	bindStructFlags(fs, &req)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if req.GrowwOrderId == "" {
		return errors.New("--groww-order-id is required")
	}
	out, err := c.CancelOrder(ctx, req)
	if err != nil {
		return err
	}
	return writeJSON(w, out)
}

func orderStatus(ctx context.Context, c *groww.Client, args []string, w io.Writer) error {
	fs := flag.NewFlagSet("orders status", flag.ContinueOnError)
	fs.SetOutput(w)
	growwID := fs.String("groww-order-id", "", "groww order id")
	refID := fs.String("order-reference-id", "", "client order reference id")
	segment := fs.String("segment", "", "CASH|FNO")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *growwID == "" && *refID == "" {
		return errors.New("--groww-order-id or --order-reference-id is required")
	}
	var (
		out groww.OrderStatusResponse
		err error
	)
	if *growwID != "" {
		out, err = c.GetOrderStatusByGrowwOrderId(ctx, *growwID, groww.Segment(*segment))
	} else {
		out, err = c.GetOrderStatusByOrderReferenceId(ctx, *refID, groww.Segment(*segment))
	}
	if err != nil {
		return err
	}
	return writeJSON(w, out)
}

func orderList(ctx context.Context, c *groww.Client, args []string, w io.Writer) error {
	fs := flag.NewFlagSet("orders list", flag.ContinueOnError)
	fs.SetOutput(w)
	segment := fs.String("segment", "", "CASH|FNO")
	page := fs.Int("page", 0, "page number")
	pageSize := fs.Int("page-size", 0, "page size")
	if err := fs.Parse(args); err != nil {
		return err
	}
	out, err := c.ListOrders(ctx, groww.ListOrdersRequest{Segment: groww.Segment(*segment), Page: *page, PageSize: *pageSize})
	if err != nil {
		return err
	}
	return writeJSON(w, out)
}

func orderDetails(ctx context.Context, c *groww.Client, args []string, w io.Writer) error {
	fs := flag.NewFlagSet("orders details", flag.ContinueOnError)
	fs.SetOutput(w)
	id := fs.String("groww-order-id", "", "groww order id")
	segment := fs.String("segment", "", "CASH|FNO")
	if err := fs.Parse(args); err != nil {
		return err
	}
	out, err := c.GetOrderDetails(ctx, groww.GetOrderDetailsRequest{GrowwOrderId: *id, Segment: groww.Segment(*segment)})
	if err != nil {
		return err
	}
	return writeJSON(w, out)
}

func orderTrades(ctx context.Context, c *groww.Client, args []string, w io.Writer) error {
	fs := flag.NewFlagSet("orders trades", flag.ContinueOnError)
	fs.SetOutput(w)
	id := fs.String("groww-order-id", "", "groww order id")
	segment := fs.String("segment", "", "CASH|FNO")
	page := fs.Int("page", 0, "page number")
	pageSize := fs.Int("page-size", 0, "page size")
	if err := fs.Parse(args); err != nil {
		return err
	}
	out, err := c.GetTradesForOrder(ctx, groww.TradesForOrderRequest{
		GrowwOrderId: *id, Segment: groww.Segment(*segment), Page: *page, PageSize: *pageSize,
	})
	if err != nil {
		return err
	}
	return writeJSON(w, out)
}

// ---------------- portfolio ----------------

func runPortfolio(ctx context.Context, c *groww.Client, args []string, w io.Writer) error {
	if len(args) == 0 {
		return errors.New("portfolio: missing action")
	}
	fs := flag.NewFlagSet("portfolio "+args[0], flag.ContinueOnError)
	fs.SetOutput(w)
	switch args[0] {
	case "holdings":
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		out, err := c.GetHoldings(ctx)
		if err != nil {
			return err
		}
		return writeJSON(w, out)
	case "positions":
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		out, err := c.GetPositions(ctx, groww.GetPositionsRequest{})
		if err != nil {
			return err
		}
		return writeJSON(w, out)
	case "positions-by-symbol":
		tradingSymbol := fs.String("trading-symbol", "", "symbol")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		out, err := c.GetPositionBySymbol(ctx, groww.GetPositionBySymbolRequest{TradingSymbol: *tradingSymbol})
		if err != nil {
			return err
		}
		return writeJSON(w, out)
	default:
		return fmt.Errorf("portfolio: unknown action %q", args[0])
	}
}

// ---------------- margin ----------------

func runMargin(ctx context.Context, c *groww.Client, args []string, w io.Writer) error {
	if len(args) == 0 {
		return errors.New("margin: missing action")
	}
	fs := flag.NewFlagSet("margin "+args[0], flag.ContinueOnError)
	fs.SetOutput(w)
	switch args[0] {
	case "detail":
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		out, err := c.GetUserMargin(ctx)
		if err != nil {
			return err
		}
		return writeJSON(w, out)
	case "orders":
		req := groww.RequiredMarginRequest{}
		bindStructFlags(fs, &req)
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		out, err := c.GetRequiredMargin(ctx, req)
		if err != nil {
			return err
		}
		return writeJSON(w, out)
	default:
		return fmt.Errorf("margin: unknown action %q", args[0])
	}
}

// ---------------- live-data ----------------

func runLiveData(ctx context.Context, c *groww.Client, args []string, w io.Writer) error {
	if len(args) == 0 {
		return errors.New("live-data: missing action")
	}
	switch args[0] {
	case "quote":
		fs := flag.NewFlagSet("live-data quote", flag.ContinueOnError)
		fs.SetOutput(w)
		exchange := fs.String("exchange", "", "NSE|BSE")
		tradingSymbol := fs.String("trading-symbol", "", "symbol")
		segment := fs.String("segment", "", "CASH|FNO")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		out, err := c.GetQuote(ctx, groww.QuoteRequest{
			Exchange: groww.Exchange(*exchange), TradingSymbol: *tradingSymbol, Segment: groww.Segment(*segment),
		})
		if err != nil {
			return err
		}
		return writeJSON(w, out)
	case "ltp":
		fs := flag.NewFlagSet("live-data ltp", flag.ContinueOnError)
		fs.SetOutput(w)
		segment := fs.String("segment", "", "CASH|FNO")
		exchangeSymbols := fs.String("exchange-symbols", "", "comma-separated NSE_RELIANCE,BSE_SENSEX")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		syms := splitNonEmpty(*exchangeSymbols, ",")
		out, err := c.GetLtp(ctx, groww.LtpRequest{
			Segment: groww.Segment(*segment), ExchangeSymbols: syms,
		})
		if err != nil {
			return err
		}
		return writeJSON(w, out)
	case "ohlc":
		fs := flag.NewFlagSet("live-data ohlc", flag.ContinueOnError)
		fs.SetOutput(w)
		segment := fs.String("segment", "", "CASH|FNO")
		exchangeSymbols := fs.String("exchange-symbols", "", "comma-separated")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		syms := splitNonEmpty(*exchangeSymbols, ",")
		out, err := c.GetOhlc(ctx, groww.OhlcRequest{
			Segment: groww.Segment(*segment), ExchangeSymbols: syms,
		})
		if err != nil {
			return err
		}
		return writeJSON(w, out)
	case "greeks":
		fs := flag.NewFlagSet("live-data greeks", flag.ContinueOnError)
		fs.SetOutput(w)
		req := groww.GetGreeksRequest{}
		bindStructFlags(fs, &req)
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		out, err := c.GetGreeks(ctx, req)
		if err != nil {
			return err
		}
		return writeJSON(w, out)
	case "option-chain":
		fs := flag.NewFlagSet("live-data option-chain", flag.ContinueOnError)
		fs.SetOutput(w)
		exchange := fs.String("exchange", "", "exchange")
		underlying := fs.String("underlying", "", "underlying")
		expiry := fs.String("expiry", "", "expiry YYYY-MM-DD")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		exp, err := time.Parse("2006-01-02", *expiry)
		if err != nil {
			return fmt.Errorf("--expiry must be YYYY-MM-DD: %w", err)
		}
		out, err := c.GetOptionChain(ctx, groww.GetOptionChainRequest{
			Exchange: groww.Exchange(*exchange), Underlying: *underlying, ExpiryDate: exp,
		})
		if err != nil {
			return err
		}
		return writeJSON(w, out)
	default:
		return fmt.Errorf("live-data: unknown action %q", args[0])
	}
}

// ---------------- backtesting ----------------

func runBacktesting(ctx context.Context, c *groww.Client, args []string, w io.Writer) error {
	if len(args) == 0 {
		return errors.New("backtesting: missing action")
	}
	switch args[0] {
	case "expiries":
		fs := flag.NewFlagSet("backtesting expiries", flag.ContinueOnError)
		fs.SetOutput(w)
		exchange := fs.String("exchange", "", "exchange")
		underlying := fs.String("underlying", "", "underlying")
		year := fs.Int("year", 0, "year")
		month := fs.Int("month", 0, "month 1-12")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		out, err := c.GetExpiries(ctx, groww.GetExpiriesRequest{
			Exchange: groww.Exchange(*exchange), UnderlyingSymbol: *underlying,
			Year: *year, Month: *month,
		})
		if err != nil {
			return err
		}
		return writeJSON(w, out)
	case "contracts":
		fs := flag.NewFlagSet("backtesting contracts", flag.ContinueOnError)
		fs.SetOutput(w)
		req := groww.GetContractsRequest{}
		bindStructFlags(fs, &req)
		expiry := fs.String("expiry-date", "", "expiry YYYY-MM-DD")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *expiry != "" {
			t, err := time.Parse("2006-01-02", *expiry)
			if err != nil {
				return fmt.Errorf("--expiry-date must be YYYY-MM-DD: %w", err)
			}
			req.ExpiryDate = t
		}
		out, err := c.GetContracts(ctx, req)
		if err != nil {
			return err
		}
		return writeJSON(w, out)
	case "candles":
		fs := flag.NewFlagSet("backtesting candles", flag.ContinueOnError)
		fs.SetOutput(w)
		req := groww.GetHistoricalCandlesRequest{}
		bindStructFlags(fs, &req)
		start := fs.String("start-time", "", "start YYYY-MM-DD HH:MM:SS")
		end := fs.String("end-time", "", "end YYYY-MM-DD HH:MM:SS")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *start != "" {
			t, err := time.Parse("2006-01-02 15:04:05", *start)
			if err != nil {
				return fmt.Errorf("--start-time must be YYYY-MM-DD HH:MM:SS: %w", err)
			}
			req.StartTime = t
		}
		if *end != "" {
			t, err := time.Parse("2006-01-02 15:04:05", *end)
			if err != nil {
				return fmt.Errorf("--end-time must be YYYY-MM-DD HH:MM:SS: %w", err)
			}
			req.EndTime = t
		}
		out, err := c.GetHistoricalCandles(ctx, req)
		if err != nil {
			return err
		}
		return writeJSON(w, out)
	default:
		return fmt.Errorf("backtesting: unknown action %q", args[0])
	}
}

// ---------------- instruments ----------------

func runInstruments(ctx context.Context, c *groww.Client, args []string, w io.Writer) error {
	out, err := c.Instruments(ctx)
	if err != nil {
		return err
	}
	return writeJSON(w, out)
}

// ---------------- smart-orders ----------------

func runSmartOrders(ctx context.Context, c *groww.Client, args []string, w io.Writer) error {
	if len(args) == 0 {
		return errors.New("smart-orders: missing action")
	}
	switch args[0] {
	case "place":
		fs := flag.NewFlagSet("smart-orders place", flag.ContinueOnError)
		fs.SetOutput(w)
		req := groww.CreateGttOrderRequest{}
		bindStructFlags(fs, &req)
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		out, err := c.CreateSmartOrder(ctx, req)
		if err != nil {
			return err
		}
		return writeJSON(w, out)
	case "modify":
		fs := flag.NewFlagSet("smart-orders modify", flag.ContinueOnError)
		fs.SetOutput(w)
		smartOrderID := fs.String("smart-order-id", "", "smart order id")
		req := groww.ModifyGttOrderRequest{}
		bindStructFlags(fs, &req)
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		out, err := c.ModifySmartOrder(ctx, *smartOrderID, req)
		if err != nil {
			return err
		}
		return writeJSON(w, out)
	case "cancel":
		fs := flag.NewFlagSet("smart-orders cancel", flag.ContinueOnError)
		fs.SetOutput(w)
		req := groww.CancelSmartOrderRequest{}
		bindStructFlags(fs, &req)
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		out, err := c.CancelSmartOrder(ctx, req)
		if err != nil {
			return err
		}
		return writeJSON(w, out)
	case "status":
		fs := flag.NewFlagSet("smart-orders status", flag.ContinueOnError)
		fs.SetOutput(w)
		req := groww.GetSmartOrderRequest{}
		bindStructFlags(fs, &req)
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		out, err := c.GetSmartOrder(ctx, req)
		if err != nil {
			return err
		}
		return writeJSON(w, out)
	case "list":
		fs := flag.NewFlagSet("smart-orders list", flag.ContinueOnError)
		fs.SetOutput(w)
		req := groww.ListSmartOrdersRequest{}
		bindStructFlags(fs, &req)
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		out, err := c.ListSmartOrders(ctx, req)
		if err != nil {
			return err
		}
		return writeJSON(w, out)
	default:
		return fmt.Errorf("smart-orders: unknown action %q", args[0])
	}
}

// ---------------- helpers ----------------

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// bindStructFlags iterates each public field of the struct and registers a
// --kebab-case flag that parses from string. Lets us reuse Groww request
// structs as CLI flags with minimal boilerplate.
func bindStructFlags(fs *flag.FlagSet, ptr any) {
	v := reflect.ValueOf(ptr).Elem()
	if v.Kind() != reflect.Struct {
		return
	}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		ft := t.Field(i)
		if ft.PkgPath != "" {
			continue
		}
		name := toFlag(ft.Name)
		var def string
		if tag := ft.Tag.Get("default"); tag != "" {
			def = tag
		}
		rv := v.Field(i)
		switch rv.Kind() {
		case reflect.String:
			if rv.Type().Name() != "" {
				// typed string (e.g. Segment, Exchange) -> register as string anyway
			}
			val := rv.String()
			fs.StringVar(toStringPtr(rv), name, val, "")
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			fs.Int64Var(toInt64Ptr(rv), name, rv.Int(), "")
		case reflect.Float32, reflect.Float64:
			fs.Float64Var(toFloat64Ptr(rv), name, rv.Float(), "")
		case reflect.Bool:
			fs.BoolVar(toBoolPtr(rv), name, rv.Bool(), "")
		}
		_ = def
	}
}

func toFlag(name string) string {
	var b strings.Builder
	for i, r := range name {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('-')
		}
		if r >= 'A' && r <= 'Z' {
			b.WriteRune(r + 32)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func toStringPtr(v reflect.Value) *string {
	if v.CanAddr() {
		return v.Addr().Interface().(*string)
	}
	s := v.String()
	return &s
}

func toInt64Ptr(v reflect.Value) *int64 {
	if v.CanAddr() {
		return v.Addr().Interface().(*int64)
	}
	n := v.Int()
	return &n
}

func toFloat64Ptr(v reflect.Value) *float64 {
	if v.CanAddr() {
		return v.Addr().Interface().(*float64)
	}
	f := v.Float()
	return &f
}

func toBoolPtr(v reflect.Value) *bool {
	if v.CanAddr() {
		return v.Addr().Interface().(*bool)
	}
	b := v.Bool()
	return &b
}

// _ = strconv.Itoa unused; kept to silence linter complaints if needed.
var _ = strconv.Itoa

func splitNonEmpty(s, sep string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, sep)
	out := parts[:0]
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
