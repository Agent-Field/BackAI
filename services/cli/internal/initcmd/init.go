// SPDX-License-Identifier: Apache-2.0

// Package initcmd implements the `af-stack init` fork-bootstrap command.
package initcmd

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

type brandFile struct {
	SchemaVersion    int            `yaml:"schema_version"`
	Name             string         `yaml:"name"`
	Codename         string         `yaml:"codename"`
	DisplayName      string         `yaml:"display_name"`
	ShortDescription string         `yaml:"short_description"`
	Palette          brandPalette   `yaml:"palette"`
	Logos            brandLogos     `yaml:"logos"`
	Domains          brandDomains   `yaml:"domains"`
	Surfaces         brandSurfaces  `yaml:"surfaces"`
	Extra            map[string]any `yaml:",inline,omitempty"`
}

type brandPalette struct {
	Primary  string         `yaml:"primary"`
	Accent   string         `yaml:"accent"`
	DarkMode bool           `yaml:"dark_mode"`
	Extra    map[string]any `yaml:",inline,omitempty"`
}

type brandLogos struct {
	Light   string         `yaml:"light"`
	Dark    string         `yaml:"dark"`
	Favicon string         `yaml:"favicon"`
	Extra   map[string]any `yaml:",inline,omitempty"`
}

type brandDomains struct {
	Dashboard   string         `yaml:"dashboard"`
	CustomerApp string         `yaml:"customer_app"`
	API         string         `yaml:"api"`
	Extra       map[string]any `yaml:",inline,omitempty"`
}

type brandSurfaces struct {
	Dashboard   brandSurface   `yaml:"dashboard"`
	CustomerApp brandSurface   `yaml:"customer_app"`
	Extra       map[string]any `yaml:",inline,omitempty"`
}

type brandSurface struct {
	DisplayName string         `yaml:"display_name"`
	Subtitle    string         `yaml:"subtitle"`
	Description string         `yaml:"description"`
	Extra       map[string]any `yaml:",inline,omitempty"`
}

var hexColorRE = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
var composeNodeIDRE = regexp.MustCompile(`(?m)^\s+NODE_ID:\s*([A-Za-z0-9_.-]+)\s*$`)

