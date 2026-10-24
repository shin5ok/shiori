package iap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
)

var audiencePattern = regexp.MustCompile(`^/projects/([0-9]+)/locations/([a-z0-9-]+)/services/([a-z0-9-]+)$`)

// Grants edits the application-managed IAP binding of one Cloud Run service.
type Grants struct {
	// Client must carry cloud-platform credentials.
	Client   *http.Client
	Endpoint string
	resource string
	mu       sync.Mutex
}

// NewGrants derives the IAP resource from the Cloud Run IAP audience.
func NewGrants(client *http.Client, audience string) (*Grants, error) {
	m := audiencePattern.FindStringSubmatch(audience)
	if m == nil {
		return nil, errors.New("IAP audience must identify a Cloud Run service")
	}
	return &Grants{Client: client, Endpoint: "https://iap.googleapis.com", resource: fmt.Sprintf("projects/%s/iap_web/cloud_run-%s/services/%s", m[1], m[2], m[3])}, nil
}

func (g *Grants) Grant(ctx context.Context, email string) error  { return g.update(ctx, email, true) }
func (g *Grants) Revoke(ctx context.Context, email string) error { return g.update(ctx, email, false) }

// update is a read-modify-write guarded by the policy etag; the mutex only
// avoids needless conflicts within one instance.
func (g *Grants) update(ctx context.Context, email string, add bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	var lastErr error
	for range 4 {
		policy, err := g.call(ctx, ":getIamPolicy", []byte(`{"options":{"requestedPolicyVersion":3}}`))
		if err != nil {
			return err
		}
		updated, err := SetLinkedMember(policy, email, add)
		if err != nil {
			return err
		}
		body, _ := json.Marshal(map[string]json.RawMessage{"policy": updated})
		if _, lastErr = g.call(ctx, ":setIamPolicy", body); lastErr == nil {
			return nil
		}
		var conflict *conflictError
		if !errors.As(lastErr, &conflict) {
			return lastErr
		}
	}
	return lastErr
}

type conflictError struct{ status int }

func (e *conflictError) Error() string {
	return fmt.Sprintf("IAP policy changed concurrently (HTTP %d)", e.status)
}

func (g *Grants) call(ctx context.Context, method string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(g.Endpoint, "/")+"/v1/"+g.resource+method, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusConflict || resp.StatusCode == http.StatusPreconditionFailed || resp.StatusCode == http.StatusBadRequest && bytes.Contains(data, []byte("etag")) {
		return nil, &conflictError{resp.StatusCode}
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("IAP %s failed: HTTP %d", strings.TrimPrefix(method, ":"), resp.StatusCode)
	}
	return data, nil
}
