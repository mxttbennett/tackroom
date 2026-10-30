package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func patchCodexMCPServer(target mcpTarget, server mcpServerConfig, home string) error {
	configPath := target.configPath(home)
	data, err := os.ReadFile(configPath)
	content := ""
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read %s: %w", configPath, err)
		}
	} else {
		content = string(data)
		entry, _, err := decodeCodexMCPEntry(configPath, data, server.Name)
		if err != nil && !errors.Is(err, errCodexEntryRepairable) {
			return err
		}
		server.Env = mergeManagedEnv(tomlEntryEnv(entry), server.Env)
		if server.URL != "" {
			server = keepNativeRemoteSecrets(server, entry, target.remote.codexHeaders)
		}
	}
	header := fmt.Sprintf("[mcp_servers.%s]", server.Name)
	section := renderCodexMCPSection(server)
	if server.URL != "" {
		if section, err = renderRemoteTOMLSection(server, target.remote.codexHeaders); err != nil {
			return err
		}
	}
	updated := upsertTOMLSectionIncludingDescendants(content, header, keepNativeCodexKeys(content, header, section))
	if updated == content {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(configPath), err)
	}
	if err := os.WriteFile(configPath, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", configPath, err)
	}
	return nil
}

// keepNativeCodexKeys carries over Codex keys tackroom does not render (env_vars, startup_timeout_sec, tools.*) so a rewrite cannot drop them.
func keepNativeCodexKeys(content string, header string, section string) string {
	start := indexTOMLSectionHeader(content, header)
	if start == -1 {
		return section
	}
	top, _ := extractTOMLSection(content, header)
	owned := parseTOMLBlockValues(section)
	isOwned := func(key string) bool {
		_, ok := owned[strings.SplitN(key, ".", 2)[0]]
		return ok
	}
	native := parseTOMLBlockValues(top)
	keys := make([]string, 0, len(native))
	for key := range native {
		if !isOwned(key) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var out strings.Builder
	out.WriteString(strings.TrimRight(section, "\n") + "\n")
	for _, key := range keys {
		fmt.Fprintf(&out, "%s = %s\n", key, native[key])
	}
	out.WriteString("\n")
	prefix := strings.TrimSuffix(header, "]") + "."
	descendants := content[start+len(top) : endTOMLSectionIncludingDescendants(content, start, header)]
	for strings.TrimSpace(descendants) != "" {
		end := len(descendants)
		if next := indexNextTOMLHeader(descendants[1:]); next != -1 {
			end = next + 1
		}
		chunk := descendants[:end]
		descendants = descendants[end:]
		name := strings.TrimPrefix(strings.TrimSpace(strings.SplitN(chunk, "\n", 2)[0]), prefix)
		if closing := strings.Index(name, "]"); closing != -1 {
			name = name[:closing]
		}
		if !isOwned(name) {
			out.WriteString(strings.TrimRight(chunk, "\n") + "\n\n")
		}
	}
	return out.String()
}
