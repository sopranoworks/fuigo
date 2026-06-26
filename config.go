package fuigo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ConfigFile is the name of the per-module configuration file fuigo looks for.
const ConfigFile = "fuigo.yaml"

// allowedCommands are the only step prefixes fuigo will execute. "go" runs the
// external go tool; "npmgo" and "esbuild" are compiled-in built-in commands.
var allowedCommands = []string{"go", "npmgo", "esbuild"}

// Config is the parsed contents of a module's fuigo.yaml.
type Config struct {
	Steps []string `yaml:"steps"`
}

// LoadConfig reads fuigo.yaml from dir. If the file does not exist it returns
// (nil, nil): a module without a fuigo.yaml is valid and means "plain go
// install". Any present config is validated: steps must be non-empty and every
// step must start with one of the allowed commands.
func LoadConfig(dir string) (*Config, error) {
	path := filepath.Join(dir, ConfigFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", ConfigFile, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", ConfigFile, err)
	}

	if len(cfg.Steps) == 0 {
		return nil, fmt.Errorf("%s: steps must not be empty", ConfigFile)
	}
	for i, step := range cfg.Steps {
		if !isAllowedStep(step) {
			return nil, fmt.Errorf("%s: step %d %q must start with one of %s",
				ConfigFile, i+1, step, strings.Join(allowedCommands, ", "))
		}
	}
	return &cfg, nil
}

// isAllowedStep reports whether step's first token is an allowed command.
func isAllowedStep(step string) bool {
	fields := strings.Fields(step)
	if len(fields) == 0 {
		return false
	}
	for _, cmd := range allowedCommands {
		if fields[0] == cmd {
			return true
		}
	}
	return false
}
