package config

import (
	"fmt"
	"net"
	"strconv"
)

// MCP 只保存非敏感设置；是否运行由宿主显式控制，不随配置自动启动。
type MCP struct {
	ListenHost string `toml:"listen_host" json:"listenHost"`
	Port       int    `toml:"port" json:"port"`
	AllowLAN   bool   `toml:"allow_lan" json:"allowLAN"`
	TLSCert    string `toml:"tls_cert,omitempty" json:"tlsCert"`
	TLSKey     string `toml:"tls_key,omitempty" json:"tlsKey"`
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
	if m.AllowLAN && m.TLSCert == "" {
		return fmt.Errorf("MCP 局域网访问必须配置 TLS")
	}
	return nil
}

func (m MCP) Address() string { return net.JoinHostPort(m.ListenHost, strconv.Itoa(m.Port)) }

func (m MCP) URL() string {
	scheme := "http"
	if m.TLSCert != "" {
		scheme = "https"
	}
	return scheme + "://" + m.Address() + "/mcp"
}