// Run handles `af-stack init`.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("af-stack init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	name := fs.String("name", "", "project display name, e.g. DocuChat")
	color := fs.String("color", "", "primary brand color as #RRGGBB")
	logo := fs.String("logo", "", "logo file to copy into both app public directories")
	template := fs.String("template", TemplateNode,
		"scaffold template: node (rebrand only) | coding-agent (rebrand + a real coding agent)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("init: unexpected argument %q", fs.Arg(0))
	}

	tmpl := strings.TrimSpace(strings.ToLower(*template))
	if tmpl == "" {
		tmpl = TemplateNode
	}
	if !validTemplate(tmpl) {
		return fmt.Errorf("init: unknown --template %q (want one of: %s)",
			*template, strings.Join(knownTemplates, ", "))
	}

	projectName := strings.TrimSpace(*name)
	if projectName == "" {
		prompted, err := prompt(stdin, stdout, "Project name")
		if err != nil {
			return err
		}
		projectName = strings.TrimSpace(prompted)
	}
	if projectName == "" {
		return errors.New("init: --name is required")
	}

	primaryColor := strings.TrimSpace(*color)
	if primaryColor == "" {
		primaryColor = "#111827"
	}
	if !hexColorRE.MatchString(primaryColor) {
		return fmt.Errorf("init: --color must be a 6-digit hex color, got %q", primaryColor)
	}
	primaryColor = strings.ToUpper(primaryColor)

	root, err := findRepoRoot()
	if err != nil {
		return err
	}

	brand, err := readBrand(filepath.Join(root, "brand.yaml"))
	if err != nil {
		return err
	}

	slug := slugify(projectName)
	brand.SchemaVersion = 1
	brand.Name = slug
	brand.Codename = slug
	brand.DisplayName = projectName
	brand.ShortDescription = fmt.Sprintf("%s backend.", projectName)
	brand.Palette.Primary = primaryColor
	if strings.TrimSpace(brand.Palette.Accent) == "" {
		brand.Palette.Accent = "#2563EB"
	}
	brand.Palette.Accent = strings.ToUpper(brand.Palette.Accent)
	brand.Palette.DarkMode = true
	brand.Domains.Dashboard = "admin.localhost"
	brand.Domains.CustomerApp = "app.localhost"
	brand.Domains.API = "api.localhost"
	brand.Surfaces.Dashboard.DisplayName = projectName
	brand.Surfaces.Dashboard.Subtitle = "Operator console"
	brand.Surfaces.Dashboard.Description = brand.ShortDescription
	brand.Surfaces.CustomerApp.DisplayName = projectName
	brand.Surfaces.CustomerApp.Subtitle = "Customer app"
	brand.Surfaces.CustomerApp.Description = fmt.Sprintf("Customer app for %s.", projectName)

	if strings.TrimSpace(*logo) != "" {
		logoPath, err := copyLogo(root, *logo)
		if err != nil {
			return err
		}
		brand.Logos.Light = logoPath
		brand.Logos.Dark = logoPath
	}
	if strings.TrimSpace(brand.Logos.Favicon) == "" {
		brand.Logos.Favicon = "./brand/favicon.ico"
	}

	if err := writeBrand(filepath.Join(root, "brand.yaml"), brand); err != nil {
		return err
	}
	if err := updateDefaultAgentName(root, slug); err != nil {
		return err
	}
	brandRegenerated := true
	if err := runBrandGenerator(root); err != nil {
		// Brand CSS/modules are also regenerated at build time, so a missing
		// pnpm / node_modules (e.g. a fresh clone that hasn't run `pnpm
		// install` yet) must NOT abort the scaffold: brand.yaml — the source
		// of truth — is already written above. Warn and carry on so the
		// hero flow (`init --template coding-agent`) still scaffolds.
		brandRegenerated = false
		fmt.Fprintf(stderr, "warning: skipped brand asset regeneration (%v)\n", err)
		fmt.Fprintf(stderr, "         run `pnpm install && pnpm run generate:brand` once Node deps are present\n")
	}

	var templateSummary string
	if tmpl == TemplateCodingAgent {
		created, skipped, err := scaffoldCodingAgent(root)
		if err != nil {
			return err
		}
		wired, err := wireCodingAgentCompose(root)
		if err != nil {
			return err
		}
		envAction, err := ensureHeroEnv(root)
		if err != nil {
			return err
		}
		templateSummary = summariseCodingAgentScaffold(created, skipped, wired, envAction)
	}

	fmt.Fprintf(stdout, "Initialized AF Stack fork for %s (template: %s)\n", projectName, tmpl)
	fmt.Fprintf(stdout, "- brand.yaml updated\n")
	if brandRegenerated {
		fmt.Fprintf(stdout, "- brand CSS/modules regenerated\n")
	} else {
		fmt.Fprintf(stdout, "- brand CSS/modules NOT regenerated (run `pnpm run generate:brand` after `pnpm install`)\n")
	}
	fmt.Fprintf(stdout, "- default agent node_id set to %s\n", slug)
	if templateSummary != "" {
		fmt.Fprint(stdout, templateSummary)
	}
	return nil
}

func prompt(stdin io.Reader, stdout io.Writer, label string) (string, error) {
	if stdin == nil {
		return "", nil
	}
	fmt.Fprintf(stdout, "%s: ", label)
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if errors.Is(err, io.EOF) {
		return strings.TrimSpace(line), nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func findRepoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if exists(filepath.Join(wd, "package.json")) &&
			exists(filepath.Join(wd, "apps", "dashboard")) &&
			exists(filepath.Join(wd, "apps", "customer-app")) {
			return wd, nil
		}
		next := filepath.Dir(wd)
		if next == wd {
			return "", errors.New("init: must run from inside an AF Stack checkout")
		}
		wd = next
	}
}

