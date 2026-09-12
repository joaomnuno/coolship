package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/joaomnuno/coolship/internal/models"
)

// Build packs Coolify accepts for an application created from a repository.
// Railpack is Coolify's own default for a new application; Nixpacks is its
// predecessor and still supported.
const (
	BuildPackRailpack   = "railpack"
	BuildPackNixpacks   = "nixpacks"
	BuildPackStatic     = "static"
	BuildPackDockerfile = "dockerfile"
	BuildPackCompose    = "dockercompose"
)

// buildPacks lists the packs init can create, in the order help shows them.
var buildPacks = []string{BuildPackRailpack, BuildPackNixpacks, BuildPackStatic, BuildPackDockerfile, BuildPackCompose}

// composeFiles are the names Coolify itself looks for, in its order.
var composeFiles = []string{"docker-compose.yaml", "docker-compose.yml", "compose.yaml", "compose.yml"}

// defaultPublishDirectory is where Coolify's UI expects a static build's
// output when Railpack or Nixpacks builds it.
const defaultPublishDirectory = "/dist"

// BuildOptions are the build pack and everything that refines it. Which
// fields apply depends on the pack: Dockerfile names the file for
// dockerfile, ComposeFile and ComposeDomains apply to dockercompose,
// Static and PublishDirectory turn a Railpack or Nixpacks build into a
// static site (PublishDirectory also applies to the static pack), and the
// three commands override what Railpack or Nixpacks detect. Paths are
// relative to the application root.
type BuildOptions struct {
	BuildPack        string
	Port             int // 0 means the build pack's default
	Static           bool
	PublishDirectory string
	Dockerfile       string
	ComposeFile      string
	ComposeDomains   []ComposeDomain
	InstallCommand   string
	BuildCommand     string
	StartCommand     string
}

// ComposeDomain gives one service of a Compose application its domain.
type ComposeDomain struct {
	Service string `json:"service"`
	Domain  string `json:"domain"`
}

// buildPlan is the settled build configuration: the pack, the port Coolify
// routes to (0 for Compose, where the file decides), and the refinements
// that apply to that pack, with Coolify's defaults filled in where the plan
// should show them.
type buildPlan struct {
	BuildPack        string
	Port             int
	Static           bool
	PublishDirectory string
	Dockerfile       string
	ComposeFile      string
	ComposeDomains   []ComposeDomain
	InstallCommand   string
	BuildCommand     string
	StartCommand     string
}

// validateBuildOptions checks what the flags say before the root is known:
// the pack name, the port, and that every refinement names the pack it
// belongs to when the pack is explicit. Refinements tied to a pack the
// detection may still pick are checked again by settleBuild.
func validateBuildOptions(options BuildOptions) error {
	if options.BuildPack != "" && !slices.Contains(buildPacks, options.BuildPack) {
		return input(fmt.Errorf("--build-pack must be one of %s, not %q", strings.Join(buildPacks, ", "), options.BuildPack))
	}
	if options.Port < 0 || options.Port > 65535 {
		return input(fmt.Errorf("--port must be between 1 and 65535, not %d", options.Port))
	}
	for _, path := range []struct{ flag, value string }{{"--dockerfile", options.Dockerfile}, {"--compose-file", options.ComposeFile}, {"--publish-dir", options.PublishDirectory}} {
		if err := validateRootPath(path.flag, path.value); err != nil {
			return err
		}
	}
	for _, domain := range options.ComposeDomains {
		if domain.Service == "" || domain.Domain == "" {
			return input(errors.New("--compose-domain takes SERVICE=URL"))
		}
		if !strings.HasPrefix(domain.Domain, "http://") && !strings.HasPrefix(domain.Domain, "https://") {
			return input(fmt.Errorf("--compose-domain %s: the domain must start with http:// or https://", domain.Service))
		}
	}
	return nil
}

