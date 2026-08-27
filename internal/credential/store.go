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
	Backend Backend
}

func (r Repository) Load(profile string) (Token, error) {
	key, err := profileKey(profile)
	if err != nil {
		return Token{}, err
	}
	data, err := r.Backend.Get(key)
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
	return r.Backend.Set(key, data)
}

func nativeTarget(key string) string {
	return "JumpAccess:" + key
}

func (r Repository) Delete(profile string) error {
	key, err := profileKey(profile)
	if err != nil {
		return err
	}
	return r.Backend.Delete(key)
}

func profileKey(profile string) (string, error) {
	if strings.TrimSpace(profile) == "" || strings.ContainsAny(profile, `/\`) || profile == "." || profile == ".." {
		return "", fmt.Errorf("invalid profile name")
	}
	return "oauth/" + profile, nil
}
