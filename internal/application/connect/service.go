package connect

import (
	"context"
	"errors"
	"fmt"
	"strings"

	projectconfig "github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/credential"
	"github.com/cmstar/jumpaccess/internal/jumpserver"
	"github.com/cmstar/jumpaccess/internal/target"
)

var (
	ErrAssetNotFound                 = errors.New("asset not found")
	ErrAssetAmbiguous                = errors.New("asset is ambiguous")
	ErrAccountNotFound               = errors.New("account not found")
	ErrAccountAmbiguous              = errors.New("account is ambiguous")
	ErrInteractiveCredentialRequired = errors.New("interactive account credential is required")
)

type ConfigLoader interface {
	Load() (projectconfig.Config, error)
}

type TokenManager interface {
	EnsureFresh(context.Context, string) (credential.Token, error)
}

type AssetAPI interface {
	ListAssets(context.Context, jumpserver.AssetQuery) (jumpserver.AssetPage, error)
	GetAsset(context.Context, string) (jumpserver.AssetDetail, error)
}

type API interface {
	AssetAPI
	CreateConnectionToken(context.Context, jumpserver.ConnectionRequest) (string, error)
	GetClientConnection(context.Context, string) (jumpserver.ClientConnection, error)
}

type Options struct {
	Target         target.Input
	Protocol       string
	NonInteractive bool
	SelectAccount  func([]jumpserver.Account) (jumpserver.Account, error)
}

type Prepared struct {
	Selection  target.Selection
	Asset      jumpserver.AssetDetail
	Account    jumpserver.Account
	Connection jumpserver.ClientConnection
}

type Service struct {
	Config ConfigLoader
	Tokens TokenManager
	NewAPI func(site, accessToken, organization string) (API, error)
}

func (s Service) Prepare(ctx context.Context, options Options) (Prepared, error) {
	protocol := options.Protocol
	if protocol == "" {
		protocol = "ssh"
	}
	if protocol != "ssh" && protocol != "sftp" {
		return Prepared{}, fmt.Errorf("connection protocol %q is not supported", protocol)
	}
	configuration, err := s.Config.Load()
	if err != nil {
		return Prepared{}, err
	}
	selection, err := target.Resolve(configuration, options.Target)
	if err != nil {
		return Prepared{}, err
	}
	token, err := s.Tokens.EnsureFresh(ctx, selection.Profile)
	if err != nil {
		return Prepared{}, err
	}
	if s.NewAPI == nil {
		return Prepared{}, fmt.Errorf("JumpServer API client is unavailable")
	}
	api, err := s.NewAPI(selection.SiteURL, token.AccessToken, selection.Organization)
	if err != nil {
		return Prepared{}, err
	}

	asset, err := ResolveAsset(ctx, api, selection.Asset)
	if err != nil {
		return Prepared{}, err
	}
	if !supportsProtocol(asset.Protocols, protocol) {
		return Prepared{}, fmt.Errorf("asset %q does not permit %s", asset.Name, strings.ToUpper(protocol))
	}
	account, err := resolveAccount(asset.Accounts, selection.Account, options)
	if err != nil {
		return Prepared{}, err
	}
	accountID := account.ID
	if accountID == "" {
		accountID = account.Username
	}
	if requiresInputCredential(account) {
		return Prepared{}, fmt.Errorf("%w for account %q; choose a managed account", ErrInteractiveCredentialRequired, accountID)
	}
	connectionTokenID, err := api.CreateConnectionToken(ctx, jumpserver.ConnectionRequest{Asset: asset.ID, Account: accountID, Protocol: protocol, ConnectMethod: protocol + "_client"})
	if err != nil {
		return Prepared{}, err
	}
	connection, err := api.GetClientConnection(ctx, connectionTokenID)
	if err != nil {
		return Prepared{}, err
	}
	if connection.Protocol != protocol {
		return Prepared{}, fmt.Errorf("connection protocol %q does not match requested %q", connection.Protocol, protocol)
	}
	return Prepared{Selection: selection, Asset: asset, Account: account, Connection: connection}, nil
}

func ResolveAsset(ctx context.Context, api AssetAPI, reference string) (jumpserver.AssetDetail, error) {
	if isUUID(reference) {
		return api.GetAsset(ctx, reference)
	}
	page, err := api.ListAssets(ctx, jumpserver.AssetQuery{Search: reference, Limit: 100})
	if err != nil {
		return jumpserver.AssetDetail{}, err
	}
	matches := make([]jumpserver.Asset, 0, 1)
	for _, asset := range page.Results {
		if asset.ID == reference || strings.EqualFold(asset.Name, reference) || strings.EqualFold(asset.Address, reference) {
			matches = append(matches, asset)
		}
	}
	if len(matches) == 0 {
		return jumpserver.AssetDetail{}, fmt.Errorf("%w: %q", ErrAssetNotFound, reference)
	}
	if len(matches) > 1 {
		return jumpserver.AssetDetail{}, fmt.Errorf("%w: %q matched %d assets", ErrAssetAmbiguous, reference, len(matches))
	}
	return api.GetAsset(ctx, matches[0].ID)
}

func isUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index := range value {
		switch index {
		case 8, 13, 18, 23:
			if value[index] != '-' {
				return false
			}
		default:
			character := value[index]
			if !((character >= '0' && character <= '9') ||
				(character >= 'a' && character <= 'f') ||
				(character >= 'A' && character <= 'F')) {
				return false
			}
		}
	}
	return true
}

func resolveAccount(accounts []jumpserver.Account, reference string, options Options) (jumpserver.Account, error) {
	if isPseudoAccount(reference) {
		return jumpserver.Account{ID: strings.ToUpper(reference), Username: strings.ToUpper(reference)}, nil
	}
	if reference != "" {
		matches := make([]jumpserver.Account, 0, 1)
		for _, account := range accounts {
			if account.ID == reference || strings.EqualFold(account.Name, reference) || strings.EqualFold(account.Alias, reference) || strings.EqualFold(account.Username, reference) {
				matches = append(matches, account)
			}
		}
		if len(matches) == 0 {
			return jumpserver.Account{}, fmt.Errorf("%w: %q", ErrAccountNotFound, reference)
		}
		if len(matches) > 1 {
			return jumpserver.Account{}, fmt.Errorf("%w: %q matched %d accounts", ErrAccountAmbiguous, reference, len(matches))
		}
		return matches[0], nil
	}
	if len(accounts) == 0 {
		return jumpserver.Account{}, ErrAccountNotFound
	}
	if len(accounts) == 1 {
		return accounts[0], nil
	}
	if options.NonInteractive || options.SelectAccount == nil {
		return jumpserver.Account{}, fmt.Errorf("%w: choose one of %d permitted accounts", ErrAccountAmbiguous, len(accounts))
	}
	return options.SelectAccount(accounts)
}

func supportsProtocol(protocols []jumpserver.Protocol, expected string) bool {
	for _, protocol := range protocols {
		if strings.EqualFold(protocol.Name, expected) {
			return true
		}
	}
	return false
}

func isPseudoAccount(value string) bool {
	switch strings.ToUpper(value) {
	case "@INPUT", "@USER", "@ANON":
		return true
	default:
		return false
	}
}

func requiresInputCredential(account jumpserver.Account) bool {
	value := strings.ToUpper(account.ID)
	if value == "" {
		value = strings.ToUpper(account.Username)
	}
	return value == "@INPUT" || value == "@USER"
}
