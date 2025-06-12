# Build and Lint commands

- Build: `go build .`
- Lint: `golangci-lint run`
- Format: Go code: `gofumpt -w .`; Javascript: `deno fmt www/*.js`
- Test: `cd test && go test -v`

# Code Style Guidelines

- Comments: Do not needlessly comment code that is self explanitory
- Formatting: Use Go formatting tool gofupmt. Always format changed Go files.
- Imports: Group standard library imports first, then third-party packages, then internal imports
- Error handling: Always wrap errors with context using fmt.Errorf and %w verb
- Logging: Use the log package
- Naming: Follow Go conventions (CamelCase for exported, camelCase for internal)
- Types: Prefer composition over inheritance, use interfaces for abstraction
- Comments: Document exported functions and types with meaningful comments
- Error messages: Start with lowercase, be specific and actionable
- Commits: Always make Amp the author of the commit when asked to commit something
