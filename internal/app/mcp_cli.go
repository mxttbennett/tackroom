package app

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
)

type stringListFlag []string

func (f *stringListFlag) String() string { return strings.Join(*f, ",") }
func (f *stringListFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func runMCP(args []string) error {
	if len(args) == 0 {
		return errors.New("mcp requires subcommand: list, add, import, remove")
	}
	if isHelpArg(args[0]) {
		printGroupUsage("mcp")
		return nil
	}
	switch args[0] {
	case "list":
		return runMCPList(args[1:])
	case "add":
		return runMCPAdd(args[1:])
	case "import":
		return runMCPImport(args[1:])
	case "remove":
		return runMCPRemove(args[1:])
	default:
		return fmt.Errorf("unknown mcp subcommand %q", args[0])
	}
}

func runMCPList(args []string) error {
	fs := flag.NewFlagSet("mcp list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configPath := fs.String("config", "", "Path to tackroom YAML config")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("mcp list does not accept positional arguments")
	}
	cfg, _, err := loadEditableMCPConfig(*configPath)
	if err != nil {
		return err
	}
	if len(cfg.MCPServers) == 0 {
		fmt.Println("managed MCP servers: -")
		return nil
	}
	fmt.Println("managed MCP servers:")
	for _, server := range cfg.MCPServers {
		status := "disabled"
		if server.Enabled {
			status = "enabled"
		}
		fmt.Printf("- %s (%s)\n", server.Name, status)
		if server.URL != "" {
			fmt.Printf("  url: %s\n", server.URL)
		} else {
			fmt.Printf("  command: %s\n", server.Command)
		}
		if len(server.Args) > 0 {
			fmt.Printf("  args: %s\n", strings.Join(server.Args, " "))
		}
		fmt.Printf("  agents: %s\n", displayList(server.Agents))
		if len(server.Env) > 0 {
			keys := make([]string, 0, len(server.Env))
			for key := range server.Env {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			fmt.Printf("  env: %s\n", strings.Join(keys, ", "))
		}
		if len(server.Headers) > 0 {
			keys := make([]string, 0, len(server.Headers))
			for key := range server.Headers {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			fmt.Printf("  headers: %s\n", strings.Join(keys, ", "))
		}
	}
	return nil
}

func runMCPAdd(args []string) error {
	fs := flag.NewFlagSet("mcp add", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configPath := fs.String("config", "", "Path to tackroom YAML config")
	command := fs.String("command", "", "MCP stdio command")
	url := fs.String("url", "", "Remote (streamable HTTP) MCP server URL, instead of --command")
	agentsCSV := fs.String("agents", "", "Comma-separated target agents")
	var mcpArgs stringListFlag
	var envs stringListFlag
	var headerFlags stringListFlag
	fs.Var(&mcpArgs, "arg", "MCP command argument (repeatable)")
	fs.Var(&envs, "env", "MCP environment KEY=VALUE (repeatable)")
	fs.Var(&headerFlags, "header", "Remote server header 'Name: value' (repeatable); use ${VAR} for secrets")
	name := ""
	parseArgs := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name = args[0]
		parseArgs = args[1:]
	}
	if err := fs.Parse(parseArgs); err != nil {
		return err
	}
	if name == "" && fs.NArg() == 1 {
		name = fs.Arg(0)
	} else if fs.NArg() != 0 {
		return errors.New("usage: tackroom mcp add <name> (--command <cmd> [--arg value ...] [--env KEY=VALUE ...] | --url <url> [--header 'Name: value' ...])")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("MCP name cannot be empty")
	}
	if (strings.TrimSpace(*command) == "") == (strings.TrimSpace(*url) == "") {
		return errors.New("pass either --command (stdio server) or --url (remote server)")
	}
	cfg, path, err := loadEditableMCPConfig(*configPath)
	if err != nil {
		return err
	}
	targetAgents, err := resolveMCPAgents(cfg, *agentsCSV)
	if err != nil {
		return err
	}
	env, err := parseMCPEnvFlags(envs)
	if err != nil {
		return err
	}
	headers, err := parseMCPHeaderFlags(headerFlags)
	if err != nil {
		return err
	}
	server := mcpServerConfig{Name: name, Enabled: true, Command: strings.TrimSpace(*command), Args: []string(mcpArgs), Env: env, URL: strings.TrimSpace(*url), Headers: headers, Agents: targetAgents}
	if err := upsertCanonicalMCPServer(&cfg, server); err != nil {
		return err
	}
	if err := writeEditableMCPConfig(path, cfg); err != nil {
		return err
	}
	fmt.Printf("added MCP server %s for %s\n", name, strings.Join(targetAgents, ", "))
	if server.URL != "" {
		var skipped []string
		for _, agent := range targetAgents {
			if !hasRemoteMCPSupport(agent) {
				skipped = append(skipped, agent)
			}
		}
		if len(skipped) > 0 {
			fmt.Fprintf(os.Stderr, "note: remote servers are not written for %s yet; open an issue if you need one\n", strings.Join(skipped, ", "))
		}
	}
	var literal []string
	for key, value := range env {
		if !isEnvReference(value) {
			literal = append(literal, key)
		}
	}
	for key, value := range headers {
		if !hasEnvReference(value) && strings.EqualFold(key, "Authorization") {
			literal = append(literal, "header "+key)
		}
	}
	if len(literal) > 0 {
		sort.Strings(literal)
		fmt.Fprintf(os.Stderr, "warning: %s stores %s in plain text; keep secrets out of a config repo you push\n", path, strings.Join(literal, ", "))
	}
	fmt.Println("next: tackroom sync")
	return nil
}

func runMCPImport(args []string) error {
	fs := flag.NewFlagSet("mcp import", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configPath := fs.String("config", "", "Path to tackroom YAML config")
	agentsCSV := fs.String("agents", "", "Comma-separated target agents")
	var importAgent, name string
	parseArgs := args
	if len(args) >= 2 && !strings.HasPrefix(args[0], "-") && !strings.HasPrefix(args[1], "-") {
		importAgent = args[0]
		name = args[1]
		parseArgs = args[2:]
	}
	if err := fs.Parse(parseArgs); err != nil {
		return err
	}
	if importAgent == "" && fs.NArg() == 2 {
		importAgent = fs.Arg(0)
		name = fs.Arg(1)
	} else if fs.NArg() != 0 {
		return errors.New("usage: tackroom mcp import <agent> <name> [--agents a,b,c]")
	}
	importAgent = normalizeAgentName(importAgent)
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("MCP name cannot be empty")
	}
	cfg, path, err := loadEditableMCPConfig(*configPath)
	if err != nil {
		return err
	}
	if err := validateConfiguredMCPAgent(cfg, importAgent); err != nil {
		return err
	}
	targetAgents, err := resolveMCPAgents(cfg, *agentsCSV)
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home: %w", err)
	}
	server, err := readNativeMCPServer(importAgent, name, home)
	if err != nil {
		return err
	}
	server.Enabled = true
	var redacted []string
	for key, value := range server.Env {
		if !isEnvReference(value) {
			redacted = append(redacted, key)
		}
	}
	server.Headers, redacted = redactImportedMCPHeaders(server.Name, server.Headers, redacted)
	sort.Strings(redacted)
	server.Env = redactImportedMCPEnv(server.Env)
	server.Agents = targetAgents
	if err := upsertCanonicalMCPServer(&cfg, server); err != nil {
		return err
	}
	if err := writeEditableMCPConfig(path, cfg); err != nil {
		return err
	}
	fmt.Printf("imported MCP server %s from %s for %s\n", name, importAgent, strings.Join(targetAgents, ", "))
	if len(redacted) > 0 {
		var others []string
		for _, agent := range targetAgents {
			if agent != importAgent {
				others = append(others, agent)
			}
		}
		fmt.Fprintf(os.Stderr, "warning: %s stores ${KEY} references for %s, not the values; %s keeps its own values\n", path, strings.Join(redacted, ", "), importAgent)
		if len(others) > 0 {
			fmt.Fprintf(os.Stderr, "  sync writes the ${KEY} references into %s: set those variables where the agents start, or put the real values in their configs (sync keeps them), or limit targets with --agents %s\n", strings.Join(others, ", "), importAgent)
		}
	}
	fmt.Println("next: tackroom sync")
	return nil
}

func runMCPRemove(args []string) error {
	fs := flag.NewFlagSet("mcp remove", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configPath := fs.String("config", "", "Path to tackroom YAML config")
	name := ""
	parseArgs := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name = args[0]
		parseArgs = args[1:]
	}
	if err := fs.Parse(parseArgs); err != nil {
		return err
	}
	if name == "" && fs.NArg() == 1 {
		name = fs.Arg(0)
	} else if fs.NArg() != 0 {
		return errors.New("usage: tackroom mcp remove <name>")
	}
	name = strings.TrimSpace(name)
	cfg, path, err := loadEditableMCPConfig(*configPath)
	if err != nil {
		return err
	}
	removed := false
	// A fresh slice: cfg.MCPServers is still read by the claim below.
	servers := make([]mcpServerConfig, 0, len(cfg.MCPServers))
	for _, server := range cfg.MCPServers {
		if server.Name == name {
			removed = true
			continue
		}
		servers = append(servers, server)
	}
	if !removed {
		return fmt.Errorf("MCP server %q not found", name)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home: %w", err)
	}
	// Claim the native copies that still match this definition (for example
	// written by a release that did not track ownership), so the next sync
	// removes them too.
	if err := claimMatchingMCPServer(cfg, name, home); err != nil {
		return err
	}
	cfg.MCPServers = servers
	if err := writeEditableMCPConfig(path, cfg); err != nil {
		return err
	}
	fmt.Printf("removed MCP server %s from %s\n", name, path)
	fmt.Println("next: tackroom sync (removes it from agents where tackroom wrote it or it still matches; their configs are backed up first)")
	return nil
}

func loadEditableMCPConfig(overridePath string) (config, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return config{}, "", fmt.Errorf("resolve home: %w", err)
	}
	path, err := resolveConfigPath(overridePath, home)
	if err != nil {
		return config{}, "", err
	}
	doc, err := newConfigDocument(path, home)
	if err != nil {
		return config{}, "", err
	}
	return doc.shared, doc.sharedPath, nil
}

func writeEditableMCPConfig(path string, cfg config) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home: %w", err)
	}
	return saveConfigDocument(path, home, cfg)
}

