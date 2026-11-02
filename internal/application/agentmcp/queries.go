package agentmcp

import (
	"context"
	"crypto/rand"
	"errors"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/cmstar/jumpaccess/internal/application/auth"
	"github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/jumpserver"
)

type ConfigReader interface{ Load() (config.Config, error) }
type AuthReader interface {
	Status(string) (auth.Status, error)
}
type ResourceReader interface {
	ListOrganizations(context.Context, string) ([]jumpserver.Organization, error)
	ListAssets(context.Context, string, string, string, int, int) (jumpserver.AssetPage, error)
	GetAssetByID(context.Context, string, string, string) (jumpserver.AssetDetail, error)
}

type Queries struct {
	instance  string
	cursorKey []byte
	config    ConfigReader
	auth      AuthReader
	resources ResourceReader
}

func NewQueries(instance string, config ConfigReader, auth AuthReader, resources ResourceReader) *Queries {
	return &Queries{instance: instance, cursorKey: []byte(rand.Text()), config: config, auth: auth, resources: resources}
}

type QueryError struct {
	Code      string
	Message   string
	Retryable bool
}

func (e *QueryError) Error() string { return e.Message }
func invalid(message string) error  { return &QueryError{Code: "INVALID_ARGUMENT", Message: message} }
func queryError(err error) error {
	var existing *QueryError
	if errors.As(err, &existing) {
		return existing
	}
	if errors.Is(err, auth.ErrLoginRequired) {
		return &QueryError{Code: "AUTH_REQUIRED", Message: "请先通过 JumpAccess 完成该 Profile 的登录"}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &QueryError{Code: "QUERY_INTERRUPTED", Message: "资源查询超时或已取消", Retryable: true}
	}
	return &QueryError{Code: "RESOURCE_UNAVAILABLE", Message: "无法查询资源，请检查登录状态、网络和 JumpServer 权限", Retryable: true}
}

type PageInput struct {
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}
type ProfilePageInput struct {
	PageInput
	Profile string `json:"profile"`
}
type AssetsInput struct {
	PageInput
	Profile      string `json:"profile"`
	Organization string `json:"organization"`
	Search       string `json:"search,omitempty"`
}
type AssetInput struct {
	Profile      string `json:"profile"`
	Organization string `json:"organization"`
	AssetID      string `json:"asset_id"`
}
type AccountsInput struct {
	PageInput
	Profile      string `json:"profile"`
	Organization string `json:"organization"`
	AssetID      string `json:"asset_id"`
}
type AliasesInput struct {
	PageInput
	Profile      string `json:"profile"`
	Organization string `json:"organization,omitempty"`
}

type Page[T any] struct {
	Profile      string `json:"profile,omitempty"`
	Organization string `json:"organization,omitempty"`
	Items        []T    `json:"items"`
	Total        int    `json:"total"`
	NextCursor   string `json:"next_cursor,omitempty"`
}
type ProfileView struct {
	Name             string `json:"name"`
	Site             string `json:"site"`
	Organization     string `json:"organization"`
	Current          bool   `json:"current"`
	LoggedIn         bool   `json:"logged_in"`
	Expired          bool   `json:"expired"`
	RefreshAvailable bool   `json:"refresh_available"`
}
type AssetView struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Address  string `json:"address"`
	Type     string `json:"type"`
	Category string `json:"category"`
}
type AccountView struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Username string   `json:"username"`
	Actions  []string `json:"actions"`
}
type ProtocolView struct {
	Name string `json:"name"`
	Port int    `json:"port"`
}
type AssetResult struct {
	Profile      string         `json:"profile"`
	Organization string         `json:"organization"`
	Asset        AssetView      `json:"asset"`
	Accounts     []AccountView  `json:"accounts"`
	Protocols    []ProtocolView `json:"protocols"`
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func (q *Queries) profile(name string) (config.Profile, error) {
	if strings.TrimSpace(name) == "" {
		return config.Profile{}, invalid("必须明确指定 Profile")
	}
	cfg, err := q.config.Load()
	if err != nil {
		return config.Profile{}, queryError(err)
	}
	profile, exists := cfg.Profiles[name]
	if !exists {
		return config.Profile{}, &QueryError{Code: "PROFILE_NOT_FOUND", Message: "指定的 Profile 不存在"}
	}
	return profile, nil
}

func (q *Queries) target(profile, organization string) error {
	if _, err := q.profile(profile); err != nil {
		return err
	}
	if !identifier.MatchString(organization) {
		return invalid("必须明确指定有效的 Organization ID")
	}
	return nil
}

func (q *Queries) ListProfiles(ctx context.Context, input PageInput) (Page[ProfileView], error) {
	offset, limit, key, err := q.page(input, "profiles")
	if err != nil {
		return Page[ProfileView]{}, err
	}
	cfg, err := q.config.Load()
	if err != nil {
		return Page[ProfileView]{}, queryError(err)
	}
	names := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	items := make([]ProfileView, 0)
	for _, name := range window(names, offset, limit) {
		if err := ctx.Err(); err != nil {
			return Page[ProfileView]{}, queryError(err)
		}
		status, err := q.auth.Status(name)
		if err != nil {
			return Page[ProfileView]{}, queryError(err)
		}
		profile := cfg.Profiles[name]
		parsed, err := url.Parse(profile.URL)
		if err != nil {
			return Page[ProfileView]{}, queryError(err)
		}
		site := (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host}).String()
		items = append(items, ProfileView{Name: name, Site: site, Organization: profile.Organization, Current: name == cfg.CurrentProfile, LoggedIn: status.LoggedIn, Expired: status.Expired, RefreshAvailable: status.RefreshAvailable})
	}
	return pageResult(q, items, len(names), offset, key, "", ""), nil
}

