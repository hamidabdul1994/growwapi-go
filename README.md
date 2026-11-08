### Growwapi-go

Unofficial Go SDK + CLI + REST server for [Groww APIs](https://groww.in/trade-api/docs/curl).
The documentation is directly picked from the API docs.

## Features

- Strongly typed Go structs for every Groww Trade API
- Use it as a **library SDK**, a **CLI**, or a **REST API** — same code paths
- Runtime configuration driven by **flags**, **environment variables**, or
  a **config.yaml** file (any combination, with flag > env > file > default
  precedence)
- Every tracked config field records **where its value came from**, so you
  can introspect via `growwapi config show` or `GET /configz`

This started as a personal requirement, and modules are added when needed.

| Module                                        | Status |
|-----------------------------------------------|--------|
| Instruments                                   | ✅      |
| Orders                                        | ✅      |
| Smart Orders                                  | ✅      |
| Portfolio                                     | ✅      |
| Margin                                        | ✅      |
| Live Data                                     | ✅      |
| Historical Data (Deprecated, use Backtesting) | ❌      |
| Backtesting                                   | ✅      |
| User                                          | ✅      |
| Annexures                                     | ✅      |

## Installation (as SDK)
```
go get github.com/rctrj/growwapi-go
```

```go
client := groww.NewClient("ACCESS_TOKEN", nil)
profile, err := client.GetUserProfile(ctx)
```

## Configuration

Every public surface (SDK, CLI, REST) starts from a single `growwapi.Config`
struct. Each tracked field knows three names and a default:

| Field           | Env var              | Flag              | YAML key        | Default                 |
|-----------------|----------------------|-------------------|-----------------|-------------------------|
| AccessToken     | GROWW_ACCESS_TOKEN   | --access-token    | access_token    | (required)              |
| APIBaseURL      | GROWW_API_BASE_URL   | --api-base-url    | api_base_url    | https://api.groww.in    |
| HTTPTimeoutSeconds | GROWW_HTTP_TIMEOUT | --http-timeout    | http_timeout    | 30                      |
| RESTListenAddr  | GROWW_REST_ADDR      | --rest-addr       | rest_addr       | :8080                   |

### Resolution order (highest wins)
1. CLI flag
2. Environment variable
3. YAML config file (path comes from `--config` or `GROWW_CONFIG`)
4. Built-in default

### Inspecting where values came from
```bash
$ growwapi --access-token MYFLAG config show
Resolved configuration:
  access_token = MYFLAG        [source=flag]
      env : GROWW_ACCESS_TOKEN
      flag: --access-token
  api_base_url = https://api.groww.in [source=default]
      ...
```

In your Go code, `cfg.SourceOf("access_token")` returns `ConfigSourceFlag`,
`ConfigSourceEnv`, `ConfigSourceFile`, or `ConfigSourceDefault`.

## CLI

```
go build -o growwapi ./cmd/cli
GROWW_ACCESS_TOKEN=… ./growwapi user profile
./growwapi --config groww.yaml orders list --segment CASH
./growwapi config show --json     # JSON dump of values + sources
```

Run `growwapi` to see all modules/actions, and `growwapi <module>` to see
module-specific flags.

## REST API

```
go build -o growwapi-rest ./cmd/restapi
GROWW_ACCESS_TOKEN=… ./growwapi-rest            # listens on :8080
```

Routes mirror the SDK API surface:

```
GET    /healthz
GET    /configz
GET    /v1/user/profile
POST   /v1/orders                     (JSON: PlaceOrderRequest)
PUT    /v1/orders/:growwOrderId       (JSON: ModifyOrderRequest)
DELETE /v1/orders/:growwOrderId?segment=...
GET    /v1/orders/:growwOrderId?segment=...
GET    /v1/orders/:growwOrderId/trades
POST   /v1/orders/status              (body picks groww vs reference id)
GET    /v1/portfolio/holdings
GET    /v1/portfolio/positions?segment=...
GET    /v1/portfolio/positions/:symbol
GET    /v1/margin/detail
POST   /v1/margin/orders?segment=...  (JSON: [OrderMarginRequest])
GET    /v1/livedata/quote?exchange=&segment=&trading_symbol=
GET    /v1/livedata/ltp?segment=&exchange_symbols=NSE_RELIANCE,...
GET    /v1/livedata/ohlc?segment=&exchange_symbols=...
GET    /v1/livedata/greeks?exchange=&underlying=&trading_symbol=&expiry=YYYY-MM-DD
GET    /v1/livedata/option-chain?exchange=&underlying=&expiry_date=YYYY-MM-DD
GET    /v1/backtesting/expiries?exchange=&underlying_symbol=
GET    /v1/backtesting/contracts?exchange=&underlying_symbol=&expiry_date=YYYY-MM-DD
GET    /v1/backtesting/candles?exchange=&segment=&groww_symbol=&start_time=&end_time=&candle_interval=
GET    /v1/instruments
POST   /v1/smart-orders               (JSON: CreateGttOrderRequest | CreateOcoOrderRequest)
PUT    /v1/smart-orders/:id           (JSON: ModifyGttOrderRequest | ModifyOcoOrderRequest)
DELETE /v1/smart-orders/:id?segment=&smart_order_type=
GET    /v1/smart-orders/:id?segment=&smart_order_type=
GET    /v1/smart-orders
```

### `config.yaml` example
```yaml
# Values defined here are the LOWEST precedence; env > file, flag > env.
access_token: "your-token"
api_base_url: "https://api.groww.in"
http_timeout: 25
rest_addr: ":9090"
```

## Future Features
- Rate Limiting
- Retries
