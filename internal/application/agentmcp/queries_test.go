package agentmcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cmstar/jumpaccess/internal/application/auth"
	"github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/jumpserver"
)

type queryAuth struct{}

func (queryAuth) Status(string) (auth.Status, error) {
	return auth.Status{LoggedIn: true, RefreshAvailable: true}, nil
}

type queryResources struct {
	lastProfile, lastOrg string
	fail                 error
	assets               []jumpserver.Asset
	accounts             []jumpserver.Account
}

func (r *queryResources) ListOrganizations(context.Context, string) ([]jumpserver.Organization, error) {
	return []jumpserver.Organization{{ID: "org", Name: "合成组织"}}, r.fail
}
func (r *queryResources) ListAssets(_ context.Context, profile, org, search string, offset, limit int) (jumpserver.AssetPage, error) {
	r.lastProfile, r.lastOrg = profile, org
	end := min(offset+limit, len(r.assets))
	offset = min(offset, end)
	return jumpserver.AssetPage{Count: len(r.assets), Results: r.assets[offset:end]}, r.fail
}
func (r *queryResources) GetAssetByID(_ context.Context, profile, org, id string) (jumpserver.AssetDetail, error) {
	return jumpserver.AssetDetail{Asset: jumpserver.Asset{ID: id}, Accounts: r.accounts}, r.fail
}

func queryFixture(t *testing.T) (*Queries, Settings, *queryResources) {
	t.Helper()
	s := testSettings(t)
	if err := s.Store.Update(context.Background(), func(c *config.Config) error {
		c.CurrentProfile = "human"
		c.Profiles["human"] = config.Profile{URL: "https://human.example.test", Organization: "human-org"}
		c.Profiles["agent"] = config.Profile{URL: "https://agent.example.test", Organization: "configured-org", Aliases: map[string]config.Alias{"ambiguous": {Asset: "duplicate", Organization: "org"}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	r := &queryResources{assets: []jumpserver.Asset{{ID: "one", Name: "duplicate"}, {ID: "two", Name: "duplicate"}, {ID: "three", Name: "unique"}}}
	return NewQueries("instance", s.Store, queryAuth{}, r), s, r
}

func TestQueriesRequireExplicitTargetAndBindPagination(t *testing.T) {
	q, s, resources := queryFixture(t)
	ctx := context.Background()
	input := AssetsInput{Profile: "agent", Organization: "org", PageInput: PageInput{Limit: 1}}
	page, err := q.ListAssets(ctx, input)
	if err != nil || len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatal("第一页无游标", err)
	}
	input.Cursor = page.NextCursor
	next, err := q.ListAssets(ctx, input)
	if err != nil || next.Items[0].ID != "two" {
		t.Fatal("未获取第二页", err)
	}
	input.Search = "changed"
	if _, err := q.ListAssets(ctx, input); err == nil {
		t.Fatal("游标可跨查询使用")
	}
	input.Search = ""
	q2 := NewQueries("other-instance", s.Store, queryAuth{}, resources)
	if _, err := q2.ListAssets(ctx, input); err == nil {
		t.Fatal("旧实例游标可继续使用")
	}
	for _, bad := range []AssetsInput{{Organization: "org"}, {Profile: "agent"}, {Profile: "missing", Organization: "org"}, {Profile: "agent", Organization: "org", PageInput: PageInput{Limit: 201}}} {
		if _, err := q.ListAssets(ctx, bad); err == nil {
			t.Fatal("接受了不明确的目标或越界分页")
		}
	}
	if resources.lastProfile != "agent" || resources.lastOrg != "org" {
		t.Fatal("查询使用了人类当前选择")
	}
	cfg, _ := s.Store.Load()
	if cfg.CurrentProfile != "human" || cfg.Profiles["agent"].Organization != "configured-org" {
		t.Fatal("查询改变了当前选择")
	}
}

func TestAliasesDoNotChooseFirstAmbiguousAsset(t *testing.T) {
	q, _, _ := queryFixture(t)
	page, err := q.ListAliases(context.Background(), AliasesInput{Profile: "agent"})
	if err != nil || len(page.Items) != 1 {
		t.Fatal(err)
	}
	if page.Items[0].ResolutionStatus != "ambiguous" || page.Items[0].AssetID != "" {
		t.Fatal("歧义名称被当成固定目标")
	}
}

func TestAliasAmbiguityOnLaterPageIsNotOverlooked(t *testing.T) {
	q, _, resources := queryFixture(t)
	resources.assets = make([]jumpserver.Asset, 101)
	for i := range resources.assets {
		resources.assets[i] = jumpserver.Asset{ID: fmt.Sprint(i), Name: "other"}
	}
	resources.assets[0].Name = "duplicate"
	resources.assets[100].Name = "duplicate"
	page, err := q.ListAliases(context.Background(), AliasesInput{Profile: "agent"})
	if err != nil || page.Items[0].ResolutionStatus != "ambiguous" {
		t.Fatal("后续页的同名资产被忽略", err)
	}
}

type repeatingResources struct{ *queryResources }

func (r repeatingResources) ListAssets(context.Context, string, string, string, int, int) (jumpserver.AssetPage, error) {
	return jumpserver.AssetPage{Count: 10, Results: []jumpserver.Asset{{ID: "one", Name: "duplicate"}}}, nil
}

func TestAliasDoesNotTrustStalledPagination(t *testing.T) {
	q, _, resources := queryFixture(t)
	q.resources = repeatingResources{resources}
	page, err := q.ListAliases(context.Background(), AliasesInput{Profile: "agent"})
	if err != nil || page.Items[0].ResolutionStatus != "unavailable" || page.Items[0].AssetID != "" {
		t.Fatal("不完整的分页被误认为唯一目标", err)
	}
}

func TestQueriesReturnSafeErrorsAndAccountFields(t *testing.T) {
	q, _, resources := queryFixture(t)
	resources.fail = errors.New("upstream secret-token and password")
	_, err := q.GetAsset(context.Background(), AssetInput{Profile: "agent", Organization: "org", AssetID: "asset"})
	var problem *QueryError
	if !errors.As(err, &problem) || strings.Contains(err.Error(), "secret-token") {
		t.Fatal("泄露底层错误或缺少业务错误码")
	}
	resources.fail = auth.ErrLoginRequired
	_, err = q.ListOrganizations(context.Background(), ProfilePageInput{Profile: "agent"})
	if !errors.As(err, &problem) || problem.Code != "AUTH_REQUIRED" {
		t.Fatal("认证错误不可识别", err)
	}
	resources.fail = nil
	resources.accounts = []jumpserver.Account{{ID: "account", Name: "managed", Username: "ops", Actions: []jumpserver.LabelValue{{Value: "connect"}}}}
	page, err := q.ListAccounts(context.Background(), AccountsInput{Profile: "agent", Organization: "org", AssetID: "asset"})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != "account" {
		t.Fatal("账号查询失败", err)
	}
}
