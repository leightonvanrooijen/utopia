package cli

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/spf13/cobra"
)

// shellManagedEnvVars are the names /bin/sh sets or rewrites for itself when the
// fake claude runs. They say nothing about the environment utopia handed over,
// so an "unchanged environment" assertion ignores them rather than pretending
// the shell is transparent.
var shellManagedEnvVars = map[string]bool{
	"PWD": true, "OLDPWD": true, "SHLVL": true, "_": true,
}

// authProject writes a project whose .utopia holds the given config.yaml and
// .env, skipping either file when its content is empty, and returns the project
// directory. An absent config.yaml is the pre-auth project: nothing to migrate.
func authProject(t *testing.T, config, envFile string) string {
	t.Helper()

	projectDir := t.TempDir()
	utopiaDir := filepath.Join(projectDir, ".utopia")
	if err := os.MkdirAll(utopiaDir, 0o755); err != nil {
		t.Fatalf("failed to create utopia dir: %v", err)
	}
	for name, content := range map[string]string{"config.yaml": config, ".env": envFile} {
		if content == "" {
			continue
		}
		if err := os.WriteFile(filepath.Join(utopiaDir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}
	return projectDir
}

// fakeClaudeOnPath installs a stand-in claude ahead of the real one on PATH and
// returns a reader for the environment it was spawned with.
//
// Putting it on PATH rather than injecting it is what makes these tests
// end-to-end: the handler builds its own *internal.CLI, so there is no seam to
// substitute, and only a real spawn can show whether the mode a handler resolved
// actually reached a subprocess. A handler that resolves the mode and drops it
// still passes every in-process assertion.
func fakeClaudeOnPath(t *testing.T) func() []string {
	t.Helper()

	dir := t.TempDir()
	dumpPath := filepath.Join(dir, "env.dump")

	script := "#!/bin/sh\nenv > " + dumpPath + "\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake claude: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	return func() []string {
		t.Helper()

		data, err := os.ReadFile(dumpPath)
		if err != nil {
			t.Fatalf("fake claude was never spawned (no environment recorded): %v", err)
		}
		return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	}
}

// runStandardsGenerateIn drives the standards generate handler against a project
// directory, with the flags it reads registered exactly as the real command
// registers them. It returns what the command wrote to stderr.
//
// standards generate stands in for the whole family: it is the shortest path
// from a resolved auth mode to a spawned claude, and the mode travels the same
// ResolveAuth -> WithAuth route in every handler. Whether each command
// registers --auth at all is covered by
// TestAuthFlagRegisteredOnClaudeInvokingCommands, and whether each one keeps the
// mode it resolves is covered by TestEveryAuthHandlerConsumesResolvedMode.
func runStandardsGenerateIn(t *testing.T, projectDir, authFlag string) string {
	t.Helper()

	cmd := &cobra.Command{Use: "generate", RunE: runStandardsGenerate}
	cmd.Flags().String("model", "", "model to use")
	cmd.Flags().String("auth", "", "credential to use")
	cmd.Flags().StringP("project", "p", projectDir, "project directory")
	if authFlag != "" {
		if err := cmd.Flags().Set("auth", authFlag); err != nil {
			t.Fatalf("Set(auth, %q) = %v", authFlag, err)
		}
	}

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("standards generate failed: %v", err)
	}
	return stderr.String()
}

// envMap reduces a "NAME=value" slice to a map, dropping the names the shell
// manages for itself.
func envMap(entries []string) map[string]string {
	vars := make(map[string]string, len(entries))
	for _, entry := range entries {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || shellManagedEnvVars[name] {
			continue
		}
		vars[name] = value
	}
	return vars
}

// The gap this guards against: config.models.* is validated at load time and
// never read, so only --model ever reaches a command. An auth mode that resolved
// correctly, reported correctly, and then failed to reach the subprocess would
// be worse than that - it prints the account that pays while billing the other
// one. Only the spawned process can tell the two apart.
func TestConfiguredAuthModeReachesSubprocess(t *testing.T) {
	t.Run("subscription in config suppresses the ambient key with no flag present", func(t *testing.T) {
		t.Setenv(domain.APIKeyEnvVar, "sk-ant-ambient")
		t.Setenv(domain.AuthTokenEnvVar, "token-ambient")

		recorded := fakeClaudeOnPath(t)
		projectDir := authProject(t, "auth:\n  mode: subscription\n", "")

		runStandardsGenerateIn(t, projectDir, "")

		subprocess := envMap(recorded())
		for _, name := range []string{domain.APIKeyEnvVar, domain.AuthTokenEnvVar} {
			if value, present := subprocess[name]; present {
				t.Errorf("%s=%q reached the subprocess: config auth.mode was validated but not consumed", name, value)
			}
		}
	})

	t.Run("api-key in config injects the project key with no flag present", func(t *testing.T) {
		t.Setenv(domain.APIKeyEnvVar, "sk-ant-ambient")
		t.Setenv(domain.AuthTokenEnvVar, "")

		recorded := fakeClaudeOnPath(t)
		projectDir := authProject(t, "auth:\n  mode: api-key\n", "ANTHROPIC_API_KEY=sk-ant-file\n")

		runStandardsGenerateIn(t, projectDir, "")

		if got := envMap(recorded())[domain.APIKeyEnvVar]; got != "sk-ant-file" {
			t.Errorf("subprocess %s = %q, want the key from .utopia/.env", domain.APIKeyEnvVar, got)
		}
	})

	// The flag has to win at the subprocess, not only in the reported line.
	t.Run("the flag overrides the configured mode at the subprocess", func(t *testing.T) {
		t.Setenv(domain.APIKeyEnvVar, "sk-ant-ambient")
		t.Setenv(domain.AuthTokenEnvVar, "")

		recorded := fakeClaudeOnPath(t)
		projectDir := authProject(t, "auth:\n  mode: api-key\n", "ANTHROPIC_API_KEY=sk-ant-file\n")

		runStandardsGenerateIn(t, projectDir, "subscription")

		if value, present := envMap(recorded())[domain.APIKeyEnvVar]; present {
			t.Errorf("subprocess %s = %q, want it absent under --auth subscription", domain.APIKeyEnvVar, value)
		}
	})
}