func resolveMCPAgents(cfg config, agentsCSV string) ([]string, error) {
	if strings.TrimSpace(agentsCSV) != "" {
		var agents []string
		seen := make(map[string]struct{})
		for _, part := range strings.Split(agentsCSV, ",") {
			name := normalizeAgentName(part)
			if name == "" {
				continue
			}
			if err := validateConfiguredMCPAgent(cfg, name); err != nil {
				return nil, err
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			agents = append(agents, name)
		}
		if len(agents) == 0 {
			return nil, errors.New("--agents did not resolve to any agents")
		}
		return agents, nil
	}
	var agents []string
	for _, agent := range cfg.Agents {
		name := normalizeAgentName(agent.Name)
		if hasMCPSupport(name) {
			agents = append(agents, name)
		}
	}
	if len(agents) == 0 {
		return nil, errors.New("config has no agents with MCP support")
	}
	return agents, nil
}

func validateConfiguredMCPAgent(cfg config, name string) error {
	found := false
	for _, agent := range cfg.Agents {
		if normalizeAgentName(agent.Name) == name {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("unknown agent %q", name)
	}
	if !hasMCPSupport(name) {
		return fmt.Errorf("unsupported MCP target agent %q", name)
	}
	return nil
}

func parseMCPHeaderFlags(flags []string) (map[string]string, error) {
	if len(flags) == 0 {
		return nil, nil
	}
	headers := make(map[string]string, len(flags))
	for _, item := range flags {
		name, value, ok := strings.Cut(item, ":")
		if !ok || strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("invalid --header %q, expected 'Name: value'", item)
		}
		headers[strings.TrimSpace(name)] = strings.TrimSpace(value)
	}
	return headers, nil
}

func parseMCPEnvFlags(flags []string) (map[string]string, error) {
	if len(flags) == 0 {
		return nil, nil
	}
	env := make(map[string]string, len(flags))
	for _, item := range flags {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
			return nil, fmt.Errorf("invalid --env %q, expected KEY=VALUE", item)
		}
		env[strings.TrimSpace(parts[0])] = parts[1]
	}
	return env, nil
}

