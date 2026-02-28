package resolver

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// aliasesFilename is the name of the aliases JSON file within
// the config directory.
const aliasesFilename = "aliases.json"

// aliasesPath returns the full path to the aliases file.
func aliasesPath(configDir string) string {
	return filepath.Join(configDir, aliasesFilename)
}

// LoadAliases reads the aliases map from configDir/aliases.json.
// Returns an empty map if the file doesn't exist.
func LoadAliases(configDir string) (map[string]string, error) {
	data, err := os.ReadFile(aliasesPath(configDir))
	if os.IsNotExist(err) {
		return make(map[string]string), nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading aliases: %w", err)
	}

	var aliases map[string]string
	if err := json.Unmarshal(data, &aliases); err != nil {
		return nil, fmt.Errorf("parsing aliases: %w", err)
	}
	if aliases == nil {
		aliases = make(map[string]string)
	}
	return aliases, nil
}

// SaveAliases writes the aliases map to configDir/aliases.json.
func SaveAliases(configDir string, aliases map[string]string) error {
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := json.MarshalIndent(aliases, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling aliases: %w", err)
	}

	if err := os.WriteFile(aliasesPath(configDir), data, 0644); err != nil {
		return fmt.Errorf("writing aliases: %w", err)
	}
	return nil
}

// SetAlias adds or updates an alias mapping name -> ref.
func SetAlias(configDir, name, ref string) error {
	if name == "" {
		return fmt.Errorf("alias name must not be empty")
	}
	if ref == "" {
		return fmt.Errorf("alias ref must not be empty")
	}

	aliases, err := LoadAliases(configDir)
	if err != nil {
		return err
	}
	aliases[name] = ref
	return SaveAliases(configDir, aliases)
}

// RemoveAlias deletes an alias by name. Returns an error if the
// alias doesn't exist.
func RemoveAlias(configDir, name string) error {
	aliases, err := LoadAliases(configDir)
	if err != nil {
		return err
	}

	if _, ok := aliases[name]; !ok {
		return fmt.Errorf("alias %q not found", name)
	}

	delete(aliases, name)
	return SaveAliases(configDir, aliases)
}

// ListAliases returns all configured aliases.
func ListAliases(configDir string) (map[string]string, error) {
	return LoadAliases(configDir)
}
