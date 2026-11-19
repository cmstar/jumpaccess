package mcpserver

import (
	"context"
	"errors"
	"strings"

	"github.com/cmstar/jumpaccess/internal/application/agentmcp"
	"github.com/cmstar/jumpaccess/internal/application/settings"
	"github.com/cmstar/jumpaccess/internal/config"
)

type aliasBinding struct {
	Name         string `json:"name" jsonschema:"Profile 内的别名"`
	Asset        string `json:"asset" jsonschema:"资产 ID、名称或地址"`
	Profile      string `json:"profile,omitempty" jsonschema:"省略时使用当前 Profile"`
	Account      string `json:"account,omitempty" jsonschema:"账号 ID、别名、名称或用户名，省略时清除账号绑定"`
	Organization string `json:"organization,omitempty" jsonschema:"组织 ID，省略时清除组织绑定"`
}

func (o Options) setAlias(ctx context.Context, binding aliasBinding) (aliasBinding, error) {
	profile, err := o.SetAlias(ctx, binding.Profile, binding.Name, config.Alias{Asset: binding.Asset, Account: binding.Account, Organization: binding.Organization})
	if err != nil {
		var missing *settings.ProfileNotFoundError
		if errors.As(err, &missing) {
			if missing.Name == "" {
				return aliasBinding{}, &agentmcp.Fault{Code: "PROFILE_REQUIRED", Message: "未设置当前 Profile，请显式提供 profile"}
			}
			return aliasBinding{}, &agentmcp.Fault{Code: "PROFILE_NOT_FOUND", Message: "指定的 Profile 不存在"}
		}
		if strings.TrimSpace(binding.Asset) == "" {
			return aliasBinding{}, &agentmcp.Fault{Code: "INVALID_ARGUMENT", Message: "asset 必须是非空的资产引用"}
		}
		return aliasBinding{}, err
	}
	binding.Profile = profile
	return binding, nil
}
