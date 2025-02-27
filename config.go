package main

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/tailscale/hujson"
)

var defaultConfig = Config{
	Port: 42069,
	UnixSocket: UnixSocketConfig{
		Enabled: true,
	},
	Logging: LoggingConfig{
		Enabled: true,
		Path:    "logs",
	},
	Servers: map[string]ServerConfig{},
}

func isJSON(data []byte) bool {
	return strings.HasPrefix(strings.TrimSpace(string(data)), "{")
}

func ReadConfig() (Config, error) {
	config := defaultConfig
	contents, err := os.ReadFile(ConfigFilePath)
	if err != nil {
		return config, err
	}
	if isJSON(contents) {
		contents, err = hujson.Standardize(contents)
		if err != nil {
			return config, err
		}
		err = json.Unmarshal(contents, &config)
		if err != nil {
			return config, err
		}
	} else {
		err = toml.Unmarshal(contents, &config)
		if err != nil {
			return config, err
		}
	}
	return config, nil
}

// Config is the main config for Octyne.
type Config struct {
	Port       uint16                  `json:"port" toml:"port"`
	UnixSocket UnixSocketConfig        `json:"unixSocket" toml:"unixSocket"`
	HTTPS      HTTPSConfig             `json:"https" toml:"https"`
	Redis      RedisConfig             `json:"redis" toml:"redis"`
	Logging    LoggingConfig           `json:"logging" toml:"logging"`
	Servers    map[string]ServerConfig `json:"servers" toml:"servers"`
}

// RedisConfig contains whether or not Redis is enabled, and if so, how to connect.
type RedisConfig struct {
	Enabled bool   `json:"enabled" toml:"enabled"`
	URL     string `json:"url" toml:"url"`
}

// HTTPSConfig contains whether or not HTTPS is enabled, and if so, path to cert and key.
type HTTPSConfig struct {
	Enabled bool   `json:"enabled" toml:"enabled"`
	Cert    string `json:"cert" toml:"cert"`
	Key     string `json:"key" toml:"key"`
}

// UnixSocketConfig contains whether or not Unix socket is enabled, and if so, path to socket.
type UnixSocketConfig struct {
	Enabled  bool   `json:"enabled" toml:"enabled"`
	Location string `json:"location" toml:"location"`
	Group    string `json:"group" toml:"group"`
}

// ServerConfig is the config for individual servers.
type ServerConfig struct {
	Enabled   bool   `json:"enabled" toml:"enabled"`
	Directory string `json:"directory" toml:"directory"`
	Command   string `json:"command" toml:"command"`
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
	Enabled bool            `json:"enabled" toml:"enabled"`
	Path    string          `json:"path" toml:"path"`
	Actions map[string]bool `json:"actions" toml:"actions"`
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
