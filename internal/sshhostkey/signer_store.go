package sshhostkey

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"

	"github.com/cmstar/jumpaccess/internal/credential"
	"golang.org/x/crypto/ssh"
)

const proxyHostKeyCredential = "ssh/proxy-host-key"

type SignerStore struct {
	Backend credential.Backend
}

func (s SignerStore) LoadOrCreate() (ssh.Signer, error) {
	if s.Backend == nil {
		return nil, fmt.Errorf("native credential store is unavailable")
	}
	stored, err := s.Backend.Get(proxyHostKeyCredential)
	if err == nil {
		defer clear(stored)
		signer, parseErr := ssh.ParsePrivateKey(stored)
		if parseErr != nil {
			return nil, fmt.Errorf("parse stored ProxyCommand host key: %w", parseErr)
		}
		return signer, nil
	}
	if !errors.Is(err, credential.ErrNotFound) {
		return nil, fmt.Errorf("load ProxyCommand host key: %w", err)
	}

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ProxyCommand host key: %w", err)
	}
	block, err := ssh.MarshalPrivateKey(privateKey, "JumpAccess ProxyCommand host key")
	if err != nil {
		return nil, fmt.Errorf("encode ProxyCommand host key: %w", err)
	}
	encoded := pem.EncodeToMemory(block)
	defer clear(encoded)
	if err := s.Backend.Set(proxyHostKeyCredential, encoded); err != nil {
		return nil, fmt.Errorf("save ProxyCommand host key: %w", err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("create ProxyCommand host signer: %w", err)
	}
	return signer, nil
}
