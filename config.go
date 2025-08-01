package main

import (
	"encoding/json"
	"os"

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
	WebUI: WebUIConfig{
		Enabled: true,
		Port:    7877,
	},
	Redis:   RedisConfig{URL: "redis://localhost", Role: "primary"}, // TODO: Remove default in 2.0
	Servers: map[string]ServerConfig{},
}

func ReadConfig() (Config, error) {
	config := defaultConfig
	contents, err := os.ReadFile(ConfigJsonPath)
	if err != nil {
		return config, err
	}
	contents, err = hujson.Standardize(contents)
	if err != nil {
		return config, err
	}
	err = json.Unmarshal(contents, &config)
	if err != nil {
		return config, err
	}
	return config, nil
}

// Config is the main config for Octyne.
type Config struct {
	Port       uint16                  `json:"port"`
	UnixSocket UnixSocketConfig        `json:"unixSocket"`
	HTTPS      HTTPSConfig             `json:"https"`
	Redis      RedisConfig             `json:"redis"`
	Logging    LoggingConfig           `json:"logging"`
	WebUI      WebUIConfig             `json:"webUI"`
	Servers    map[string]ServerConfig `json:"servers"`
}

// WebUIConfig contains whether or not the Web UI is enabled.
type WebUIConfig struct {
	Enabled bool   `json:"enabled"`
	Port    uint16 `json:"port"`
}

// RedisConfig contains whether or not Redis is enabled, and if so, how to connect.
type RedisConfig struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
	Role    string `json:"role"`
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
