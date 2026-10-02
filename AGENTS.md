# AGENTS.md

## General

Act as a senior Go engineer.

Prefer simple, idiomatic, maintainable Go over clever abstractions.

Follow the existing codebase's conventions unless there is a strong reason to change them.

## Design

- Prefer the Go standard library when practical.
- Avoid unnecessary dependencies and frameworks.
- Avoid unnecessary abstraction and excessive layering.
- Keep packages focused and cohesive.
- Prefer explicit dependency injection over global state.
- Introduce interfaces only when they provide a concrete benefit.
- Do not prematurely optimize.
- Do not perform unrelated refactoring while implementing a feature.

## Error Handling

- Never silently ignore errors.
- Wrap errors with useful context when propagating them.
- Do not expose sensitive internal information in user-facing errors.

## Security

- Treat external input as untrusted.
- Never hardcode or commit secrets.
- Use parameterized SQL queries.
- Use established cryptographic implementations rather than custom cryptography.

## Testing

New functionality should include appropriate tests.

Bug fixes should include regression tests when practical.

Before considering work complete, run:

```bash
gofmt -w <modified files>
go test ./...
go vet ./...
```

Do not claim tests pass unless they were actually executed successfully.

## Working With Existing Code

Before making significant changes:

1. Inspect the relevant existing code.
2. Understand existing patterns and conventions.
3. Search for related functionality before creating something new.
4. Preserve existing APIs unless changing them is necessary.

Keep changes focused on the requested task.

## Large Features

For substantial features:

1. Review the project specification and architecture.
2. Create or review the implementation plan.
3. Break work into independently testable milestones.
4. Implement one milestone at a time.
5. Test each milestone before continuing.
6. Keep project documentation synchronized with implementation.

Do not make significant architectural changes without explaining the reason.

## Ambiguity

If ambiguity would materially affect architecture, security, persistent data, APIs, or user-visible behavior, ask for clarification.

For minor implementation details, choose the simplest reasonable approach consistent with the existing codebase.

## Completion

Before declaring a task complete:

- Format the code.
- Run relevant tests.
- Run `go vet`.
- Review the diff for unintended changes.
- Update relevant documentation.
- Report what changed, tests executed, and any remaining limitations.
