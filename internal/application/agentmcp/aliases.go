package agentmcp

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strings"

	"github.com/cmstar/jumpaccess/internal/application/connect"
	"github.com/cmstar/jumpaccess/internal/jumpserver"
)

type AliasView struct {
	Name             string `json:"name"`
	Profile          string `json:"profile"`
	Organization     string `json:"organization"`
	AssetReference   string `json:"asset_reference"`
	AccountReference string `json:"account_reference"`
	AssetID          string `json:"asset_id,omitempty"`
	AccountID        string `json:"account_id,omitempty"`
	ResolutionStatus string `json:"resolution_status"`
}

var uuid = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var errAmbiguous = errors.New("ambiguous asset")

func (q *Queries) ListAliases(ctx context.Context, input AliasesInput) (Page[AliasView], error) {
	profile, err := q.profile(input.Profile)
	if err != nil {
		return Page[AliasView]{}, err
	}
	if input.Organization != "" && !identifier.MatchString(input.Organization) {
		return Page[AliasView]{}, invalid("Organization ID 无效")
	}
	offset, limit, key, err := q.page(input.PageInput, "aliases", input.Profile, input.Organization)
	if err != nil {
		return Page[AliasView]{}, err
	}
	names := make([]string, 0, len(profile.Aliases))
	for name, alias := range profile.Aliases {
		org := alias.Organization
		if org == "" {
			org = profile.Organization
		}
		if input.Organization == "" || input.Organization == org {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	items := make([]AliasView, 0)
	for _, name := range window(names, offset, limit) {
		if err := ctx.Err(); err != nil {
			return Page[AliasView]{}, queryError(err)
		}
		alias := profile.Aliases[name]
		org := alias.Organization
		if org == "" {
			org = profile.Organization
		}
		view := AliasView{Name: name, Profile: input.Profile, Organization: org, AssetReference: alias.Asset, AccountReference: alias.Account, ResolutionStatus: "unresolved"}
		if !identifier.MatchString(org) {
			view.ResolutionStatus = "organization_required"
			items = append(items, view)
			continue
		}
		asset, err := q.uniqueAsset(ctx, input.Profile, org, alias.Asset)
		switch {
		case errors.Is(err, errAmbiguous):
			view.ResolutionStatus = "ambiguous"
		case err != nil:
			view.ResolutionStatus = "unavailable"
		default:
			view.AssetID = asset.ID
			view.ResolutionStatus = "account_required"
			if alias.Account != "" {
				account, err := connect.ResolvePermittedAccount(asset.Accounts, alias.Account)
				if err == nil && account.ID != "" && !strings.HasPrefix(account.ID, "@") {
					view.AccountID = account.ID
					view.ResolutionStatus = "resolved"
				} else {
					view.ResolutionStatus = "account_unresolved"
				}
			}
		}
		items = append(items, view)
	}
	return pageResult(q, items, len(names), offset, key, input.Profile, input.Organization), nil
}

// 名称查找必须读完候选页再确认唯一；超出查询预算时保持未解析，绝不选择首项。
func (q *Queries) uniqueAsset(ctx context.Context, profile, org, reference string) (jumpserver.AssetDetail, error) {
	if uuid.MatchString(reference) {
		return q.resources.GetAssetByID(ctx, profile, org, reference)
	}
	var match string
	offset := 0
	seen := make(map[string]bool)
	for range 100 {
		if err := ctx.Err(); err != nil {
			return jumpserver.AssetDetail{}, err
		}
		page, err := q.resources.ListAssets(ctx, profile, org, reference, offset, 100)
		if err != nil {
			return jumpserver.AssetDetail{}, err
		}
		if page.Count < 0 || len(page.Results) > 100 {
			return jumpserver.AssetDetail{}, errors.New("invalid asset page")
		}
		progress := false
		for _, asset := range page.Results {
			if asset.ID == "" {
				return jumpserver.AssetDetail{}, errors.New("missing asset ID")
			}
			if !seen[asset.ID] {
				progress = true
				seen[asset.ID] = true
			}
			if asset.ID == reference || strings.EqualFold(asset.Name, reference) || strings.EqualFold(asset.Address, reference) {
				if match != "" && match != asset.ID {
					return jumpserver.AssetDetail{}, errAmbiguous
				}
				match = asset.ID
			}
		}
		if len(page.Results) > 0 && !progress {
			return jumpserver.AssetDetail{}, errors.New("asset pagination made no progress")
		}
		offset += len(page.Results)
		if offset >= page.Count && page.Next == "" {
			if match == "" {
				return jumpserver.AssetDetail{}, connect.ErrAssetNotFound
			}
			return q.resources.GetAssetByID(ctx, profile, org, match)
		}
		if len(page.Results) == 0 {
			break
		}
	}
	return jumpserver.AssetDetail{}, errors.New("asset lookup incomplete")
}
