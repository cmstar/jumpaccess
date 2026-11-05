package agentmcp

import (
	"context"
	"errors"
	"strings"

	"github.com/cmstar/jumpaccess/internal/application/connect"
	"github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/jumpserver"
	"github.com/cmstar/jumpaccess/internal/target"
)

func validateTargetForm(in Target) error {
	forms := 0
	for _, reference := range []string{in.Name, in.Alias, in.AssetID} {
		if reference != "" {
			forms++
		}
	}
	if forms != 1 || (in.Name != "" && (strings.TrimSpace(in.Name) == "" || len(in.Name) > 1024)) {
		return fault("INVALID_ARGUMENT", "请提供 name、alias 或完整资产 ID 目标，三种形式不能混用；name 最多 1024 字节")
	}
	if in.Alias != "" && (in.Organization != "" || in.AccountID != "") {
		return fault("INVALID_ARGUMENT", "alias 与组织或账号覆盖不能混用，需要覆盖时使用 name")
	}
	if in.Organization != "" && !identifier.MatchString(in.Organization) {
		return fault("INVALID_ARGUMENT", "Organization ID 无效")
	}
	if in.AccountID != "" && !identifier.MatchString(in.AccountID) {
		return fault("INVALID_ARGUMENT", "需要托管账号 ID，不接受交互伪账号")
	}
	return nil
}

func (c Connector) resolveNamedTarget(ctx context.Context, cfg config.Config, in Target) (Target, error) {
	reference := in.Name
	if in.Alias != "" {
		reference = in.Alias
		if _, exists := cfg.Profiles[in.Profile].Aliases[reference]; !exists {
			return Target{}, &Fault{Code: "TARGET_NOT_FOUND", Message: "Alias 不存在", Resolution: &ResolutionHint{Profile: in.Profile, NextTool: "list_aliases"}}
		}
	}
	// 默认值和别名优先级复用 CLI；资产和账号仍需按 MCP 的唯一性约束解析。
	selection, err := target.Resolve(cfg, target.Input{Target: reference, Profile: in.Profile, Organization: in.Organization, Account: in.AccountID})
	if err != nil {
		return Target{}, fault("INVALID_ARGUMENT", "无法解析目标引用")
	}
	hint := &ResolutionHint{Profile: in.Profile, Organization: selection.Organization, NextTool: "list_organizations"}
	fail := func(code, message string) (Target, error) {
		return Target{}, &Fault{Code: code, Message: message, Resolution: hint}
	}
	if selection.Organization == "" {
		return fail("TARGET_INCOMPLETE", "未指定组织且 Profile 未设置当前组织，请查询后提供 organization")
	}
	if !identifier.MatchString(selection.Organization) || strings.TrimSpace(selection.Asset) == "" {
		return fail("INVALID_ARGUMENT", "配置中的组织或资产引用无效")
	}
	hint.NextTool = "list_assets"
	asset, err := c.Queries.uniqueAsset(ctx, in.Profile, selection.Organization, selection.Asset)
	if err != nil {
		var ambiguous *ambiguousAssets
		switch {
		case errors.As(err, &ambiguous):
			hint.Assets, hint.Truncated = ambiguous.candidates, true
			return fail("TARGET_AMBIGUOUS", "资产引用匹配多个目标，候选只用于提示；请查询并明确资产 ID")
		case errors.Is(err, connect.ErrAssetNotFound):
			return fail("TARGET_NOT_FOUND", "未找到匹配资产，请检查名称或使用 list_assets")
		default:
			return Target{}, queryError(err)
		}
	}
	if !uuid.MatchString(asset.ID) || (uuid.MatchString(selection.Asset) && asset.ID != selection.Asset) {
		return Target{}, fault("RESOURCE_UNAVAILABLE", "资产查询未返回预期的有效 ID")
	}
	hint.AssetID, hint.NextTool = asset.ID, "list_accounts"
	accounts := make([]jumpserver.Account, 0, len(asset.Accounts))
	for _, account := range asset.Accounts {
		if identifier.MatchString(account.ID) {
			accounts = append(accounts, account)
		}
	}
	var account jumpserver.Account
	if in.AccountID != "" {
		// account_id 只能匹配 ID，不能意外匹配另一个账号的显示名称。
		for _, candidate := range accounts {
			if candidate.ID == in.AccountID {
				if account.ID != "" {
					return fail("TARGET_AMBIGUOUS", "授权账号 ID 重复，无法唯一确定")
				}
				account = candidate
			}
		}
		if account.ID == "" {
			return fail("TARGET_NOT_FOUND", "未找到指定托管账号，请使用 list_accounts")
		}
	} else if selection.Account != "" {
		account, err = connect.ResolvePermittedAccount(accounts, selection.Account)
		if err != nil {
			hint.Accounts, hint.Truncated = accountViews(accounts[:min(len(accounts), 20)]), len(accounts) > 20
			if errors.Is(err, connect.ErrAccountAmbiguous) {
				return fail("TARGET_AMBIGUOUS", "Alias 账号无法唯一确定，请显式提供 account_id")
			}
			return fail("TARGET_NOT_FOUND", "Alias 未匹配到可用托管账号，请显式提供 account_id")
		}
	} else {
		switch len(accounts) {
		case 0:
			return fail("TARGET_INCOMPLETE", "资产没有可用托管账号，不支持交互输入凭据")
		case 1:
			account = accounts[0]
		default:
			hint.Accounts, hint.Truncated = accountViews(accounts[:min(len(accounts), 20)]), len(accounts) > 20
			return fail("TARGET_AMBIGUOUS", "存在多个托管账号，请显式提供 account_id")
		}
	}
	return Target{Profile: in.Profile, Organization: selection.Organization, AssetID: asset.ID, AccountID: account.ID}, nil
}
