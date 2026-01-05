package api

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "net/url"
    "strings"
    "time"

    "github.com/redis/go-redis/v9"
)

// BronivikClient is a simple HTTP client to call bronivik_jr availability APIs.
type BronivikClient struct {
	baseURL    string
	apiKey     string
	apiExtra   string
	httpClient *http.Client

	redis    *redis.Client
	cacheTTL time.Duration
}

// AvailabilityResponse represents the response from availability API.
type AvailabilityResponse struct {
	Available bool `json:"available"`
}

// Item represents an item from the API.
type Item struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	TotalQuantity int    `json:"total_quantity"`
}

// NewBronivikClient constructs a client with baseURL, API key and extra header.
func NewBronivikClient(baseURL, apiKey, apiExtra string) *BronivikClient {
	return &BronivikClient{
		baseURL:    baseURL,
		apiKey:     apiKey,
		apiExtra:   apiExtra,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// UseRedisCache configures optional Redis caching for GET endpoints.
func (c *BronivikClient) UseRedisCache(redisClient *redis.Client, ttl time.Duration) {
    c.redis = redisClient
    c.cacheTTL = ttl
}

// GetAvailability fetches availability for item/date (YYYY-MM-DD).
func (c *BronivikClient) GetAvailability(ctx context.Context, itemName, date string) (*AvailabilityResponse, error) {
    endpoint := fmt.Sprintf("%s/api/v1/availability/%s?date=%s", c.baseURL, url.PathEscape(itemName), url.QueryEscape(date))
    cacheKey := fmt.Sprintf("availability:%s:%s", itemName, date)
    var resp AvailabilityResponse

    if c.readCache(ctx, cacheKey, &resp) {
        return &resp, nil
    }

    if err := c.doGet(ctx, endpoint, &resp); err != nil {
        return nil, err
    }
    c.writeCache(ctx, cacheKey, resp)
    return &resp, nil
}

// GetAvailabilityBulk fetches availability for multiple items/dates.
type BulkAvailabilityRequest struct {
    Items []string `json:"items"`
    Dates []string `json:"dates"`
}

type BulkAvailabilityResponse struct {
    Results []map[string]any `json:"results"`
}

func (c *BronivikClient) GetAvailabilityBulk(ctx context.Context, items, dates []string) (*BulkAvailabilityResponse, error) {
    endpoint := fmt.Sprintf("%s/api/v1/availability/bulk", c.baseURL)
    body := BulkAvailabilityRequest{Items: items, Dates: dates}
    var resp BulkAvailabilityResponse
    if err := c.doPost(ctx, endpoint, body, &resp); err != nil {
        return nil, err
    }
    return &resp, nil
}

// ListItems returns all items.
func (c *BronivikClient) ListItems(ctx context.Context) ([]Item, error) {
    endpoint := fmt.Sprintf("%s/api/v1/items", c.baseURL)
    cacheKey := "items"
    var wrap struct {
        Items []Item `json:"items"`
    }

    if c.readCache(ctx, cacheKey, &wrap) {
        return wrap.Items, nil
    }

    if err := c.doGet(ctx, endpoint, &wrap); err != nil {
        return nil, err
    }
    c.writeCache(ctx, cacheKey, wrap)
    return wrap.Items, nil
}

func (c *BronivikClient) readCache(ctx context.Context, key string, out any) bool {
    if c.redis == nil || c.cacheTTL <= 0 {
        return false
    }
    val, err := c.redis.Get(ctx, key).Result()
    if err != nil {
        return false
    }
    if err := json.Unmarshal([]byte(val), out); err != nil {
        return false
    }
    return true
}

func (c *BronivikClient) writeCache(ctx context.Context, key string, val any) {
    if c.redis == nil || c.cacheTTL <= 0 {
        return
    }
    data, err := json.Marshal(val)
    if err != nil {
        return
    }
    _ = c.redis.Set(ctx, key, data, c.cacheTTL).Err()
}

func (c *BronivikClient) doGet(ctx context.Context, endpoint string, out any) error {
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
    if err != nil {
        return err
    }
    c.addHeaders(req)
    return c.do(req, out)
}

func (c *BronivikClient) doPost(ctx context.Context, endpoint string, body any, out any) error {
    data, err := json.Marshal(body)
    if err != nil {
        return err
    }
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(data)))
    if err != nil {
        return err
    }
    req.Header.Set("Content-Type", "application/json")
    c.addHeaders(req)
    return c.do(req, out)
}

func (c *BronivikClient) do(req *http.Request, out any) error {
    resp, err := c.httpClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    if resp.StatusCode >= 300 {
        return fmt.Errorf("http %d", resp.StatusCode)
    }
    if out == nil {
        return nil
    }
    dec := json.NewDecoder(resp.Body)
    return dec.Decode(out)
}

func (c *BronivikClient) addHeaders(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("x-api-key", c.apiKey)
	}
	if c.apiExtra != "" {
		req.Header.Set("x-api-extra", c.apiExtra)
	}
}
