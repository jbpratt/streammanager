# Project Overview

This is a livestream manager built on the back of Linux FIFO files and FFmpeg.
An API is written in Go with two main processes - one looping over a queue and
writing it to a FIFO, the other reading from the FIFO publishing to an RTMP
endpoint. The idea is to allow FFmpeg to stream multiple files within a single
process.

# Build and Lint commands

- Build: `go build .`
- Lint: `golangci-lint run`
- Format: Go code: `gofumpt -w .`; Javascript: `deno fmt www/*.js`
- Test: `cd test && go test -v` (comprehensive e2e test available)
- Test single: `cd test && go test -v -run TestEndToEnd` (main e2e test)
- Run app: `go run main.go` (default: HTTP :8080, RTMP :1935)

# Code Style Guidelines

- Comments: Do not needlessly comment code that is self explanitory
- Formatting: Use Go formatting tool gofupmt. ALWAYS format changed Go files.
- Imports: Group standard library imports first, then third-party packages, then internal imports
- Error handling: ALWAYS wrap errors with context using fmt.Errorf and %w verb
- Logging: Use the zap package (configured for structured logging)
- Naming: Follow Go conventions (CamelCase for exported, camelCase for internal)
- Types: Prefer composition over inheritance, use interfaces for abstraction
- Comments: Document exported functions and types with meaningful comments
- Error messages: Start with lowercase, be specific and actionable
- Commits: ALWAYS make Amp the author of the commit when asked to commit something (use the `--author` flag). Never `git add .`, ONLY stage newly created files or modified files

# Feature development

- ALWAYS write unit tests or end to end tests, adding to existing suites when possible
- Use `ffprobe` to determine information about video files when testing

# Parallel Task Execution with Sub-Agents

WHEN TO USE SUB-AGENTS IN PARALLEL:
- PROACTIVELY use sub-agents for ANY independent tasks that can be performed simultaneously
- When implementing features across different parts of the codebase that don't interfere with each other
- When performing multiple file modifications, searches, or analysis operations that are unrelated
- When working on different layers of an application (frontend, backend, API layer, etc.) after planning
- ALWAYS use for multiple independent implementation tasks
- For any task where parallel execution would save time and tasks don't depend on each other

KEY PRINCIPLE: ALL INDEPENDENT TASKS CAN AND SHOULD BE PARALLELIZED WITH SUB-AGENTS
- Independent tasks include: separate feature implementations, different file modifications, isolated bug fixes, parallel research tasks
- ALWAYS spawn multiple sub-agents when you need to perform more than one independent task

WHEN NOT TO USE PARALLEL SUB-AGENTS:
- For tasks that might interfere with each other (e.g., editing the same file simultaneously)
- When later tasks depend directly on the results of earlier ones
- When tasks must be executed in a specific sequence
- For single logical operations that should be handled by one agent

HOW IT WORKS:
- Each sub-agent receives its own execution environment and task specification
- Progress is tracked independently for each sub-agent
- Results are consolidated once all sub-agents complete their work
- Sub-agents can work on different parts of the system simultaneously