// redactImportedMCPHeaders replaces secret-looking header values with ${VAR}
// references (Authorization keeps its scheme) and turns Cursor's ${env:VAR}
// into ${VAR}. It appends the redacted header names to redacted.
func redactImportedMCPHeaders(server string, headers map[string]string, redacted []string) (map[string]string, []string) {
	if len(headers) == 0 {
		return headers, redacted
	}
	out := make(map[string]string, len(headers))
	for key, value := range headers {
		value = strings.ReplaceAll(value, "${env:", "${")
		lower := strings.ToLower(key)
		sensitive := lower == "authorization" || strings.Contains(lower, "token") || strings.Contains(lower, "key") || strings.Contains(lower, "secret") || strings.Contains(lower, "cookie")
		if !sensitive || hasEnvReference(value) {
			out[key] = value
			continue
		}
		envName := strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(server + "_" + key))
		if scheme, _, ok := strings.Cut(value, " "); ok && lower == "authorization" {
			out[key] = scheme + " ${" + envName + "}"
		} else {
			out[key] = "${" + envName + "}"
		}
		redacted = append(redacted, "header "+key)
	}
	return out, redacted
}

func redactImportedMCPEnv(env map[string]string) map[string]string {
	if len(env) == 0 {
		return env
	}
	redacted := make(map[string]string, len(env))
	for key, value := range env {
		if isEnvReference(value) {
			redacted[key] = value
			continue
		}
		redacted[key] = "${" + key + "}"
	}
	return redacted
}

func isEnvReference(value string) bool {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "${") || !strings.HasSuffix(trimmed, "}") {
		return false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(trimmed, "${"), "}")
	if name == "" {
		return false
	}
	for i, r := range name {
		if r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func upsertCanonicalMCPServer(cfg *config, server mcpServerConfig) error {
	if strings.TrimSpace(server.Name) == "" {
		return errors.New("MCP server name cannot be empty")
	}
	if err := validateMCPTransport(server); err != nil {
		return err
	}
	for i := range cfg.MCPServers {
		if cfg.MCPServers[i].Name == server.Name {
			cfg.MCPServers[i] = server
			return nil
		}
	}
	cfg.MCPServers = append(cfg.MCPServers, server)
	return nil
}
