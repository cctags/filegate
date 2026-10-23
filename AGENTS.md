# Repository Guidelines

## Project Structure & Module Organization

This workspace currently contains no application source, tests, assets, or build manifests. Establish the project structure with the first implementation and update this guide to match. Suggested directories are `src/` for application code, `tests/` for automated tests, `public/` for static assets, and `docs/` for supporting documentation. Group modules by feature as the application grows.

## Build, Test, and Development Commands

No development, build, lint, or test commands are configured yet. When introducing a toolchain, document its prerequisites and exact setup and execution commands in `README.md`. Provide consistent entry points for local development, production builds, linting, and tests. Verify commands before adding them here; do not assume commands such as `npm test` are available.

## Coding Style & Naming Conventions

Follow the conventions of the chosen language and framework. Configure a formatter and linter early, and use their configuration as the source of truth for indentation and formatting. Keep naming consistent within each module, use descriptive identifiers, and avoid mixing unrelated formatting changes into functional changes.

## Testing Guidelines

No testing framework or coverage threshold is established. Add tests alongside new behavior once a framework is selected. Use descriptive test names that explain the expected outcome, follow the framework’s file naming convention, and prioritize critical workflows and regression cases. Document how to run individual tests and the complete suite.

## Commit & Pull Request Guidelines

No Git history is available to establish existing commit conventions. Use concise, imperative commit subjects, such as `Add initial application scaffold`. Keep changes focused. Pull requests should describe the purpose, summarize behavior changes, link relevant issues, and report validation performed. Include screenshots for visible interface changes and explain any setup or configuration changes.

## Security & Configuration

Keep credentials and local configuration out of version control. Document required environment variables with placeholder values in an example configuration file. Review new dependencies and commit the selected package manager’s lockfile when applicable.
