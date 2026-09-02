package desktop

import "github.com/cmstar/jumpaccess/internal/guiconfig"

type AuthStatus struct {
	LoggedIn         bool   `json:"loggedIn"`
	Expired          bool   `json:"expired"`
	RefreshAvailable bool   `json:"refreshAvailable"`
	ExpiresAt        string `json:"expiresAt"`
}

type ProfileSummary struct {
	Name         string     `json:"name"`
	URL          string     `json:"url"`
	Organization string     `json:"organization"`
	AliasCount   int        `json:"aliasCount"`
	Auth         AuthStatus `json:"auth"`
}

type BootstrapState struct {
	Version             string              `json:"version"`
	CurrentProfile      string              `json:"currentProfile"`
	CurrentOrganization string              `json:"currentOrganization"`
	Profiles            []ProfileSummary    `json:"profiles"`
	Preferences         guiconfig.Config    `json:"preferences"`
	Workspace           guiconfig.Workspace `json:"workspace"`
}

type OrganizationView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type AliasView struct {
	Name         string `json:"name"`
	Asset        string `json:"asset"`
	Account      string `json:"account"`
	Organization string `json:"organization"`
}

type AssetView struct {
	ID       string      `json:"id"`
	Name     string      `json:"name"`
	Address  string      `json:"address"`
	Type     string      `json:"type"`
	Category string      `json:"category"`
	Aliases  []AliasView `json:"aliases"`
}

type AssetPage struct {
	Count      int         `json:"count"`
	Offset     int         `json:"offset"`
	Limit      int         `json:"limit"`
	AliasCount int         `json:"aliasCount"`
	Results    []AssetView `json:"results"`
}

type AssetListRequest struct {
	Profile      string `json:"profile"`
	Organization string `json:"organization"`
	Search       string `json:"search"`
	Offset       int    `json:"offset"`
	Limit        int    `json:"limit"`
}

type AssetRequest struct {
	Profile      string `json:"profile"`
	Organization string `json:"organization"`
	Asset        string `json:"asset"`
}

type AccountView struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Alias    string `json:"alias"`
	Username string `json:"username"`
}

type ProtocolView struct {
	Name string `json:"name"`
	Port int    `json:"port"`
}

type AssetDetailView struct {
	AssetView
	Accounts  []AccountView  `json:"accounts"`
	Protocols []ProtocolView `json:"protocols"`
}

type QuickSearchRequest struct {
	Profile      string `json:"profile"`
	Organization string `json:"organization"`
	Query        string `json:"query"`
	Limit        int    `json:"limit"`
}

type CreateAliasRequest struct {
	Profile string `json:"profile"`
	Asset   string `json:"asset"`
	Name    string `json:"name"`
	Account string `json:"account"`
}

type RenameAliasRequest struct {
	Profile     string `json:"profile"`
	CurrentName string `json:"currentName"`
	NewName     string `json:"newName"`
}

type AliasAccountRequest struct {
	Profile string `json:"profile"`
	Name    string `json:"name"`
	Account string `json:"account"`
}
