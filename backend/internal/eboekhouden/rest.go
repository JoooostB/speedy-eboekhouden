package eboekhouden

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const restBaseURL = "https://api.e-boekhouden.nl"

var restHTTPClient = &http.Client{Timeout: 30 * time.Second}

// RestClient provides access to the official e-boekhouden REST API.
type RestClient struct {
	accessToken string
	source      string

	sessionToken   string
	sessionExpires time.Time
	mu             sync.Mutex
}

// NewRestClient creates a new REST API client.
func NewRestClient(accessToken string) *RestClient {
	return &RestClient{
		accessToken: accessToken,
		source:      "Speedy",
	}
}

func (c *RestClient) ensureSession() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sessionToken != "" && time.Now().Before(c.sessionExpires.Add(-60*time.Second)) {
		return c.sessionToken, nil
	}

	payload, _ := json.Marshal(map[string]string{
		"accessToken": c.accessToken,
		"source":      c.source,
	})

	req, err := http.NewRequest("POST", restBaseURL+"/v1/session", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("creating REST session request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := restHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("REST session request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.ReadAll(io.LimitReader(resp.Body, 1024)) // drain but don't use in error
		return "", fmt.Errorf("sessie met e-boekhouden.nl REST API mislukt (status %d)", resp.StatusCode)
	}

	var session struct {
		Token     string `json:"token"`
		ExpiresIn int    `json:"expiresIn"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return "", fmt.Errorf("decoding REST session: %w", err)
	}

	c.sessionToken = session.Token
	c.sessionExpires = time.Now().Add(time.Duration(session.ExpiresIn) * time.Second)
	return c.sessionToken, nil
}

func (c *RestClient) request(method, path string, body interface{}) (json.RawMessage, error) {
	token, err := c.ensureSession()
	if err != nil {
		return nil, err
	}

	var bodyReader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, restBaseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("creating REST request: %w", err)
	}
	req.Header.Set("Authorization", token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := restHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("REST request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10 MB max
	if err != nil {
		return nil, fmt.Errorf("reading REST response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("e-boekhouden.nl REST API fout (status %d)", resp.StatusCode)
	}

	if resp.StatusCode == 204 || len(respBody) == 0 {
		return json.RawMessage(`{}`), nil
	}

	return json.RawMessage(respBody), nil
}

// GetInvoices returns invoices.
func (c *RestClient) GetInvoices(limit, offset int) (json.RawMessage, error) {
	path := fmt.Sprintf("/v1/invoice?limit=%d&offset=%d", limit, offset)
	return c.request("GET", path, nil)
}

// GetInvoice returns a single invoice by ID.
func (c *RestClient) GetInvoice(id int) (json.RawMessage, error) {
	return c.request("GET", fmt.Sprintf("/v1/invoice/%d", id), nil)
}

// CreateInvoice creates a new sales invoice.
func (c *RestClient) CreateInvoice(payload json.RawMessage) (json.RawMessage, error) {
	return c.request("POST", "/v1/invoice", payload)
}

// GetCostCenters returns cost centers.
func (c *RestClient) GetCostCenters() (json.RawMessage, error) {
	return c.request("GET", "/v1/costcenter", nil)
}

// CreateCostCenter creates a new cost center.
func (c *RestClient) CreateCostCenter(payload json.RawMessage) (json.RawMessage, error) {
	return c.request("POST", "/v1/costcenter", payload)
}

// GetEmailTemplates returns email templates.
func (c *RestClient) GetEmailTemplates() (json.RawMessage, error) {
	return c.request("GET", "/v1/emailtemplate", nil)
}

// GetAdministrations returns linked administrations.
func (c *RestClient) GetAdministrations() (json.RawMessage, error) {
	return c.request("GET", "/v1/administration/linked", nil)
}