func (q *Queries) ListOrganizations(ctx context.Context, input ProfilePageInput) (Page[jumpserver.Organization], error) {
	if _, err := q.profile(input.Profile); err != nil {
		return Page[jumpserver.Organization]{}, err
	}
	offset, limit, key, err := q.page(input.PageInput, "organizations", input.Profile)
	if err != nil {
		return Page[jumpserver.Organization]{}, err
	}
	items, err := q.resources.ListOrganizations(ctx, input.Profile)
	if err != nil {
		return Page[jumpserver.Organization]{}, queryError(err)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return pageResult(q, window(items, offset, limit), len(items), offset, key, input.Profile, ""), nil
}

func (q *Queries) ListAssets(ctx context.Context, input AssetsInput) (Page[AssetView], error) {
	if err := q.target(input.Profile, input.Organization); err != nil {
		return Page[AssetView]{}, err
	}
	if len(input.Search) > 1024 {
		return Page[AssetView]{}, invalid("搜索条件过长")
	}
	offset, limit, key, err := q.page(input.PageInput, "assets", input.Profile, input.Organization, input.Search)
	if err != nil {
		return Page[AssetView]{}, err
	}
	page, err := q.resources.ListAssets(ctx, input.Profile, input.Organization, input.Search, offset, limit)
	if err != nil {
		return Page[AssetView]{}, queryError(err)
	}
	if len(page.Results) > limit || page.Count < 0 || (len(page.Results) == 0 && page.Count > offset) {
		return Page[AssetView]{}, queryError(errors.New("invalid pagination"))
	}
	items := make([]AssetView, 0, len(page.Results))
	for _, asset := range page.Results {
		items = append(items, assetView(asset))
	}
	return pageResult(q, items, page.Count, offset, key, input.Profile, input.Organization), nil
}

func (q *Queries) GetAsset(ctx context.Context, input AssetInput) (AssetResult, error) {
	if err := q.target(input.Profile, input.Organization); err != nil {
		return AssetResult{}, err
	}
	if !identifier.MatchString(input.AssetID) {
		return AssetResult{}, invalid("必须指定有效的 Asset ID")
	}
	asset, err := q.resources.GetAssetByID(ctx, input.Profile, input.Organization, input.AssetID)
	if err != nil {
		return AssetResult{}, queryError(err)
	}
	result := AssetResult{Profile: input.Profile, Organization: input.Organization, Asset: assetView(asset.Asset), Accounts: accountViews(asset.Accounts), Protocols: make([]ProtocolView, 0, len(asset.Protocols))}
	for _, protocol := range asset.Protocols {
		result.Protocols = append(result.Protocols, ProtocolView{Name: protocol.Name, Port: protocol.Port})
	}
	return result, nil
}

func (q *Queries) ListAccounts(ctx context.Context, input AccountsInput) (Page[AccountView], error) {
	offset, limit, key, err := q.page(input.PageInput, "accounts", input.Profile, input.Organization, input.AssetID)
	if err != nil {
		return Page[AccountView]{}, err
	}
	asset, err := q.GetAsset(ctx, AssetInput{Profile: input.Profile, Organization: input.Organization, AssetID: input.AssetID})
	if err != nil {
		return Page[AccountView]{}, err
	}
	sort.Slice(asset.Accounts, func(i, j int) bool { return asset.Accounts[i].ID < asset.Accounts[j].ID })
	return pageResult(q, window(asset.Accounts, offset, limit), len(asset.Accounts), offset, key, input.Profile, input.Organization), nil
}

func assetView(asset jumpserver.Asset) AssetView {
	return AssetView{ID: asset.ID, Name: asset.Name, Address: asset.Address, Type: asset.Type.Value, Category: asset.Category.Value}
}
func accountViews(accounts []jumpserver.Account) []AccountView {
	items := make([]AccountView, 0, len(accounts))
	for _, account := range accounts {
		actions := make([]string, 0, len(account.Actions))
		for _, action := range account.Actions {
			actions = append(actions, action.Value)
		}
		items = append(items, AccountView{ID: account.ID, Name: account.Name, Username: account.Username, Actions: actions})
	}
	return items
}
