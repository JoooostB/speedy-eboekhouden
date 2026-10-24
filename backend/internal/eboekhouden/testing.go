package eboekhouden

// NewClientForTesting returns a client that sends its API calls to baseURL
// (an httptest.Server) instead of e-boekhouden.nl, so tests in other packages
// can stand in a fake e-boekhouden. It is for tests only; production code has
// no reason to call it and the base URL cannot be changed in any other way.
func NewClientForTesting(baseURL, token string) (*Client, error) {
	c, err := newClient(token)
	if err != nil {
		return nil, err
	}
	c.setBaseURL(baseURL)
	return c, nil
}
