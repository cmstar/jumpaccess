package jumpserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

type LabelValue struct {
	Value string `json:"value"`
	Label string `json:"label,omitempty"`
}

func (v *LabelValue) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		if err := json.Unmarshal(data, &v.Value); err != nil {
			return err
		}
		v.Label = v.Value
		return nil
	}
	type plain LabelValue
	return json.Unmarshal(data, (*plain)(v))
}

type Organization struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Asset struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Address  string     `json:"address"`
	Type     LabelValue `json:"type"`
	Category LabelValue `json:"category"`
}

type Account struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Alias    string `json:"alias"`
	Username string `json:"username"`
}

type Protocol struct {
	Name string `json:"name"`
	Port int    `json:"port"`
}

type AssetDetail struct {
	Asset
	Accounts  []Account  `json:"permed_accounts"`
	Protocols []Protocol `json:"permed_protocols"`
}

type AssetPage struct {
	Count    int     `json:"count"`
	Next     string  `json:"next"`
	Previous string  `json:"previous"`
	Results  []Asset `json:"results"`
}

type AssetQuery struct {
	Type     string
	Category string
	Offset   int
	Limit    int
	Search   string
	Order    string
}

type ConnectionOptions struct {
	TokenReusable   bool `json:"token_reusable"`
	DisableAutoHash bool `json:"disableautohash"`
}

type ConnectionRequest struct {
	Asset          string            `json:"asset"`
	Account        string            `json:"account"`
	Protocol       string            `json:"protocol"`
	InputUsername  string            `json:"input_username"`
	InputSecret    string            `json:"input_secret"`
	ConnectMethod  string            `json:"connect_method"`
	ConnectOptions ConnectionOptions `json:"connect_options"`
}

type Endpoint struct {
	Host string `json:"host"`
	Port int    `json:"-"`
}

func (e *Endpoint) UnmarshalJSON(data []byte) error {
	var decoded struct {
		Host string  `json:"host"`
		Port flexInt `json:"port"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	e.Host = decoded.Host
	e.Port = int(decoded.Port)
	return nil
}

type ConnectionCredential struct {
	ID    string `json:"id"`
	Value string `json:"value"`
}

type ClientConnection struct {
	Protocol string               `json:"protocol"`
	Asset    Asset                `json:"asset"`
	Endpoint Endpoint             `json:"endpoint"`
	Token    ConnectionCredential `json:"token"`
}

func (c ClientConnection) Username() string { return "JMS-" + c.Token.ID }
func (c ClientConnection) Password() string { return c.Token.Value }

type flexInt int

func (v *flexInt) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var raw any
	if err := decoder.Decode(&raw); err != nil {
		return err
	}
	var parsed int64
	var err error
	switch value := raw.(type) {
	case json.Number:
		parsed, err = value.Int64()
	case string:
		parsed, err = strconv.ParseInt(value, 10, 32)
	default:
		err = fmt.Errorf("port must be a number or numeric string")
	}
	if err != nil {
		return err
	}
	*v = flexInt(parsed)
	return nil
}
