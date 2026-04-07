# Repository Guidelines

## Project Structure & Module Organization
`main.go` is the Wails entrypoint. Core backend logic lives in `services/`, with most Go tests beside the code as `*_test.go`. The Vue 3 frontend is under `frontend/src/` with feature folders such as `components/`, `composables/`, `router/`, and `services/`. Generated Wails TypeScript bindings live in `frontend/bindings/`; regenerate them instead of editing by hand. Packaging assets and cross-platform tasks live in `build/`, while static assets are split across `assets/` and `resources/`.

## Build, Test, and Development Commands
Use the repository task runner first:

- `wails3 task dev`: run the desktop app in development mode.
- `wails3 task build`: build the current platform binary.
- `wails3 task package`: create a production package for the current OS.
- `wails3 task common:generate:bindings`: refresh `frontend/bindings/` after Go API changes.
- `wails3 task common:update:build-assets`: refresh Wails build assets.

For frontend-only work, use `frontend/package.json` scripts:

- `npm install` in `frontend/`: install dependencies.
- `npm run build` in `frontend/`: production type-check and bundle.
- `npm run build:dev` in `frontend/`: development bundle without minification.

On Windows, keep the build order strict: build `frontend/dist` first, then run Go build. Do not run them in parallel because `main.go` embeds `frontend/dist`, and concurrent rebuilds can race on asset filenames. In this environment, never rely on the default Go architecture for release testing; explicitly set `GOARCH=amd64` or you may produce a 32-bit executable that exits immediately. Known-good sequence for a single-file test build is: `cd frontend && npm ci && npm run build`, then from repo root run `go build -trimpath -buildvcs=false -ldflags='-w -s -H windowsgui' -o bin/<name>.exe` with `GOOS=windows`, `GOARCH=amd64`, and `CGO_ENABLED=0`.

## Coding Style & Naming Conventions
Follow existing Go and Vue conventions rather than reformatting unrelated files. Run `gofmt` on changed Go files; keep Go package names lowercase and tests in `*_test.go`. Vue components use PascalCase filenames such as `BaseModal.vue`; composables use `useXxx.ts`; utility modules use descriptive camelCase names. Prefer small functions, early returns, and explicit constants over magic numbers.

## Testing Guidelines
Backend coverage is primarily Go unit tests in `services/` and repository-root `*_test.go` files. Run focused checks first, for example `go test ./services -run TestGemini -timeout 60s`, then broaden to `go test ./... -timeout 60s` when the change crosses packages. There is no dedicated frontend test suite in this snapshot, so validate UI changes with `npm run build` and a manual `wails3 task dev` pass.

## Commit & Pull Request Guidelines
Recent history uses Conventional Commit style, e.g. `fix: 修复 v2.6.25 CI 构建失败并重新生成前端绑定`. Keep subjects short, imperative, and scoped when useful (`feat:`, `fix:`, `refactor:`). PRs should explain the user-visible change, list verification commands, link related issues, and include screenshots for UI work. Do not mix generated bindings, packaging output, and unrelated cleanup into the same review.
