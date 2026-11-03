package agentmcp

import (
	"context"
	_ "embed"
	"encoding/json"
	"io"

	"golang.org/x/crypto/ssh"
)

//go:embed upload_remote.py
var uploadRemoteScript string

func remoteUploadCall(ctx context.Context, client *ssh.Client, p map[string]any, consume func(io.Reader, io.Writer) error) (json.RawMessage, []ownedRemoteFile, error) {
	return remoteFileCall(ctx, client, p, consume, uploadRemoteScript, "upload")
}
