// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateShellCommandPaths(t *testing.T) {
	// Create temporary directories for testing
	tmpDir, err := os.MkdirTemp("", "test-allowed")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create subdirectory for allowed files
	allowedSubDir := filepath.Join(tmpDir, "allowed")
	if err := os.MkdirAll(allowedSubDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a directory outside the allowed directory
	outsideDir, err := os.MkdirTemp("", "test-outside")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(outsideDir)

	allowedDirs := []string{tmpDir}
	workDir := tmpDir

	tests := []struct {
		name        string
		command     string
		workDir     string
		allowedDirs []string
		expectError bool
		errorMsg    string
	}{
		// Valid commands within allowed directories
		{
			name:        "simple redirect within allowed dir",
			command:     "echo test > file.txt",
			workDir:     workDir,
			allowedDirs: allowedDirs,
			expectError: false,
		},
		{
			name:        "append redirect within allowed dir",
			command:     "echo test >> file.txt",
			workDir:     workDir,
			allowedDirs: allowedDirs,
			expectError: false,
		},
		{
			name:        "stderr redirect within allowed dir",
			command:     "echo test 2> error.txt",
			workDir:     workDir,
			allowedDirs: allowedDirs,
			expectError: false,
		},
		{
			name:        "redirect to subdirectory",
			command:     "echo test > allowed/subfile.txt",
			workDir:     workDir,
			allowedDirs: allowedDirs,
			expectError: false,
		},
		{
			name:        "pipe to tee within allowed dir",
			command:     "echo test | tee output.txt",
			workDir:     workDir,
			allowedDirs: allowedDirs,
			expectError: false,
		},
		{
			name:        "multiple redirects",
			command:     "echo test > file1.txt >> file2.txt",
			workDir:     workDir,
			allowedDirs: allowedDirs,
			expectError: false,
		},
		{
			name:        "absolute path within allowed dir",
			command:     "echo test > " + filepath.Join(allowedSubDir, "file.txt"),
			workDir:     workDir,
			allowedDirs: allowedDirs,
			expectError: false,
		},

		// Invalid commands outside allowed directories
		{
			name:        "redirect outside allowed dir",
			command:     "echo test > " + filepath.Join(outsideDir, "blocked.txt"),
			workDir:     workDir,
			allowedDirs: allowedDirs,
			expectError: true,
			errorMsg:    "access denied",
		},
		{
			name:        "pipe to tee outside allowed dir",
			command:     "echo test | tee " + filepath.Join(outsideDir, "blocked.txt"),
			workDir:     workDir,
			allowedDirs: allowedDirs,
			expectError: true,
			errorMsg:    "access denied",
		},
		{
			name:        "stderr redirect outside allowed dir",
			command:     "echo test 2> " + filepath.Join(outsideDir, "error.txt"),
			workDir:     workDir,
			allowedDirs: allowedDirs,
			expectError: true,
			errorMsg:    "access denied",
		},
		{
			name:        "redirect with parent directory traversal - outside allowed",
			command:     "echo test > ../blocked.txt",
			workDir:     allowedSubDir,
			allowedDirs: []string{allowedSubDir}, // only subdir is allowed
			expectError: true,
			errorMsg:    "access denied",
		},
		{
			name:        "relative path outside allowed dir",
			command:     "echo test > ../../blocked.txt",
			workDir:     allowedSubDir,
			allowedDirs: []string{allowedSubDir}, // only subdir allowed
			expectError: true,
			errorMsg:    "access denied",
		},
		{
			name:        "parent traversal within allowed root",
			command:     "echo test > ../allowed-file.txt",
			workDir:     allowedSubDir,
			allowedDirs: allowedDirs, // tmpDir is allowed
			expectError: false,       // ../ allowed because it's within tmpDir
		},

		// Edge cases
		{
			name:        "no allowed dirs specified",
			command:     "echo test > anywhere.txt",
			workDir:     workDir,
			allowedDirs: []string{},
			expectError: false,
		},
		{
			name:        "command without redirection",
			command:     "ls -la",
			workDir:     workDir,
			allowedDirs: allowedDirs,
			expectError: false,
		},
		{
			name:        "command with input redirection",
			command:     "cat < input.txt",
			workDir:     workDir,
			allowedDirs: allowedDirs,
			expectError: false,
		},
		{
			name:        "complex command with valid paths",
			command:     "ls . | tee output.txt > backup.txt",
			workDir:     workDir,
			allowedDirs: allowedDirs,
			expectError: false,
		},
		{
			name:        "quoted paths",
			command:     "echo test > 'file with spaces.txt'",
			workDir:     workDir,
			allowedDirs: allowedDirs,
			expectError: false,
		},
		{
			name:        "quoted paths outside allowed dir",
			command:     "echo test > '" + filepath.Join(outsideDir, "blocked.txt") + "'",
			workDir:     workDir,
			allowedDirs: allowedDirs,
			expectError: true,
			errorMsg:    "access denied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateShellCommandPaths(tt.command, tt.workDir, tt.allowedDirs)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
					return
				}
				if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error to contain %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestBashTool_Run(t *testing.T) {
	// Create temporary directories for testing
	tmpDir, err := os.MkdirTemp("", "test-bash-allowed")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	outsideDir, err := os.MkdirTemp("", "test-bash-outside")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(outsideDir)

	tool := &BashTool{}

	tests := []struct {
		name        string
		command     string
		workDir     string
		allowedDirs []string
		expectError bool
	}{
		{
			name:        "valid command without redirection",
			command:     "echo test",
			workDir:     tmpDir,
			allowedDirs: []string{tmpDir},
			expectError: false,
		},
		{
			name:        "valid redirect to allowed dir",
			command:     "echo test > output.txt",
			workDir:     tmpDir,
			allowedDirs: []string{tmpDir},
			expectError: false,
		},
		{
			name:        "invalid redirect outside allowed dir",
			command:     "echo test > " + filepath.Join(outsideDir, "blocked.txt"),
			workDir:     tmpDir,
			allowedDirs: []string{tmpDir},
			expectError: true,
		},
		{
			name:        "no restrictions when no allowed dirs",
			command:     "echo test > anywhere.txt",
			workDir:     tmpDir,
			allowedDirs: []string{},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			ctx = context.WithValue(ctx, KubeconfigKey, "")
			ctx = context.WithValue(ctx, WorkDirKey, tt.workDir)
			if len(tt.allowedDirs) > 0 {
				ctx = context.WithValue(ctx, AllowedDirsKey, tt.allowedDirs)
			}

			args := map[string]any{
				"command": tt.command,
			}

			result, err := tool.Run(ctx, args)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			execResult, ok := result.(*ExecResult)
			if !ok {
				t.Errorf("expected ExecResult, got %T", result)
				return
			}

			if tt.expectError {
				if execResult.Error == "" {
					t.Errorf("expected error but got none")
				}
				if !strings.Contains(execResult.Error, "access denied") {
					t.Errorf("expected 'access denied' error, got %q", execResult.Error)
				}
			} else {
				if execResult.Error != "" && strings.Contains(execResult.Error, "access denied") {
					t.Errorf("unexpected access denied error: %s", execResult.Error)
				}
			}
		})
	}
}