func readBrand(path string) (brandFile, error) {
	brand := brandFile{
		SchemaVersion:    1,
		Name:             "af-stack",
		Codename:         "af-stack",
		DisplayName:      "AF Stack",
		ShortDescription: "The open backend platform for the AI era.",
		Palette: brandPalette{
			Primary:  "#111827",
			Accent:   "#2563EB",
			DarkMode: true,
		},
		Logos: brandLogos{
			Light:   "./brand/logo-light.svg",
			Dark:    "./brand/logo-dark.svg",
			Favicon: "./brand/favicon.ico",
		},
		Domains: brandDomains{
			Dashboard:   "admin.localhost",
			CustomerApp: "app.localhost",
			API:         "api.localhost",
		},
	}
	if !exists(path) {
		return brand, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return brandFile{}, err
	}
	if err := yaml.Unmarshal(raw, &brand); err != nil {
		return brandFile{}, fmt.Errorf("init: read brand.yaml: %w", err)
	}
	return brand, nil
}

func writeBrand(path string, brand brandFile) error {
	raw, err := yaml.Marshal(brand)
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func copyLogo(root, rawPath string) (string, error) {
	source, err := filepath.Abs(rawPath)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(source)
	if err != nil {
		return "", fmt.Errorf("init: logo %q: %w", rawPath, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("init: logo %q is a directory", rawPath)
	}
	ext := strings.ToLower(filepath.Ext(source))
	if ext == "" {
		ext = ".logo"
	}
	destRel := filepath.ToSlash(filepath.Join("brand", "logo"+ext))
	dest := filepath.Join(root, filepath.FromSlash(destRel))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	raw, err := os.ReadFile(source)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(dest, raw, 0o644); err != nil {
		return "", err
	}
	return "./" + destRel, nil
}

func updateDefaultAgentName(root, next string) error {
	current := currentAgentName(root)
	replacements := map[string]string{
		"NODE_ID: " + current:                        "NODE_ID: " + next,
		"NODE_ID=" + current:                         "NODE_ID=" + next,
		`os.getenv("NODE_ID", "` + current + `")`:    `os.getenv("NODE_ID", "` + next + `")`,
		current + ".echo":                            next + ".echo",
		current + ".summarize":                       next + ".summarize",
		"Registers as `" + current + "`":             "Registers as `" + next + "`",
		"The runtime's " + current + "-agent points": "The runtime's " + next + "-agent points",
	}
	targets := []string{
		"docker-compose.yml",
		"apps/backend/agents/sample/Dockerfile",
		"apps/backend/agents/sample/main.py",
		"apps/backend/agents/sample/README.md",
		"apps/backend/litellm-config.yaml",
	}
	for _, rel := range targets {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if !exists(path) {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		updated := string(raw)
		for old, newValue := range replacements {
			updated = strings.ReplaceAll(updated, old, newValue)
		}
		if updated == string(raw) {
			continue
		}
		if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func currentAgentName(root string) string {
	raw, err := os.ReadFile(filepath.Join(root, "docker-compose.yml"))
	if err == nil {
		if match := composeNodeIDRE.FindSubmatch(raw); len(match) == 2 {
			return string(match[1])
		}
	}
	return "sample"
}

func slugify(input string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(input) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case unicode.IsSpace(r) || r == '-' || r == '_' || r == '.':
			if b.Len() > 0 && !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "app"
	}
	return out
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

var runBrandGenerator = func(root string) error {
	cmd := exec.Command("pnpm", "run", "generate:brand")
	cmd.Dir = root
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("init: generate brand: %w\n%s", err, output.String())
	}
	return nil
}
