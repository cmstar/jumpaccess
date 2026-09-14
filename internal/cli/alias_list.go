package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	projectconfig "github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/jumpserver"
)

type aliasListEntry struct {
	name, asset, address, account, organization string
	assetRef, accountRef, organizationRef       string
}

func listAliases(ctx context.Context, deps Dependencies, profileName string, profile projectconfig.Profile) ([]aliasListEntry, error) {
	names := make([]string, 0, len(profile.Aliases))
	for name := range profile.Aliases {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, nil
	}

	service := deps.Resources
	// 未登录时仍可查看本地配置，不发起资源请求或交互登录。
	if deps.Auth != nil {
		status, err := deps.Auth.Status(profileName)
		if err != nil || !status.LoggedIn {
			service = nil
		}
	}
	organizations := map[string]string{}
	if service != nil {
		items, err := service.ListOrganizations(ctx, profileName)
		if err == nil {
			for _, item := range items {
				organizations[item.ID] = displayName(item.Name)
			}
		}
	}

	// 缓存仅用于本次输出，组织不同的相同引用必须分别解析；失败也不重复请求。
	type assetKey struct{ organization, reference string }
	type assetResult struct {
		detail jumpserver.AssetDetail
		err    error
	}
	assets := map[assetKey]assetResult{}
	entries := make([]aliasListEntry, 0, len(names))
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		alias := profile.Aliases[name]
		org := alias.Organization
		inherited := org == "" && profile.Organization != ""
		if inherited {
			org = profile.Organization
		}
		entry := aliasListEntry{
			name: name, asset: unavailableName(alias.Asset), address: "—",
			account: "未绑定", organization: "未设置",
			assetRef: alias.Asset, accountRef: alias.Account, organizationRef: org,
		}
		if alias.Account != "" {
			entry.account = aliasAccountLabel(nil, alias.Account)
		}
		if org != "" {
			entry.organization = organizations[org]
			if entry.organization == "" {
				entry.organization = unavailableName(org)
			}
			if inherited {
				entry.organization += "（继承）"
			}
		}
		if service != nil {
			key := assetKey{org, alias.Asset}
			result, exists := assets[key]
			if !exists {
				result.detail, result.err = service.FindAsset(ctx, profileName, org, alias.Asset)
				assets[key] = result
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if result.err == nil {
				entry.asset = displayName(result.detail.Name)
				if strings.TrimSpace(result.detail.Name) == "" {
					entry.asset = alias.Asset + "（名称未提供）"
				}
				if result.detail.Address != "" {
					entry.address = result.detail.Address
				}
				entry.account = aliasAccountLabel(result.detail.Accounts, alias.Account)
			}
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func aliasAccountLabel(accounts []jumpserver.Account, reference string) string {
	if reference == "" {
		return "未绑定"
	}
	switch strings.ToUpper(reference) {
	case "@INPUT", "@USER", "@ANON":
		return strings.ToUpper(reference)
	}
	var matched *jumpserver.Account
	for _, account := range accounts {
		if account.ID == reference || strings.EqualFold(account.Name, reference) || strings.EqualFold(account.Username, reference) || strings.EqualFold(account.Alias, reference) {
			if matched != nil {
				return unavailableName(reference)
			}
			matched = &account
		}
	}
	if matched == nil {
		return unavailableName(reference)
	}
	if matched.Name != "" && matched.Username != "" && matched.Name != matched.Username {
		return matched.Name + "（" + matched.Username + "）"
	}
	for _, label := range []string{matched.Name, matched.Username, matched.Alias} {
		if strings.TrimSpace(label) != "" {
			return label
		}
	}
	return reference + "（名称未提供）"
}

func displayName(name string) string {
	if strings.TrimSpace(name) == "" {
		return "名称未提供"
	}
	return name
}

func unavailableName(reference string) string {
	return reference + "（名称暂不可用）"
}

func writeAliases(output io.Writer, entries []aliasListEntry, long bool) error {
	if long {
		if len(entries) == 0 {
			_, err := fmt.Fprintln(output, "暂无 Alias")
			return err
		}
		for index, entry := range entries {
			if index > 0 {
				if _, err := fmt.Fprintln(output); err != nil {
					return err
				}
			}
			accountRef, orgRef := entry.accountRef, entry.organizationRef
			if accountRef == "" {
				accountRef = "未绑定"
			}
			if orgRef == "" {
				orgRef = "未设置"
			}
			if err := writeTable(output, []string{"ALIAS", entry.name}, [][]string{
				{"ASSET", entry.asset}, {"ASSET REF", entry.assetRef},
				{"ADDRESS", entry.address}, {"ACCOUNT", entry.account},
				{"ACCOUNT REF", accountRef}, {"ORGANIZATION", entry.organization},
				{"ORG REF", orgRef},
			}); err != nil {
				return err
			}
		}
		return nil
	}
	rows := make([][]string, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, []string{entry.name, entry.asset, entry.address, entry.account, entry.organization})
	}
	return writeTable(output, []string{"ALIAS", "ASSET", "ADDRESS", "ACCOUNT", "ORGANIZATION"}, rows)
}
