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

// ValidateConfig checks the fuigo.yaml in dir without executing anything. It
// reports whether a config is present, the step count, and every problem found
// (not just the first). A missing fuigo.yaml is reported as found=false with no
// problems — that is a valid state (plain go install).
func ValidateConfig(dir string) (found bool, steps int, problems []string) {
	path := filepath.Join(dir, ConfigFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, 0, nil
		}
		return true, 0, []string{fmt.Sprintf("reading %s: %v", ConfigFile, err)}
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return true, 0, []string{fmt.Sprintf("invalid YAML: %v", err)}
	}
	if len(cfg.Steps) == 0 {
		return true, 0, []string{"steps must not be empty"}
	}

	for i, step := range cfg.Steps {
		n := i + 1
		if strings.TrimSpace(step.Command) == "" {
			problems = append(problems, fmt.Sprintf("step %d: empty command", n))
			continue
		}
		if !isAllowedCommand(step.Command) {
			problems = append(problems, fmt.Sprintf(
				"step %d: command %q not allowed (must start with go/npmgo/esbuild)", n, step.Command))
		}
		if step.Workdir != "" {
			if err := validateWorkdir(step.Workdir); err != nil {
				problems = append(problems, fmt.Sprintf("step %d: %v", n, err))
			}
		}
	}
	return true, len(cfg.Steps), problems
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
