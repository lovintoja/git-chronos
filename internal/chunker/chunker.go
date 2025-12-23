package chunker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// CommitChunk represents a proposed commit derived from a section of a diff.
type CommitChunk struct {
	CommitMessage string   `json:"commit_message"`
	FilesInvolved []string `json:"files_involved"`
}

// claudePrompt is sent as stdin to the claude subprocess.
// It instructs claude to behave as a JSON API — no markdown, no explanation.
const claudePrompt = `You are a JSON API. You will receive a unified git diff on stdin.
Analyze the diff and split it into logical commits. For each commit, provide:
- commit_message: a concise, imperative-mood commit message (e.g. "feat: add user auth")
- files_involved: list of file paths touched by that commit

Respond with ONLY a valid JSON array. No markdown, no code fences, no explanation.
Example output:
[
  {"commit_message": "feat: add login endpoint", "files_involved": ["api/auth.go", "api/auth_test.go"]},
  {"commit_message": "chore: update dependencies", "files_involved": ["go.mod", "go.sum"]}
]

Here is the diff:
`

// ChunkDiff pipes rawDiff into the claude CLI and returns parsed commit chunks.
// It returns an error if:
//   - claude is not found in PATH
//   - the subprocess exits non-zero
//   - stdout cannot be parsed as []CommitChunk JSON
func ChunkDiff(rawDiff string) ([]CommitChunk, error) {
	// Verify claude is available in PATH before attempting execution
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		return nil, fmt.Errorf("claude CLI not found in PATH: %w\n  Install it from https://claude.ai/code or ensure it is on your $PATH", err)
	}

	// Build the full input: prompt + diff
	input := claudePrompt + rawDiff

	// Construct the command. We pass flags so claude runs non-interactively:
	//   --print    = print response to stdout and exit (non-interactive mode)
	//   --no-markdown = disable markdown rendering so we get raw text
	//   -p ""      = empty system prompt override (we supply everything via stdin)
	// The actual user message is delivered via stdin pipe.
	cmd := exec.Command(claudePath, "--print", "--output-format", "text")

	// Pipe the prompt+diff into stdin — NEVER pass diff as a CLI argument
	cmd.Stdin = strings.NewReader(input)

	// Capture stdout and stderr separately
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		stderrStr := strings.TrimSpace(stderr.String())
		if stderrStr != "" {
			return nil, fmt.Errorf("claude subprocess failed: %w\nstderr: %s", err, stderrStr)
		}
		return nil, fmt.Errorf("claude subprocess failed: %w", err)
	}

	raw := strings.TrimSpace(stdout.String())
	if raw == "" {
		return nil, fmt.Errorf("claude returned empty output")
	}

	// Strip markdown code fences if claude wrapped the JSON despite instructions
	raw = stripCodeFences(raw)

	var chunks []CommitChunk
	if err := json.Unmarshal([]byte(raw), &chunks); err != nil {
		// Include the raw output in the error to aid debugging
		preview := raw
		if len(preview) > 500 {
			preview = preview[:500] + "...(truncated)"
		}
		return nil, fmt.Errorf("failed to parse claude output as JSON: %w\nraw output: %s", err, preview)
	}

	return chunks, nil
}

// stripCodeFences removes optional ```json ... ``` or ``` ... ``` wrappers.
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	for _, fence := range []string{"```json", "```"} {
		if strings.HasPrefix(s, fence) {
			s = strings.TrimPrefix(s, fence)
			s = strings.TrimSuffix(s, "```")
			s = strings.TrimSpace(s)
			break
		}
	}
	return s
}
