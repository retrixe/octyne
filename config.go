package main

import (
	"encoding/json"

	"github.com/tailscale/hujson"
)

func StripLineCommentsFromJSON(json []byte) ([]byte, error) {
	ast, err := hujson.Parse(json)
	if err != nil {
		return nil, err
	}
	ast.Standardize()
	return ast.Pack(), nil
}

// Config is the main config for Octyne.
type Config struct {
	Port       uint16                  `json:"port"`
	UnixSocket UnixSocketConfig        `json:"unixSocket"`
	HTTPS      HTTPSConfig             `json:"https"`
	Redis      RedisConfig             `json:"redis"`
	Logging    LoggingConfig           `json:"logging"`
	Servers    map[string]ServerConfig `json:"servers"`
}

var defaultConfig = Config{
	Port: 42069,
	UnixSocket: UnixSocketConfig{
		Enabled: true,
	},
	Logging: LoggingConfig{
		Enabled: true,
		Path:    "logs",
		Actions: map[string]bool{},
	},
}

// UnmarshalJSON unmarshals Config and sets default values.
func (c *Config) UnmarshalJSON(data []byte) error {
	type alias Config // Prevent recursive calls to UnmarshalJSON.
	conf := alias(defaultConfig)
	err := json.Unmarshal(data, &conf)
	*c = Config(conf)
	return err
}

// RedisConfig contains whether or not Redis is enabled, and if so, how to connect.
type RedisConfig struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
}

// HTTPSConfig contains whether or not HTTPS is enabled, and if so, path to cert and key.
type HTTPSConfig struct {
	Enabled bool   `json:"enabled"`
	Cert    string `json:"cert"`
	Key     string `json:"key"`
}

// UnixSocketConfig contains whether or not Unix socket is enabled, and if so, path to socket.
type UnixSocketConfig struct {
	Enabled  bool   `json:"enabled"`
	Location string `json:"location"`
	Group    string `json:"group"`
}

// UnmarshalJSON unmarshals UnixSocketConfig and sets default values.
func (c *UnixSocketConfig) UnmarshalJSON(data []byte) error {
	type alias UnixSocketConfig // Prevent recursive calls to UnmarshalJSON.
	conf := alias{Enabled: true}
	err := json.Unmarshal(data, &conf)
	*c = UnixSocketConfig(conf)
	return err
}

// ServerConfig is the config for individual servers.
type ServerConfig struct {
	Enabled   bool   `json:"enabled"`
	Directory string `json:"directory"`
	Command   string `json:"command"`
}

// UnmarshalJSON unmarshals ServerConfig and sets default values.
func (c *ServerConfig) UnmarshalJSON(data []byte) error {
	type alias ServerConfig // Prevent recursive calls to UnmarshalJSON.
	conf := alias{Enabled: true}
	err := json.Unmarshal(data, &conf)
	*c = ServerConfig(conf)
	return err
}

// LoggingConfig is the config for action logging.
type LoggingConfig struct {
	Enabled bool            `json:"enabled"`
	Path    string          `json:"path"`
	Actions map[string]bool `json:"actions"`
}

// UnmarshalJSON unmarshals LoggingConfig and sets default values.
func (c *LoggingConfig) UnmarshalJSON(data []byte) error {
	type alias LoggingConfig // Prevent recursive calls to UnmarshalJSON.
	conf := alias(defaultConfig.Logging)
	err := json.Unmarshal(data, &conf)
	*c = LoggingConfig(conf)
	return err
}

// ShouldLog returns whether or not a particular action should be logged.
func (c *LoggingConfig) ShouldLog(action string) bool {
	if !c.Enabled {
		return false
	} else if c.Actions == nil {
		return true
	}
	value, exists := c.Actions[action]
	return !exists || value
}
