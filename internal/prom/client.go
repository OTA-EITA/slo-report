// Package prom is a minimal client for the Prometheus HTTP query API, which
// VictoriaMetrics and most hosted Prometheus services also implement.
package prom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client queries a Prometheus-compatible API.
type Client struct {
	BaseURL     string
	BearerToken string
	HTTP        *http.Client
}

// ErrNoData means the query returned no series or a NaN, e.g. no traffic.
var ErrNoData = errors.New("no data")

type apiResponse struct {
	Status    string `json:"status"`
	ErrorType string `json:"errorType"`
	Error     string `json:"error"`
	Data      struct {
		ResultType string          `json:"resultType"`
		Result     json.RawMessage `json:"result"`
	} `json:"data"`
}

// Scalar evaluates an instant query that must return a single number.
func (c Client) Scalar(ctx context.Context, query string, at time.Time) (float64, error) {
	u, err := url.Parse(strings.TrimSuffix(c.BaseURL, "/") + "/api/v1/query")
	if err != nil {
		return 0, err
	}
	q := u.Query()
	q.Set("query", query)
	q.Set("time", strconv.FormatInt(at.Unix(), 10))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, err
	}
	if c.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.BearerToken)
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return 0, err
	}
	var r apiResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return 0, fmt.Errorf("query API returned HTTP %d: %s", resp.StatusCode, truncate(string(body)))
	}
	if r.Status != "success" {
		return 0, fmt.Errorf("query failed (%s): %s", r.ErrorType, r.Error)
	}
	return parseResult(r.Data.ResultType, r.Data.Result)
}

func parseResult(resultType string, raw json.RawMessage) (float64, error) {
	var sample []any
	switch resultType {
	case "scalar":
		if err := json.Unmarshal(raw, &sample); err != nil {
			return 0, err
		}
	case "vector":
		var vec []struct {
			Value []any `json:"value"`
		}
		if err := json.Unmarshal(raw, &vec); err != nil {
			return 0, err
		}
		switch len(vec) {
		case 0:
			return 0, ErrNoData
		case 1:
			sample = vec[0].Value
		default:
			return 0, fmt.Errorf("query returned %d series; aggregate it with sum() so it returns one", len(vec))
		}
	default:
		return 0, fmt.Errorf("unsupported result type %q", resultType)
	}
	if len(sample) != 2 {
		return 0, fmt.Errorf("malformed sample %v", sample)
	}
	s, ok := sample[1].(string)
	if !ok {
		return 0, fmt.Errorf("malformed sample value %v", sample[1])
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, ErrNoData
	}
	return v, nil
}

func truncate(s string) string {
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}
