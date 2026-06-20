package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

const DefaultDataDir = "./.gramli"

type Settings struct {
	ConfigPath string
	DataDir    string
	DBPath     string
	LogLevel   string
	LogFile    string
	Quiet      bool
	Verbose    bool
	JSON       bool
	NoColor    bool
	Yes        bool
	DryRun     bool
}

func DefaultConfigYAML(dataDir string) string {
	return fmt.Sprintf(`app:
  data_dir: %s
  active_account: default
logging:
  level: info
  file: %s
  color: true
database:
  path: %s
auth:
  session_dir: %s
  browser: default
  login_timeout: 5m
sync:
  default_limit: 100
  metadata_only: true
  rate_limit: 20rpm
  delay: 2s
  jitter: 1s
downloads:
  output_dir: %s
  concurrency: 2
  skip_existing: true
  filename_template: "{collection}/{owner}/{shortcode}/{index}_{type}.{ext}"
  write_metadata: true
exports:
  output_dir: %s
  pretty_json: true
`, dataDir, filepath.ToSlash(filepath.Join(dataDir, "logs", "gramli.log")), filepath.ToSlash(filepath.Join(dataDir, "gramli.db")), filepath.ToSlash(filepath.Join(dataDir, "sessions")), filepath.ToSlash(filepath.Join(dataDir, "downloads")), filepath.ToSlash(filepath.Join(dataDir, "exports")))
}

func Resolve(s Settings) Settings {
	if s.DataDir == "" {
		s.DataDir = DefaultDataDir
	}
	if s.ConfigPath == "" {
		s.ConfigPath = filepath.Join(s.DataDir, "config.yaml")
	}
	if s.DBPath == "" {
		s.DBPath = filepath.Join(s.DataDir, "gramli.db")
	}
	if s.LogLevel == "" {
		s.LogLevel = "info"
	}
	if s.LogFile == "" {
		s.LogFile = filepath.Join(s.DataDir, "logs", "gramli.log")
	}
	return s
}

func EnsureDataDirs(dataDir string) error {
	for _, dir := range []string{
		dataDir,
		filepath.Join(dataDir, "sessions"),
		filepath.Join(dataDir, "cache"),
		filepath.Join(dataDir, "logs"),
		filepath.Join(dataDir, "exports"),
		filepath.Join(dataDir, "downloads"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func WriteDefaultConfig(path, dataDir string, force bool) error {
	if _, err := os.Stat(path); err == nil && !force {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(DefaultConfigYAML(dataDir)), 0o644)
}

// SetValue updates a dotted key (e.g. "downloads.concurrency") in the YAML
// config file in place, creating intermediate maps as needed. Values are typed
// as bool/int when they parse cleanly, otherwise stored as strings.
func SetValue(path, key, value string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	root := map[string]any{}
	if len(b) > 0 {
		if err := yaml.Unmarshal(b, &root); err != nil {
			return err
		}
	}
	parts := strings.Split(key, ".")
	m := root
	for i, p := range parts {
		if i == len(parts)-1 {
			m[p] = inferType(value)
			break
		}
		next, ok := m[p].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[p] = next
		}
		m = next
	}
	out, err := yaml.Marshal(root)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

func inferType(value string) any {
	if b, err := strconv.ParseBool(value); err == nil && (value == "true" || value == "false") {
		return b
	}
	if n, err := strconv.ParseInt(value, 10, 64); err == nil {
		return n
	}
	return value
}

func Load(path string) (*viper.Viper, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	v.SetDefault("auth.login_timeout", 5*time.Minute)
	if _, err := os.Stat(path); err != nil {
		return v, nil
	}
	return v, v.ReadInConfig()
}