// validateRootPath accepts a path inside the application root: relative,
// with or without a leading slash, and never escaping it.
func validateRootPath(flag, value string) error {
	if value == "" {
		return nil
	}
	clean := filepath.ToSlash(filepath.Clean("/" + value))
	if clean == "/" || slices.Contains(strings.Split(filepath.ToSlash(value), "/"), "..") {
		return input(fmt.Errorf("%s must name a file or directory inside the application root, not %q", flag, value))
	}
	return nil
}

// rootPath normalizes a path the way Coolify stores it: with a leading
// slash, relative to the base directory.
func rootPath(value string) string {
	return filepath.ToSlash(filepath.Clean("/" + value))
}

// detectBuildPack reads the application root the way Coolify's own creation
// form would: a compose file makes a Compose application, a Dockerfile
// builds itself, a page with no package manifest is served as it is, and
// anything else is handed to Railpack, Coolify's default. The compose file
// found is returned so the application points at the right one.
func detectBuildPack(dir string) (pack, composeFile string) {
	for _, name := range composeFiles {
		if isFile(filepath.Join(dir, name)) {
			return BuildPackCompose, "/" + name
		}
	}
	if isFile(filepath.Join(dir, "Dockerfile")) {
		return BuildPackDockerfile, ""
	}
	if isFile(filepath.Join(dir, "index.html")) && !isFile(filepath.Join(dir, "package.json")) {
		return BuildPackStatic, ""
	}
	return BuildPackRailpack, ""
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// settleBuild combines the flags with what the root contains and refuses a
// refinement that does not belong to the pack, so the plan never carries a
// setting Coolify would ignore. An explicit --dockerfile or --compose-file
// (or --compose-domain) picks its pack when --build-pack is not given, so
// naming the file is enough; --build-pack still wins, and still conflicts
// loudly with a refinement that names the wrong pack.
func settleBuild(options BuildOptions, appRoot string) (buildPlan, error) {
	plan := buildPlan{BuildPack: options.BuildPack}
	detected, composeFile := detectBuildPack(appRoot)
	if plan.BuildPack == "" {
		switch {
		case options.Dockerfile != "":
			plan.BuildPack = BuildPackDockerfile
		case options.ComposeFile != "" || len(options.ComposeDomains) > 0:
			plan.BuildPack = BuildPackCompose
		default:
			plan.BuildPack = detected
		}
	}
	builds := plan.BuildPack == BuildPackRailpack || plan.BuildPack == BuildPackNixpacks
	if options.Static && !builds {
		if plan.BuildPack == BuildPackStatic {
			return buildPlan{}, input(errors.New("--static is implied by --build-pack static; pass one of them"))
		}
		return buildPlan{}, input(fmt.Errorf("--static applies to railpack and nixpacks, not %s", plan.BuildPack))
	}
	if options.PublishDirectory != "" && !options.Static && plan.BuildPack != BuildPackStatic {
		return buildPlan{}, input(fmt.Errorf("--publish-dir applies to --static and --build-pack static, not %s", plan.BuildPack))
	}
	if (options.InstallCommand != "" || options.BuildCommand != "" || options.StartCommand != "") && !builds {
		return buildPlan{}, input(fmt.Errorf("--install-command, --build-command, and --start-command apply to railpack and nixpacks, not %s", plan.BuildPack))
	}
	if options.Dockerfile != "" && plan.BuildPack != BuildPackDockerfile {
		return buildPlan{}, input(fmt.Errorf("--dockerfile applies to --build-pack dockerfile, not %s", plan.BuildPack))
	}
	if (options.ComposeFile != "" || len(options.ComposeDomains) > 0) && plan.BuildPack != BuildPackCompose {
		return buildPlan{}, input(fmt.Errorf("--compose-file and --compose-domain apply to --build-pack dockercompose, not %s", plan.BuildPack))
	}
	if options.Port != 0 && plan.BuildPack == BuildPackCompose {
		return buildPlan{}, input(errors.New("--port does not apply to a Compose application; each service's ports come from the compose file"))
	}

	switch plan.BuildPack {
	case BuildPackRailpack, BuildPackNixpacks:
		plan.Static = options.Static
		plan.InstallCommand, plan.BuildCommand, plan.StartCommand = options.InstallCommand, options.BuildCommand, options.StartCommand
		if plan.Static {
			plan.PublishDirectory = defaultPublishDirectory
			if options.PublishDirectory != "" {
				plan.PublishDirectory = rootPath(options.PublishDirectory)
			}
		}
	case BuildPackStatic:
		if options.PublishDirectory != "" {
			plan.PublishDirectory = rootPath(options.PublishDirectory)
		}
	case BuildPackDockerfile:
		plan.Dockerfile = "/Dockerfile"
		if options.Dockerfile != "" {
			plan.Dockerfile = rootPath(options.Dockerfile)
			if !isFile(filepath.Join(appRoot, plan.Dockerfile)) {
				return buildPlan{}, input(fmt.Errorf("--dockerfile %s: no such file in %s", options.Dockerfile, appRoot))
			}
		}
	case BuildPackCompose:
		plan.ComposeFile = composeFile
		if options.ComposeFile != "" {
			plan.ComposeFile = rootPath(options.ComposeFile)
			if !isFile(filepath.Join(appRoot, plan.ComposeFile)) {
				return buildPlan{}, input(fmt.Errorf("--compose-file %s: no such file in %s", options.ComposeFile, appRoot))
			}
		} else if plan.ComposeFile == "" {
			return buildPlan{}, input(fmt.Errorf("no compose file in %s; pass --compose-file PATH", appRoot))
		}
		plan.ComposeDomains = options.ComposeDomains
	}
	plan.Port = options.Port
	if plan.Port == 0 && plan.BuildPack != BuildPackCompose {
		plan.Port = defaultPort(plan.BuildPack, plan.Static)
	}
	return plan, nil
}

// defaultPort is what each build pack's typical result listens on: 3000 for
// a Railpack or Nixpacks build, 80 for anything nginx serves.
func defaultPort(buildPack string, static bool) int {
	if (buildPack == BuildPackRailpack || buildPack == BuildPackNixpacks) && !static {
		return 3000
	}
	return 80
}

// apply writes the settled build into the creation request. A Compose
// application is sent port 80 because the endpoint requires a port and then
// ignores it. Coolify's own health check is switched off for a Dockerfile
// application, as Coolify's UI does, because the check it would generate
// needs curl or wget inside the image; a HEALTHCHECK in the Dockerfile is
// still honored.
func (b buildPlan) apply(spec *models.ApplicationSpec) {
	spec.BuildPack = b.BuildPack
	spec.PortsExposes = fmt.Sprint(b.Port)
	spec.IsStatic = b.Static
	spec.PublishDirectory = b.PublishDirectory
	spec.InstallCommand, spec.BuildCommand, spec.StartCommand = b.InstallCommand, b.BuildCommand, b.StartCommand
	switch b.BuildPack {
	case BuildPackDockerfile:
		if b.Dockerfile != "/Dockerfile" {
			spec.DockerfileLocation = b.Dockerfile
		}
		disabled := false
		spec.HealthCheckEnabled = &disabled
	case BuildPackCompose:
		spec.PortsExposes = "80"
		spec.DockerComposeLocation = b.ComposeFile
		for _, domain := range b.ComposeDomains {
			spec.DockerComposeDomains = append(spec.DockerComposeDomains, models.ComposeDomain{Name: domain.Service, Domain: domain.Domain})
		}
	}
}

// ParseComposeDomain reads one --compose-domain value, SERVICE=URL.
func ParseComposeDomain(value string) (ComposeDomain, error) {
	service, domain, ok := strings.Cut(value, "=")
	service, domain = strings.TrimSpace(service), strings.TrimSpace(domain)
	if !ok || service == "" || domain == "" {
		return ComposeDomain{}, fmt.Errorf("--compose-domain takes SERVICE=URL, not %q", value)
	}
	return ComposeDomain{Service: service, Domain: domain}, nil
}
