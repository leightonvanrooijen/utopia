package ralph

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/ui"
)

// The defect this covers satisfied every unit test on the resolution helpers: the
// resolved model was correct and simply never reached the subprocess, because the
// default executor was the one role whose model was set on the CLI conditionally.
// So the assertion has to be made on the argv the claude binary was actually
// spawned with, from a real default-executor attempt driven through Execute -
// a test that stopped at resolveDefaultExecutorModel, or that exercised the
// escalated executor or a validator, would have passed against the defect.
func TestExecute_DefaultExecutorModelReachesTheBinary(t *testing.T) {
	tests := []struct {
		name     string
		models   *domain.ModelConfig
		override string
		want     string
		notWant  string
	}{
		{
			name:   "models.execute reaches the binary when no --model override is given",
			models: &domain.ModelConfig{Execute: "sonnet"},
			want:   "--model sonnet",
		},
		{
			name:   "models.default reaches the binary when execute is absent",
			models: &domain.ModelConfig{Default: "haiku"},
			want:   "--model haiku",
		},
		{
			// A project with no models section gets the executor default explicitly
			// rather than the claude binary's ambient one, which is the point of the
			// fix: what ran an attempt has to be a property of the project, not of the
			// machine. The wrapper's "no model configured means no flag" guarantee is
			// asserted where it is true - TestCLI_ModelAndEffortReachTheBinary.
			name:   "no models section falls back to the executor default",
			models: nil,
			want:   "--model sonnet",
		},
		{
			name:     "--model overrides models.execute for that invocation",
			models:   &domain.ModelConfig{Execute: "sonnet"},
			override: "fable",
			want:     "--model fable",
			notWant:  "sonnet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectDir := t.TempDir()
			store := internal.NewYAMLStore(filepath.Join(projectDir, ".utopia"))
			const specID = "cr-model/phase-1"

			item := &domain.WorkItem{ID: "wi-1", Order: 1, Status: domain.WorkItemPending, Prompt: "do the thing"}
			if err := store.SaveWorkItemForSpec(specID, item); err != nil {
				t.Fatalf("SaveWorkItemForSpec() = %v", err)
			}
			recorded := argRecordingClaudeOnPath(t)

			var stdout, stderr bytes.Buffer
			var result *Result
			var err error
			captureStdout(t, func() {
				result, err = Execute(context.Background(), specID, store,
					&domain.Config{Models: tt.models}, projectDir, "",
					Overrides{Out: ui.NewPrinter(&stdout, &stderr), Model: tt.override})
			})
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if result.Completed != 1 {
				t.Fatalf("Execute() completed %d of %d, want the item to reach a default-executor attempt", result.Completed, result.Total)
			}

			// One spawn, and it is the default-executor attempt: the config carries no
			// validators, so nothing else invokes claude, and the stand-in completes on
			// its first answer so there is no mechanical retry. Asserting the count
			// keeps `want` anchored to the executor rather than to whichever role
			// happened to put the flag on the run.
			invocations := recorded()
			if len(invocations) != 1 {
				t.Fatalf("claude spawned %d times %q, want exactly the one default-executor attempt", len(invocations), invocations)
			}
			args := invocations[0]
			if !strings.Contains(args, tt.want) {
				t.Errorf("claude args = %q, want %q", args, tt.want)
			}
			if tt.notWant != "" && strings.Contains(args, tt.notWant) {
				t.Errorf("claude args = %q, want the flag to have replaced %q", args, tt.notWant)
			}
		})
	}
}

// argRecordingClaudeOnPath installs a stand-in claude that records the arguments
// of every invocation and then answers with a completion token, in the
// stream-json shape the execution loop asks for. It goes on PATH rather than
// being injected because Execute builds its own *internal.CLI: only a real spawn
// can report the flags the subprocess was handed.
//
// It returns one entry per spawn rather than one blob, so an assertion can name
// which invocation it is about instead of matching anywhere in the run.
func argRecordingClaudeOnPath(t *testing.T) func() []string {
	t.Helper()

	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args")
	script := "#!/bin/sh\n" +
		`printf '%s ' "$@" >> ` + argsPath + "\n" +
		"printf '\\n' >> " + argsPath + "\n" +
		`echo '{"type":"system","subtype":"init","model":"claude-test"}'` + "\n" +
		`echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"done <COMPLETE>"}}}'` + "\n" +
		`echo '{"type":"result","subtype":"success","result":"done <COMPLETE>"}'` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake claude: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	return func() []string {
		t.Helper()

		data, err := os.ReadFile(argsPath)
		if err != nil {
			t.Fatalf("fake claude did not record its arguments: %v", err)
		}
		var invocations []string
		for _, line := range strings.Split(string(data), "\n") {
			if args := strings.Join(strings.Fields(line), " "); args != "" {
				invocations = append(invocations, args)
			}
		}
		return invocations
	}
}
