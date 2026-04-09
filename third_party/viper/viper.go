package viper

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Viper struct {
	configName   string
	configType   string
	configPaths  []string
	envPrefix    string
	envReplacer  *strings.Replacer
	autoEnv      bool
	defaults     map[string]any
	configValues map[string]any
}

type ConfigFileNotFoundError struct{}

func (e ConfigFileNotFoundError) Error() string {
	return "config file not found"
}

func New() *Viper {
	return &Viper{
		defaults: make(map[string]any),
	}
}

func (v *Viper) SetConfigName(name string) {
	v.configName = name
}

func (v *Viper) SetConfigType(configType string) {
	v.configType = configType
}

func (v *Viper) AddConfigPath(path string) {
	v.configPaths = append(v.configPaths, path)
}

func (v *Viper) SetEnvPrefix(prefix string) {
	v.envPrefix = prefix
}

func (v *Viper) SetEnvKeyReplacer(replacer *strings.Replacer) {
	v.envReplacer = replacer
}

func (v *Viper) AutomaticEnv() {
	v.autoEnv = true
}

func (v *Viper) SetDefault(key string, value any) {
	assignNestedValue(v.defaults, key, value)
}

func (v *Viper) ReadInConfig() error {
	filename := v.configName
	if v.configType != "" {
		filename = fmt.Sprintf("%s.%s", filename, v.configType)
	}

	for _, configPath := range v.configPaths {
		fullPath := filepath.Join(configPath, filename)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}

		values, err := parseSimpleYAML(string(data))
		if err != nil {
			return err
		}

		v.configValues = values
		return nil
	}

	return ConfigFileNotFoundError{}
}

func (v *Viper) Unmarshal(target any) error {
	merged := deepCopyMap(v.defaults)
	deepMerge(merged, v.configValues)

	if v.autoEnv {
		applyEnvOverrides(merged, "", v.envPrefix, v.envReplacer)
	}

	data, err := json.Marshal(merged)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, target)
}

func applyEnvOverrides(values map[string]any, prefix string, envPrefix string, replacer *strings.Replacer) {
	for key, value := range values {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}

		if nested, ok := value.(map[string]any); ok {
			applyEnvOverrides(nested, path, envPrefix, replacer)
		}

		envKey := path
		if replacer != nil {
			envKey = replacer.Replace(envKey)
		}
		envKey = strings.ToUpper(envKey)
		if envPrefix != "" {
			envKey = envPrefix + "_" + envKey
		}

		if envValue, ok := os.LookupEnv(envKey); ok {
			values[key] = parseOverrideValue(envValue, value)
		}
	}
}

func assignNestedValue(values map[string]any, path string, value any) {
	parts := strings.Split(path, ".")
	current := values

	for _, part := range parts[:len(parts)-1] {
		next, ok := current[part].(map[string]any)
		if !ok {
			next = make(map[string]any)
			current[part] = next
		}
		current = next
	}

	current[parts[len(parts)-1]] = value
}

func deepCopyMap(values map[string]any) map[string]any {
	if values == nil {
		return map[string]any{}
	}

	copyMap := make(map[string]any, len(values))
	for key, value := range values {
		if nested, ok := value.(map[string]any); ok {
			copyMap[key] = deepCopyMap(nested)
			continue
		}
		copyMap[key] = value
	}

	return copyMap
}

func deepMerge(dst map[string]any, src map[string]any) {
	for key, value := range src {
		srcNested, srcIsMap := value.(map[string]any)
		dstNested, dstIsMap := dst[key].(map[string]any)

		if srcIsMap && dstIsMap {
			deepMerge(dstNested, srcNested)
			continue
		}

		dst[key] = value
	}
}

func parseSimpleYAML(content string) (map[string]any, error) {
	root := map[string]any{}
	stack := []map[string]any{root}

	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimRight(rawLine, " \t")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		indent := countLeadingSpaces(line)
		level := indent / 2
		if indent%2 != 0 {
			return nil, fmt.Errorf("invalid indentation: %q", rawLine)
		}
		if level >= len(stack) {
			return nil, fmt.Errorf("invalid nesting: %q", rawLine)
		}

		stack = stack[:level+1]
		current := stack[level]

		parts := strings.SplitN(strings.TrimSpace(line), ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid line: %q", rawLine)
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		if value == "" {
			child := map[string]any{}
			current[key] = child
			stack = append(stack, child)
			continue
		}

		current[key] = parseScalar(value)
	}

	return root, nil
}

func parseScalar(value string) any {
	unquoted := strings.Trim(value, "\"")

	if strings.HasPrefix(unquoted, "[") && strings.HasSuffix(unquoted, "]") {
		var parsed any
		if err := json.Unmarshal([]byte(unquoted), &parsed); err == nil {
			return parsed
		}
	}

	if intValue, err := strconv.Atoi(unquoted); err == nil {
		return intValue
	}

	switch strings.ToLower(unquoted) {
	case "true":
		return true
	case "false":
		return false
	}

	return unquoted
}

func parseOverrideValue(value string, existing any) any {
	switch existing.(type) {
	case []any, []string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return []string{}
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			var parsed any
			if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
				return parsed
			}
		}

		parts := strings.Split(trimmed, ",")
		items := make([]string, 0, len(parts))
		for _, part := range parts {
			item := strings.TrimSpace(part)
			if item == "" {
				continue
			}
			items = append(items, item)
		}
		return items
	default:
		return parseScalar(value)
	}
}

func countLeadingSpaces(line string) int {
	count := 0
	for _, char := range line {
		if char != ' ' {
			break
		}
		count++
	}
	return count
}
