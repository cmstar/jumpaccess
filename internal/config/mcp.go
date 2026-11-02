package config

import (
	"encoding/base64"
	"fmt"
	"net"
	"strconv"
)

// MCP 保存共享设置与访问 Token；是否运行由宿主显式控制。
type MCP struct {
	ListenHost string `toml:"listen_host" json:"listenHost"`
	Port       int    `toml:"port" json:"port"`
	AllowLAN   bool   `toml:"allow_lan" json:"allowLAN"`
	TLSCert    string `toml:"tls_cert,omitempty" json:"tlsCert"`
	TLSKey     string `toml:"tls_key,omitempty" json:"tlsKey"`
	Token      string `toml:"token,omitempty" json:"token,omitempty"`
}

func DefaultMCP() MCP { return MCP{ListenHost: "127.0.0.1", Port: 39287} }

func (c Config) MCPSettings() MCP {
	if c.MCP == nil {
		return DefaultMCP()
	}
	return *c.MCP
}

func (m MCP) Validate() error {
	if m.Port < 1 || m.Port > 65535 {
		return fmt.Errorf("MCP 端口必须在 1–65535 之间")
	}
	ip := net.ParseIP(m.ListenHost)
	if ip == nil && m.ListenHost != "localhost" {
		return fmt.Errorf("MCP 监听地址必须为 IP 或 localhost")
	}
	loopback := m.ListenHost == "localhost" || ip.IsLoopback()
	if !loopback && !m.AllowLAN {
		return fmt.Errorf("MCP 非回环监听需要显式允许局域网访问")
	}
	if (m.TLSCert == "") != (m.TLSKey == "") {
		return fmt.Errorf("MCP TLS 证书和私钥必须同时配置")
	}
	// 未配置的旧文件仍可读取；显式保存 MCP 设置和启动服务时要求 Token。
	if m.Token != "" {
		return ValidateMCPToken(m.Token)
	}
	return nil
}

func ValidateMCPToken(value string) error {
	if value == "" {
		return fmt.Errorf("MCP 访问 Token 不能为空")
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(value)
	defer clear(decoded)
	if err != nil || len(value) != 43 || len(decoded) != 32 {
		return fmt.Errorf("MCP Token 必须为 32 字节随机值的无填充 Base64url 编码")
	}
	return nil
}

// WithoutToken 用于实例描述等不应保存或返回访问凭据的场景。
func (m MCP) WithoutToken() MCP { m.Token = ""; return m }

func (m MCP) Address() string { return net.JoinHostPort(m.ListenHost, strconv.Itoa(m.Port)) }

func (m MCP) URL() string {
	scheme := "http"
	if m.TLSCert != "" {
		scheme = "https"
	}
	return scheme + "://" + m.Address() + "/mcp"
}