// Backward compatibility, at the only place it can be observed: a project that
// never configured a credential must hand its claude subprocess the environment
// utopia itself is running with, variable for variable.
func TestNoAuthSelectionInheritsSubprocessEnvironment(t *testing.T) {
	configs := map[string]string{
		"no config.yaml at all":              "",
		"a config.yaml with no auth section": "verification:\n  max_iterations: 3\n",
	}

	for name, config := range configs {
		t.Run(name, func(t *testing.T) {
			t.Setenv(domain.APIKeyEnvVar, "sk-ant-ambient")
			t.Setenv(domain.AuthTokenEnvVar, "token-ambient")

			recorded := fakeClaudeOnPath(t)
			projectDir := authProject(t, config, "")

			stderr := runStandardsGenerateIn(t, projectDir, "")

			// No auth section and no flag selects no credential, so there is no
			// switch to announce. An existing project sees the output it always saw.
			if strings.Contains(stderr, "Auth:") {
				t.Errorf("stderr = %q, want no credential report for a project with no auth selection", stderr)
			}

			inherited, subprocess := envMap(os.Environ()), envMap(recorded())
			for name, want := range inherited {
				got, present := subprocess[name]
				if !present {
					t.Errorf("%s was dropped from the subprocess environment", name)
					continue
				}
				if got != want {
					t.Errorf("subprocess %s = %q, want the inherited %q", name, got, want)
				}
			}
			for name := range subprocess {
				if _, present := inherited[name]; !present {
					t.Errorf("%s was added to the subprocess environment", name)
				}
			}
		})
	}
}

// A config with no auth section is valid, not merely tolerated: loading one must
// neither fail nor warn, so no existing project needs migrating.
func TestConfigWithoutAuthSectionLoadsSilently(t *testing.T) {
	t.Setenv(domain.APIKeyEnvVar, "")
	t.Setenv(domain.AuthTokenEnvVar, "")

	projectDir := authProject(t, "verification:\n  max_iterations: 3\n", "")

	cmd := &cobra.Command{Use: "fake"}
	cmd.Flags().String("auth", "", "credential to use")
	cmd.Flags().StringP("project", "p", projectDir, "project directory")

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	mode, err := ResolveAuth(cmd)
	if err != nil {
		t.Fatalf("ResolveAuth() error = %v, want a config with no auth section to load", err)
	}
	if mode != "" {
		t.Errorf("ResolveAuth() = %q, want the empty mode", mode)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("ResolveAuth() wrote %q / %q, want silence for a config with no auth section", stdout.String(), stderr.String())
	}
}

// Every handler must keep the mode ResolveAuth hands back. Discarding it into _
// is exactly how config.models.* came to be validated but never read: the value
// was resolved, then thrown away one line later, and no test noticed because
// resolution itself kept working.
//
// This reads the package source rather than running each command because the
// alternative - driving all twelve handlers to a real spawn - needs a distinct
// fixture per command (drafts, change requests, work items) and would still miss
// any command added later. A handler that drops the mode fails here on the day
// it is written.
func TestEveryAuthHandlerConsumesResolvedMode(t *testing.T) {
	fset := token.NewFileSet()

	callsFound := 0
	for _, file := range parsePackageSource(t, fset, ".") {
		ast.Inspect(file, func(n ast.Node) bool {
			switch stmt := n.(type) {
			case *ast.AssignStmt:
				for _, rhs := range stmt.Rhs {
					if !isResolveAuthCall(rhs) {
						continue
					}
					callsFound++
					// The mode is the first result; _ means it was dropped.
					if ident, ok := stmt.Lhs[0].(*ast.Ident); ok && ident.Name == "_" {
						t.Errorf("%s: the auth mode from ResolveAuth is discarded into _ - pass it to the claude spawn site instead",
							fset.Position(stmt.Pos()))
					}
				}
			case *ast.ExprStmt:
				if isResolveAuthCall(stmt.X) {
					callsFound++
					t.Errorf("%s: ResolveAuth is called for its report only - pass the mode it returns to the claude spawn site",
						fset.Position(stmt.Pos()))
				}
			}
			return true
		})
	}

	// Without this the test would pass vacuously if ResolveAuth were renamed.
	if callsFound < len(claudeCommandPaths) {
		t.Errorf("found %d ResolveAuth call sites, want at least %d (one per command that accepts --auth)",
			callsFound, len(claudeCommandPaths))
	}
}

