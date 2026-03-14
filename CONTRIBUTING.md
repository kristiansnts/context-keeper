# Contributing to context-keeper

Thank you for your interest in contributing! 🎉

## Setup

```bash
git clone https://github.com/yourusername/context-keeper
cd context-keeper
npm install
npm run build
```

## Project Structure

```
packages/
├── core/        # Shared SQLite storage engine
├── cli/         # Terminal CLI (ctx command)
├── mcp-server/  # MCP server (Claude Code, Cursor, Continue)
└── vscode/      # VS Code extension (GitHub Copilot)
```

## Development

```bash
# Build all packages
npm run build

# Watch mode
npm run dev

# Run tests
npm run test
```

## Pull Request Guidelines

- Keep PRs focused on a single feature or fix
- Add tests for new functionality
- Update README if adding new features
- Use conventional commits: `feat:`, `fix:`, `docs:`, `chore:`

## Issues

- Bug reports: Use the bug report template
- Feature requests: Describe the use case clearly
- Questions: Use GitHub Discussions

## Code of Conduct

Be kind. Be constructive. Help each other build better tools.
