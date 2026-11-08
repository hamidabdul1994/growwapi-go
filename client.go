package growwapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultAPIBaseURL = "https://api.groww.in"

// Client to access groww apis
type Client struct {
	accessToken string
	baseURL     string
	httpClient  *http.Client
}

// NewClient creates a new Client. Pass httpClient=nil to use http.DefaultClient.
func NewClient(accessToken string, httpClient *http.Client) Client {
	return NewClientWithBaseURL(accessToken, defaultAPIBaseURL, httpClient)
}

// NewClientWithBaseURL is like NewClient but allows overriding the API base URL
// (helpful for tests and for pointing at alternative deployments).
func NewClientWithBaseURL(accessToken, baseURL string, httpClient *http.Client) Client {
	if baseURL == "" {
		baseURL = defaultAPIBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return Client{accessToken: accessToken, baseURL: baseURL, httpClient: httpClient}
}

// NewClientFromConfig builds a Client from a loaded Config.
func NewClientFromConfig(c *Config) Client {
	return NewClientWithBaseURL(c.AccessToken, c.APIBaseURL, &http.Client{Timeout: time.Duration(c.HTTPTimeoutSeconds) * time.Second})
}

// ErrorCode are codes returned by GROWW APIs
type ErrorCode string

const (
	ErrorCodeGA000 ErrorCode = "GA000"
	ErrorCodeGA001 ErrorCode = "GA001"
	ErrorCodeGA003 ErrorCode = "GA003"
	ErrorCodeGA004 ErrorCode = "GA004"
	ErrorCodeGA005 ErrorCode = "GA005"
	ErrorCodeGA006 ErrorCode = "GA006"
	ErrorCodeGA007 ErrorCode = "GA007"
)

var errorCodeMessages = map[ErrorCode]string{
	ErrorCodeGA000: "Internal error occurred",
	ErrorCodeGA001: "Bad request",
	ErrorCodeGA003: "Unable to serve request currently",
	ErrorCodeGA004: "Requested entity does not exist",
	ErrorCodeGA005: "User not authorised to perform this operation",
	ErrorCodeGA006: "Requested entity does not exist",
	ErrorCodeGA007: "Duplicate order reference id",
}

// Message returns the message related to ErrorCode.
// If you want the actual message returned by the API, use Error.Message
func (e ErrorCode) Message() string {
	if got, ok := errorCodeMessages[e]; ok {
		return got
	}

	return "Unknown Error"
}

type errorResponse struct {
	Status string `json:"status"`
	Error  Error  `json:"error"`
}

// Error represents the error data returned from Groww APIs
type Error struct {
	Code     ErrorCode `json:"code"`
	Message  string    `json:"message"`
	Metadata any       `json:"metadata"`
}

type apiResponse[T any] struct {
	Status  string `json:"status"`
	Payload T      `json:"payload"`
}

func (e Error) Error() string {
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (c *Client) headers() http.Header {
	headers := make(http.Header)

	headers.Add("Accept", "application/json")
	headers.Add("Content-Type", "application/json")
	headers.Add("X-API-VERSION", "1.0")
	headers.Add("Authorization", fmt.Sprintf("Bearer %s", c.accessToken))

	return headers
}

// Ohlc contains open, high, low and close prices for an instrument in an interval
type Ohlc struct {
	// Opening price
	Open float32 `json:"open"`
	// Highest price
	High float32 `json:"high"`
	// Lowest price
	Low float32 `json:"low"`
	// Closing price
	Close float32 `json:"close"`
}

// Ohlcv contains Ohlc for an instrument along with volume
type Ohlcv struct {
	Ohlc
	Volume float32 `json:"volume"`
}

type ohlcString struct {
	Ohlc
}

func (o *ohlcString) UnmarshalJSON(bytes []byte) error {
	var ohlc Ohlc
	if err := json.Unmarshal(bytes, &ohlc); err != nil {
		return err
	}

	o.Ohlc = ohlc
	return nil
}

type asQueryParam interface {
	queryParams() url.Values
}

// resolveURL returns raw unchanged if it already contains "://"
// (absolute); otherwise it is resolved against the client's baseURL.
func (c *Client) resolveURL(raw string) string {
	if raw == "" {
		return raw
	}
	if strings.Contains(raw, "://") {
		return raw
	}
	return strings.TrimRight(c.baseURL, "/") + "/" + strings.TrimLeft(raw, "/")
}

func doGetRequest[T any](ctx context.Context, c *Client, url string, queries asQueryParam) (out T, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.resolveURL(url), nil)
	if err != nil {
		return out, fmt.Errorf("http.NewRequest: %w", err)
	}

	if queries != nil {
		params := queries.queryParams()
		req.URL.RawQuery = params.Encode()
	}

	return doRequest[T](ctx, c, req)
}

func doPostRequest[T any](ctx context.Context, c *Client, url string, body any) (out T, err error) {
	msg, err := json.Marshal(body)
	if err != nil {
		return out, fmt.Errorf("json.Marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.resolveURL(url), bytes.NewBuffer(msg))
	if err != nil {
		return out, fmt.Errorf("http.NewRequest: %w", err)
	}

	return doRequest[T](ctx, c, req)
}

func doPutRequest[T any](ctx context.Context, c *Client, url string, body any) (out T, err error) {
	msg, err := json.Marshal(body)
	if err != nil {
		return out, fmt.Errorf("json.Marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.resolveURL(url), bytes.NewBuffer(msg))
	if err != nil {
		return out, fmt.Errorf("http.NewRequest: %w", err)
	}

	return doRequest[T](ctx, c, req)
}

func doRequest[T any](ctx context.Context, c *Client, req *http.Request) (out T, err error) {
	req.Header = c.headers()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return out, fmt.Errorf("c.httpClient.Do: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusAccepted:
		var r apiResponse[T]
		if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
			return out, fmt.Errorf("json.NewDecoder(success_response): %w", err)
		}
		return r.Payload, nil

	default:
		var e errorResponse
		if err := json.NewDecoder(resp.Body).Decode(&e); err != nil {
			return out, fmt.Errorf("json.NewDecoder(success_response): %w", err)
		}
		return out, e.Error
	}
}
