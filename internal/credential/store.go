package credential

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var ErrNotFound = errors.New("credential not found")

type Backend interface {
	Get(key string) ([]byte, error)
	Set(key string, value []byte) error
	Delete(key string) error
}

type Repository struct {
	Backend       Backend
	LegacyBackend Backend
}

func (r Repository) Load(profile string) (Token, error) {
	key, err := profileKey(profile)
	if err != nil {
		return Token{}, err
	}
	data, err := r.Backend.Get(key)
	if errors.Is(err, ErrNotFound) && r.LegacyBackend != nil {
		data, err = r.LegacyBackend.Get(key)
	}
	if err != nil {
		return Token{}, err
	}
	defer clear(data)
	var token Token
	if err := json.Unmarshal(data, &token); err != nil {
		return Token{}, fmt.Errorf("decode stored credential: %w", err)
	}
	if token.AccessToken == "" {
		return Token{}, fmt.Errorf("stored credential is incomplete")
	}
	return token, nil
}

func (r Repository) Save(profile string, token Token) error {
	key, err := profileKey(profile)
	if err != nil {
		return err
	}
	if token.AccessToken == "" {
		return fmt.Errorf("OAuth access token is empty")
	}
	data, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("encode credential: %w", err)
	}
	defer clear(data)
	if err := r.Backend.Set(key, data); err != nil {
		return err
	}
	if r.LegacyBackend != nil {
		if err := r.LegacyBackend.Delete(key); err != nil && !errors.Is(err, ErrNotFound) {
			return fmt.Errorf("delete legacy credential: %w", err)
		}
	}
	return nil
}

func nativeTarget(key string) string {
	return "JumpAccess:" + key
}

func (r Repository) Delete(profile string) error {
	key, err := profileKey(profile)
	if err != nil {
		return err
	}
	removed := false
	var deleteErrors []error
	for _, backend := range []Backend{r.Backend, r.LegacyBackend} {
		if backend == nil {
			continue
		}
		err := backend.Delete(key)
		switch {
		case err == nil:
			removed = true
		case errors.Is(err, ErrNotFound):
		default:
			deleteErrors = append(deleteErrors, err)
		}
	}
	if len(deleteErrors) > 0 {
		return errors.Join(deleteErrors...)
	}
	if !removed {
		return ErrNotFound
	}
	return nil
}

func profileKey(profile string) (string, error) {
	if strings.TrimSpace(profile) == "" {
		return "", fmt.Errorf("invalid profile name")
	}
	return "oauth/" + profile, nil
}
