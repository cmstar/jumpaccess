package connect

import (
	"fmt"
	"strings"

	"github.com/cmstar/jumpaccess/internal/jumpserver"
)

// ResolvePermittedAccount 从资产授权账号中解析明确引用，拒绝重名歧义。
// 空引用、交互选择和连接伪账号由调用方按用例处理。
func ResolvePermittedAccount(accounts []jumpserver.Account, reference string) (jumpserver.Account, error) {
	if reference == "" {
		return jumpserver.Account{}, ErrAccountNotFound
	}
	var selected jumpserver.Account
	matches := 0
	for _, account := range accounts {
		if account.ID == reference || strings.EqualFold(account.Name, reference) || strings.EqualFold(account.Alias, reference) || strings.EqualFold(account.Username, reference) {
			selected = account
			matches++
		}
	}
	if matches == 0 {
		return jumpserver.Account{}, fmt.Errorf("%w: %q", ErrAccountNotFound, reference)
	}
	if matches > 1 {
		return jumpserver.Account{}, fmt.Errorf("%w: %q matched %d accounts", ErrAccountAmbiguous, reference, matches)
	}
	return selected, nil
}
