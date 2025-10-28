# Nirmata Fork Changes

This document details the differences between this Nirmata-maintained fork and the upstream [GoogleCloudPlatform/kubectl-ai](https://github.com/GoogleCloudPlatform/kubectl-ai) repository.

## Overview

This fork extends kubectl-ai with additional features and security enhancements required for the Nirmata AI agent (nctl ai).

## Changes

### Security Enhancements

#### Bash Tool Directory Access Control

**Issue**: The bash tool executed shell commands without validating file paths, allowing writes outside allowed directories via shell redirection operators (`>`, `>>`, `2>`, `| tee`, etc.).

**Root Cause**: 
- The filesystem MCP server enforces allowed directories for filesystem tools (read_file, write_file)
- The bash tool executed commands directly without path validation
- Shell redirection operators bypassed the filesystem tool sandbox entirely

**Impact**: 
- Prevents shell command redirection from bypassing sandbox restrictions
- Ensures consistent security enforcement across all file operations
- Maintains backward compatibility (no restrictions when no allowed directories specified)

---
