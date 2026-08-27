package jumpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	baseURL     *url.URL
	accessToken string
	orgID       string
	httpClient  *http.Client
	Timezone    string
}

func NewClient(site, accessToken, organization string, httpClient *http.Client) (*Client, error) {
	parsed, err := url.Parse(site)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("invalid JumpServer URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return nil, fmt.Errorf("JumpServer URL must contain only scheme and host")
	}
	parsed.Path = "/"
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{baseURL: parsed, accessToken: accessToken, orgID: organization, httpClient: httpClient, Timezone: localTimezone()}, nil
}

func (c *Client) ListOrganizations(ctx context.Context) ([]Organization, error) {
	var permissions struct {
		PAM       []Organization `json:"pam_orgs"`
		Audit     []Organization `json:"audit_orgs"`
		Console   []Organization `json:"console_orgs"`
		Workbench []Organization `json:"workbench_orgs"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/users/profile/permissions/", nil, http.StatusOK, &permissions); err != nil {
		return nil, err
	}
	byID := make(map[string]Organization)
	for _, group := range [][]Organization{permissions.PAM, permissions.Audit, permissions.Console, permissions.Workbench} {
		for _, organization := range group {
			if organization.ID != "" {
				byID[organization.ID] = organization
			}
		}
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make([]Organization, 0, len(ids))
	for _, id := range ids {
		result = append(result, byID[id])
	}
	return result, nil
}

func (c *Client) ListAssets(ctx context.Context, query AssetQuery) (AssetPage, error) {
	values := url.Values{}
	setIfNotEmpty(values, "type", query.Type)
	setIfNotEmpty(values, "category", query.Category)
	setIfNotEmpty(values, "search", query.Search)
	setIfNotEmpty(values, "order", query.Order)
	if query.Offset > 0 {
		values.Set("offset", strconv.Itoa(query.Offset))
	}
	if query.Limit > 0 {
		values.Set("limit", strconv.Itoa(query.Limit))
	}
	if c.orgID != "" {
		values.Set("oid", c.orgID)
	}
	requestPath := "/api/v1/perms/users/self/assets/"
	if encoded := values.Encode(); encoded != "" {
		requestPath += "?" + encoded
	}
	var page AssetPage
	if err := c.doJSON(ctx, http.MethodGet, requestPath, nil, http.StatusOK, &page); err != nil {
		return AssetPage{}, err
	}
	return page, nil
}

func (c *Client) GetAsset(ctx context.Context, id string) (AssetDetail, error) {
	if strings.TrimSpace(id) == "" {
		return AssetDetail{}, fmt.Errorf("asset ID is required")
	}
	var asset AssetDetail
	requestPath := "/api/v1/perms/users/self/assets/" + url.PathEscape(id) + "/"
	if err := c.doJSON(ctx, http.MethodGet, requestPath, nil, http.StatusOK, &asset); err != nil {
		return AssetDetail{}, err
	}
	return asset, nil
}

func (c *Client) CreateConnectionToken(ctx context.Context, request ConnectionRequest) (string, error) {
	if request.Asset == "" {
		return "", fmt.Errorf("asset ID is required")
	}
	request.Protocol = "ssh"
	request.ConnectMethod = "ssh_client"
	request.ConnectOptions = ConnectionOptions{TokenReusable: false, DisableAutoHash: false}
	var response json.RawMessage
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/authentication/connection-token/", request, http.StatusCreated, &response); err != nil {
		return "", err
	}
	var object struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(response, &object); err != nil || object.ID == "" {
		var id string
		if stringErr := json.Unmarshal(response, &id); stringErr != nil || id == "" {
			return "", fmt.Errorf("connection-token response is incomplete")
		}
		return id, nil
	}
	return object.ID, nil
}

func (c *Client) GetClientConnection(ctx context.Context, connectionTokenID string) (ClientConnection, error) {
	if connectionTokenID == "" {
		return ClientConnection{}, fmt.Errorf("connection token ID is required")
	}
	var response struct {
		URL string `json:"url"`
	}
	requestPath := "/api/v1/authentication/connection-token/" + url.PathEscape(connectionTokenID) + "/client-url/"
	if err := c.doJSON(ctx, http.MethodGet, requestPath, nil, http.StatusOK, &response); err != nil {
		return ClientConnection{}, err
	}
	return DecodeClientURL(response.URL)
}

func (c *Client) doJSON(ctx context.Context, method, requestPath string, body any, expectedStatus int, target any) error {
	endpoint, err := c.baseURL.Parse(requestPath)
	if err != nil {
		return fmt.Errorf("build JumpServer API URL: %w", err)
	}
	var encoded io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode JumpServer API request: %w", err)
		}
		defer clear(data)
		encoded = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), encoded)
	if err != nil {
		return fmt.Errorf("create JumpServer API request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.accessToken)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Referer", c.baseURL.String())
	request.Header.Set("X-TZ", c.Timezone)
	if c.orgID != "" {
		request.Header.Set("X-JMS-ORG", c.orgID)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("call JumpServer API: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != expectedStatus {
		return fmt.Errorf("JumpServer API %s %s returned HTTP %d", method, endpoint.Path, response.StatusCode)
	}
	if target == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(target); err != nil {
		return fmt.Errorf("decode JumpServer API response: %w", err)
	}
	return nil
}

func setIfNotEmpty(values url.Values, key, value string) {
	if value != "" {
		values.Set(key, value)
	}
}

func localTimezone() string {
	_, seconds := time.Now().Zone()
	sign := "+"
	if seconds < 0 {
		sign = "-"
		seconds = -seconds
	}
	return fmt.Sprintf("%s%02d:%02d", sign, seconds/3600, seconds%3600/60)
}
