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
	Steps []Step `yaml:"steps"`
}

// Step is a single pre-build step. It is either a bare command (string form)
// or a command plus a workdir (map form). Workdir is a path relative to the
// module root; an empty Workdir means the module root itself.
type Step struct {
	Command string
	Workdir string
}

// UnmarshalYAML accepts a step as either a scalar string ("go generate ./...")
// or a mapping ({command: ..., workdir: ...}).
func (s *Step) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		s.Command = value.Value
		return nil
	case yaml.MappingNode:
		var aux struct {
			Command string `yaml:"command"`
			Workdir string `yaml:"workdir"`
		}
		if err := value.Decode(&aux); err != nil {
			return err
		}
		s.Command = aux.Command
		s.Workdir = aux.Workdir
		return nil
	default:
		return fmt.Errorf("step must be a string or a {command, workdir} mapping")
	}
}

// String renders a step for display, noting the workdir when present.
func (s Step) String() string {
	if s.Workdir != "" {
		return fmt.Sprintf("%s (in %s)", s.Command, s.Workdir)
	}
	return s.Command
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
		if strings.TrimSpace(step.Command) == "" {
			return nil, fmt.Errorf("%s: step %d has an empty command", ConfigFile, i+1)
		}
		if !isAllowedCommand(step.Command) {
			return nil, fmt.Errorf("%s: step %d %q must start with one of %s",
				ConfigFile, i+1, step.Command, strings.Join(allowedCommands, ", "))
		}
	}
	return &cfg, nil
}

// isAllowedCommand reports whether command's first token is an allowed command.
func isAllowedCommand(command string) bool {
	fields := strings.Fields(command)
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
