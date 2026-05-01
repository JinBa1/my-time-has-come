package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func mergeClaudeSettings(home string) (map[string]any, map[string]string, []settingsError) {
	merged := make(map[string]any)
	scope := make(map[string]string)
	var found bool
	var errs []settingsError

	userPath := userClaudeSettingsPath(home)
	out := loadSettingsLayerChecked(merged, scope, userPath, "user")
	if out.err != nil {
		errs = append(errs, *out.err)
	}
	if out.found {
		found = true
	}

	walkSettingsPathChecked(merged, scope, ".claude/settings.json", "project", home, &found, &errs)
	walkSettingsPathChecked(merged, scope, ".claude/settings.local.json", "local", home, &found, &errs)

	out = loadSettingsLayerChecked(merged, scope, managedSettingsPath, "managed")
	if out.err != nil {
		errs = append(errs, *out.err)
	}
	if out.found {
		found = true
	}

	if !found && len(errs) == 0 {
		return nil, nil, nil
	}
	return merged, scope, errs
}

func userClaudeSettingsPath(home string) string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "settings.json")
	}
	return filepath.Join(home, ".claude", "settings.json")
}

type settingsLoadOutcome struct {
	found bool
	err   *settingsError
}

func loadSettingsLayerChecked(merged map[string]any, scope map[string]string, path, layerName string) settingsLoadOutcome {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return settingsLoadOutcome{}
		}
		return settingsLoadOutcome{err: &settingsError{path: path, scope: layerName, err: err}}
	}
	layer := make(map[string]any)
	if err := json.Unmarshal(data, &layer); err != nil {
		return settingsLoadOutcome{err: &settingsError{path: path, scope: layerName, err: err}}
	}
	for k, v := range layer {
		if k == "hooks" {
			mergeHooksSetting(merged, v)
			scope[k] = layerName
			continue
		}
		merged[k] = v
		scope[k] = layerName
	}
	return settingsLoadOutcome{found: true}
}

func mergeHooksSetting(merged map[string]any, value any) {
	incomingHooks, ok := value.(map[string]any)
	if !ok {
		merged["hooks"] = value
		return
	}

	existingHooks, ok := merged["hooks"].(map[string]any)
	if !ok {
		existingHooks = make(map[string]any)
		merged["hooks"] = existingHooks
	}

	for eventName, incomingValue := range incomingHooks {
		incomingGroups, incomingOK := incomingValue.([]any)
		existingGroups, existingOK := existingHooks[eventName].([]any)
		if incomingOK && existingOK {
			combined := append(append([]any{}, existingGroups...), incomingGroups...)
			existingHooks[eventName] = combined
			continue
		}
		existingHooks[eventName] = incomingValue
	}
}

func sameFile(a, b string) bool {
	aInfo, errA := os.Stat(a)
	bInfo, errB := os.Stat(b)
	if errA != nil || errB != nil {
		return false
	}
	return os.SameFile(aInfo, bInfo)
}

func walkSettingsPathChecked(merged map[string]any, scope map[string]string, relPath, layerName, home string, found *bool, errs *[]settingsError) {
	dir, _ := os.Getwd()
	homeResolved, _ := filepath.EvalSymlinks(home)
	userPath := userClaudeSettingsPath(home)

	for {
		candidate := filepath.Join(dir, relPath)
		if !sameFile(candidate, userPath) {
			out := loadSettingsLayerChecked(merged, scope, candidate, layerName)
			if out.err != nil {
				*errs = append(*errs, *out.err)
			}
			if out.found {
				*found = true
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dirResolved, _ := filepath.EvalSymlinks(dir)
		if dirResolved == homeResolved {
			break
		}
		dir = parent
	}
}
