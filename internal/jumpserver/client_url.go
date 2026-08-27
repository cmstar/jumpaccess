package jumpserver

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

func DecodeClientURL(raw string) (ClientConnection, error) {
	if !strings.HasPrefix(raw, "jms://") {
		return ClientConnection{}, fmt.Errorf("connection client URL has an unsupported scheme")
	}
	encoded := strings.TrimPrefix(raw, "jms://")
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(encoded)
	}
	if err != nil {
		return ClientConnection{}, fmt.Errorf("decode connection client URL: invalid base64")
	}
	defer clear(data)
	var connection ClientConnection
	if err := json.Unmarshal(data, &connection); err != nil {
		return ClientConnection{}, fmt.Errorf("decode connection client URL: invalid JSON")
	}
	if connection.Protocol != "ssh" {
		return ClientConnection{}, fmt.Errorf("connection protocol %q is not supported", connection.Protocol)
	}
	if strings.TrimSpace(connection.Endpoint.Host) == "" {
		return ClientConnection{}, fmt.Errorf("connection endpoint host is empty")
	}
	port := connection.Endpoint.Port
	if port < 1 || port > 65535 {
		return ClientConnection{}, fmt.Errorf("connection endpoint port is invalid")
	}
	if connection.Token.ID == "" || connection.Token.Value == "" {
		return ClientConnection{}, fmt.Errorf("connection credential is incomplete")
	}
	return connection, nil
}
