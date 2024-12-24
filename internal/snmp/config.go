package snmp

import (
	"time"

	"github.com/influxdata/telegraf/config"
)

type ClientConfig struct {
	// Timeout to wait for a response.
	Timeout              config.Duration `toml:"timeout" json:"timeout"`
	Retries              int             `toml:"retries" json:"retries"`
	Version              uint8           `toml:"version" json:"version"`
	UnconnectedUDPSocket bool            `toml:"unconnected_udp_socket" json:"unconnected_udp_socket"`

	// Parameters for Version 1 & 2
	Community string `toml:"community" json:"community"`

	// Parameters for Version 2 & 3
	MaxRepetitions uint32 `toml:"max_repetitions" json:"max_repetitions"`

	// Parameters for Version 3
	ContextName  string        `toml:"context_name" json:"context_name"`
	SecLevel     string        `toml:"sec_level" json:"sec_level"`
	SecName      string        `toml:"sec_name" json:"sec_name"`
	AuthProtocol string        `toml:"auth_protocol" json:"auth_protocol"`
	AuthPassword config.Secret `toml:"auth_password" json:"auth_password"`
	PrivProtocol string        `toml:"priv_protocol" json:"priv_protocol"`
	PrivPassword config.Secret `toml:"priv_password" json:"priv_password"`
	EngineID     string        `toml:"-" json:"-"`
	EngineBoots  uint32        `toml:"-" json:"-"`
	EngineTime   uint32        `toml:"-" json:"-"`

	// Path to mib files
	Path       []string `toml:"path" json:"path"`
	Translator string   `toml:"-" json:"-"`
}

func DefaultClientConfig() *ClientConfig {
	return &ClientConfig{
		Timeout:        config.Duration(5 * time.Second),
		Retries:        3,
		Version:        2,
		Path:           []string{"/usr/share/snmp/mibs"},
		Translator:     "gosmi",
		Community:      "public",
		MaxRepetitions: 10,
		SecLevel:       "authNoPriv",
		SecName:        "myuser",
		AuthProtocol:   "MD5",
		AuthPassword:   config.NewSecret([]byte("pass")),
	}
}