// parsePackageSource parses every non-test Go file directly inside dir.
//
// It reads the directory itself rather than calling parser.ParseDir, which is
// deprecated for assigning files to packages without honouring build tags. The
// module-wide walk below already parses file by file, so both structural tests
// now see source the same way.
func parsePackageSource(t *testing.T, fset *token.FileSet, dir string) []*ast.File {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read %s: %v", dir, err)
	}

	var files []*ast.File
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("failed to parse %s: %v", name, err)
		}
		files = append(files, file)
	}
	return files
}

// isResolveAuthCall reports whether an expression calls the package-level
// ResolveAuth, ignoring ResolveAuthFlag and any other similarly prefixed name.
func isResolveAuthCall(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	ident, ok := call.Fun.(*ast.Ident)
	return ok && ident.Name == "ResolveAuth"
}

// The other half of "consumed at runtime, not merely validated".
// TestEveryAuthHandlerConsumesResolvedMode proves no handler throws the mode
// away, and TestConfiguredAuthModeReachesSubprocess proves it survives one whole
// route to a spawned process. Between them sits a blind spot: a claude spawn site
// that was never offered a credential at all. A new command calling
// internal.NewCLI() bare would inherit the ambient environment no matter what the
// project configured, and every other auth test would still pass.
//
// The rule is per-file rather than per-call because three constructors
// legitimately build their CLI before any mode is known - validators' Creator,
// Editor and Runner each accept one later through a WithAuth of their own.
// Requiring such a file to declare that forwarding method excuses them without an
// allowlist of source positions to go stale, and still fails on the day a spawn
// site defers to nothing.
func TestEveryClaudeSpawnSiteSelectsACredential(t *testing.T) {
	fset := token.NewFileSet()
	spawnSites := 0

	// The whole module, not just this package: ralph, discover, harvest and
	// validators all spawn claude, and a credential that reaches only the
	// commands would split one run's usage across two accounts.
	moduleRoot := filepath.Join("..", "..")
	err := filepath.WalkDir(moduleRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			// The root is spelled "../..", whose base name would read as hidden.
			if path != moduleRoot && strings.HasPrefix(entry.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		if declaresWithAuthMethod(file) {
			return nil
		}

		var chains []*ast.CallExpr
		ast.Inspect(file, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok && rootsAtNewCLI(call) {
				chains = append(chains, call)
			}
			return true
		})

		for _, call := range chains {
			// The bare NewCLI() inside a longer chain is the same spawn site as
			// the chain wrapping it, so only the outermost one is judged.
			if enclosedByAnother(call, chains) {
				continue
			}
			spawnSites++
			if !chainSelectsCredential(call) {
				t.Errorf("%s: this claude spawn site builds a CLI without WithAuth, so it inherits the ambient environment whatever the project configured",
					fset.Position(call.Pos()))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk the module: %v", err)
	}

	// Without this a mistyped root, or a NewCLI that was renamed, would pass by
	// finding nothing at all.
	if spawnSites < len(claudeCommandPaths) {
		t.Errorf("found %d claude spawn sites, want at least %d (one per command that accepts --auth)",
			spawnSites, len(claudeCommandPaths))
	}
}

// rootsAtNewCLI reports whether a method-call chain bottoms out at NewCLI(),
// matching both the internal.NewCLI() of an importing package and the bare
// NewCLI() of package internal itself.
func rootsAtNewCLI(call *ast.CallExpr) bool {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name == "NewCLI"
	case *ast.SelectorExpr:
		if fun.Sel.Name == "NewCLI" {
			return true
		}
		inner, ok := fun.X.(*ast.CallExpr)
		return ok && rootsAtNewCLI(inner)
	}
	return false
}

// chainSelectsCredential reports whether a NewCLI chain includes a WithAuth
// call, in any position among the other With* options.
func chainSelectsCredential(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if sel.Sel.Name == "WithAuth" {
		return true
	}
	inner, ok := sel.X.(*ast.CallExpr)
	return ok && chainSelectsCredential(inner)
}

// enclosedByAnother reports whether call sits inside a longer chain in the same
// set, which makes it the inner call of that chain rather than its own site.
func enclosedByAnother(call *ast.CallExpr, all []*ast.CallExpr) bool {
	for _, other := range all {
		if other != call && other.Pos() <= call.Pos() && call.End() <= other.End() {
			return true
		}
	}
	return false
}

// declaresWithAuthMethod reports whether a file declares a WithAuth method,
// which is how a type that builds its CLI up front still accepts a credential
// afterwards.
func declaresWithAuthMethod(file *ast.File) bool {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Recv != nil && fn.Name.Name == "WithAuth" {
			return true
		}
	}
	return false
}
