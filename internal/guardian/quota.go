package guardian

import (
	"context"
	"errors"
	"net/http"
)

// Quota is an account's VPN quota as reported by Guardian's X-Quota-* headers.
type Quota struct {
	Limit     *int64
	Remaining *int64
	Reset     *int64
}

// Quota fetches quota headers without downloading the proxy pass body. Guardian
// supports HEAD on the pass endpoint; older deployments fall back to FetchPass.
func (c *Client) Quota(ctx context.Context, accessToken string) (*Quota, error) {
	header, _, err := c.do(ctx, http.MethodHead, "/api/v1/fpn/token", accessToken)
	if err == nil {
		quota := quotaFromHeader(header)
		if quota != nil {
			return quota, nil
		}
		return c.quotaFromPass(ctx, accessToken)
	}
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		switch httpErr.Status {
		case http.StatusNotFound, http.StatusMethodNotAllowed:
			return c.quotaFromPass(ctx, accessToken)
		case http.StatusTooManyRequests:
			zero := int64(0)
			return &Quota{Remaining: &zero}, nil
		}
	}
	return nil, err
}

func (c *Client) quotaFromPass(ctx context.Context, accessToken string) (*Quota, error) {
	pass, err := c.FetchPass(ctx, accessToken)
	if err != nil {
		var httpErr *HTTPError
		if errors.As(err, &httpErr) && httpErr.Status == http.StatusTooManyRequests {
			zero := int64(0)
			return &Quota{Remaining: &zero}, nil
		}
		return nil, err
	}
	return &Quota{Limit: pass.QuotaLimit, Remaining: pass.QuotaRemaining, Reset: pass.QuotaReset}, nil
}

func quotaFromHeader(header http.Header) *Quota {
	limit := headerInt(header, "X-Quota-Limit")
	remaining := headerInt(header, "X-Quota-Remaining")
	reset := headerInt(header, "X-Quota-Reset")
	if limit == nil && remaining == nil && reset == nil {
		return nil
	}
	return &Quota{Limit: limit, Remaining: remaining, Reset: reset}
}
