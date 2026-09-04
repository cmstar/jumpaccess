package main

import (
	"context"
	"errors"

	desktopapp "github.com/cmstar/jumpaccess/internal/application/desktop"
	sshsessionapp "github.com/cmstar/jumpaccess/internal/application/sshsession"
	"github.com/cmstar/jumpaccess/internal/guiconfig"
	"github.com/cmstar/jumpaccess/internal/systemfont"
	"github.com/cmstar/jumpaccess/internal/systemopen"
)

func (a *desktopApp) Bootstrap() (desktopapp.BootstrapState, error) {
	return a.api.Bootstrap()
}

func (a *desktopApp) ListOrganizations(profile string) ([]desktopapp.OrganizationView, error) {
	return a.api.ListOrganizations(a.context(), profile)
}

func (a *desktopApp) ListAssets(request desktopapp.AssetListRequest) (desktopapp.AssetPage, error) {
	return a.api.ListAssets(a.context(), request)
}

func (a *desktopApp) GetAsset(request desktopapp.AssetRequest) (desktopapp.AssetDetailView, error) {
	return a.api.GetAsset(a.context(), request)
}

func (a *desktopApp) QuickSearch(request desktopapp.QuickSearchRequest) ([]desktopapp.AssetView, error) {
	return a.api.QuickSearch(a.context(), request)
}

func (a *desktopApp) AddProfile(name, siteURL string) error {
	return a.api.AddProfile(name, siteURL)
}

func (a *desktopApp) UpdateProfileURL(name, siteURL string) error {
	return a.api.UpdateProfileURL(name, siteURL)
}

func (a *desktopApp) DeleteProfile(name string) error {
	var closeErr error
	for _, session := range a.sessions.List() {
		if session.Profile == name {
			closeErr = errors.Join(closeErr, a.sessions.Close(session.ID))
		}
	}
	if a.sftp != nil {
		for _, session := range a.sftp.List() {
			if session.Profile == name {
				closeErr = errors.Join(closeErr, a.sftp.Close(session.ID))
			}
		}
	}
	if closeErr != nil {
		return closeErr
	}
	if err := a.api.DeleteProfile(name); err != nil {
		return err
	}
	return a.preferences.Update(a.context(), func(value *guiconfig.Config) error {
		remaining := make([]guiconfig.WorkspaceTab, 0, len(value.Workspace.Tabs))
		activeRemoved := false
		for _, tab := range value.Workspace.Tabs {
			if (tab.Type == "ssh" || tab.Type == "sftp") && tab.Profile == name {
				activeRemoved = activeRemoved || tab.ID == value.Workspace.ActiveTabID
				continue
			}
			remaining = append(remaining, tab)
		}
		value.Workspace.Tabs = remaining
		if activeRemoved {
			value.Workspace.ActiveTabID = ""
			if len(remaining) > 0 {
				value.Workspace.ActiveTabID = remaining[0].ID
			}
		}
		return nil
	})
}

func (a *desktopApp) UseProfile(name string) error {
	return a.api.UseProfile(name)
}

func (a *desktopApp) SetOrganization(profile, organization string) error {
	return a.api.SetOrganization(profile, organization)
}

func (a *desktopApp) CreateAlias(request desktopapp.CreateAliasRequest) (desktopapp.AliasView, error) {
	return a.api.CreateAlias(a.context(), request)
}

func (a *desktopApp) DeleteAlias(profile, name string) error {
	return a.api.DeleteAlias(profile, name)
}

func (a *desktopApp) RenameAlias(request desktopapp.RenameAliasRequest) (desktopapp.AliasView, error) {
	return a.api.RenameAlias(request)
}

func (a *desktopApp) SetAliasAccount(request desktopapp.AliasAccountRequest) error {
	return a.api.SetAliasAccount(a.context(), request)
}

func (a *desktopApp) SavePreferences(value guiconfig.Config) error {
	return a.api.SavePreferences(value)
}

func (a *desktopApp) SaveWorkspace(value guiconfig.Workspace) error {
	return a.api.SaveWorkspace(a.context(), value)
}

func (a *desktopApp) GetAuthStatus(profile string) (desktopapp.AuthStatus, error) {
	return a.api.GetAuthStatus(profile)
}

func (a *desktopApp) RefreshAuth(profile string) (desktopapp.AuthStatus, error) {
	return a.api.RefreshAuth(a.context(), profile)
}

func (a *desktopApp) StartLogin(profile string) (desktopapp.LoginAttempt, error) {
	return a.api.StartLogin(a.context(), profile)
}

func (a *desktopApp) CompleteLogin(attemptID, callbackURL string) (desktopapp.AuthStatus, error) {
	return a.api.CompleteLogin(a.context(), attemptID, callbackURL)
}

func (a *desktopApp) CancelLogin(attemptID string) error {
	return a.api.CancelLogin(attemptID)
}

func (a *desktopApp) Logout(profile string) error {
	return a.api.Logout(a.context(), profile)
}

func (a *desktopApp) LicenseText() string {
	return a.api.LicenseText()
}

func (a *desktopApp) OpenConfig() error {
	return systemopen.Open(a.core.ConfigPath)
}

func (a *desktopApp) ListMonospaceFonts() ([]string, error) {
	return systemfont.MonospacedFamilies()
}

func (a *desktopApp) StartSSHSession(request sshsessionapp.StartRequest) (sshsessionapp.StateEvent, error) {
	return a.sessions.Start(a.context(), request)
}

func (a *desktopApp) ListSSHSessions() []sshsessionapp.StateEvent {
	return a.sessions.List()
}

func (a *desktopApp) WriteSSHSession(id, data string) error {
	return a.sessions.Write(id, data)
}

func (a *desktopApp) ResizeSSHSession(id string, columns, rows int) error {
	return a.sessions.Resize(id, columns, rows)
}

func (a *desktopApp) CloseSSHSession(id string) error {
	return a.sessions.Close(id)
}

func (a *desktopApp) ResolveSSHHostKey(id string, accepted bool) error {
	return a.hostKeys.Resolve(id, accepted)
}

func (a *desktopApp) context() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}
