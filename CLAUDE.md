# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Venaqui is a Go CLI tool with a Terminal UI (TUI) for high-speed downloads. It integrates Real-Debrid (premium link unrestriction service) with aria2 (download manager) and provides a polished terminal interface built with Bubble Tea.

## Build & Development Commands

```bash
make build             # Build binary for current platform
make install           # Install to $GOPATH/bin
make test              # Run all tests (go test ./...)
make clean             # Remove build artifacts
make release           # Build for all platforms (Linux, macOS, Windows)
go test -v ./pkg/...   # Run tests for a specific package
go build -o venaqui ./cmd/venaqui  # Direct build
```

Version info is injected via ldflags: `main.version`, `main.commit`, `main.date`.

## Architecture

**Entry point**: `cmd/venaqui/main.go` — Cobra CLI that orchestrates the download flow: validate input → check aria2 → process link via Real-Debrid → queue download via aria2 → launch TUI.

**Internal packages** (`internal/`):
- **tui/**: Bubble Tea Model-View-Update pattern. `model.go` holds state, `update.go` handles events (key presses, tick-based status polling every 1s), `view.go` renders progress bars/stats/speed graphs, `styles.go` defines the red color palette with Lip Gloss.
- **realdebrid/**: HTTP client for Real-Debrid REST API. Handles link unrestriction (`unrestrict.go`), torrent/magnet processing (`torrent.go`), and token validation (`auth.go`).
- **aria2/**: WebSocket RPC client using arigo. Manages downloads with 16 connections/16 splits (`download.go`) and tracks status/progress/ETA (`status.go`).
- **config/**: Viper-based YAML config from `~/.venaqui/config.yaml`. Holds RD API token, aria2 RPC settings, download directory.
- **utils/**: URL/path validation, link type detection (magnet vs torrent vs hoster), download folder resolution.

**Public package** (`pkg/models/`): `DownloadState` enum and `Download` model with progress helpers.

## Key Patterns

- TUI uses Elm architecture (Bubble Tea): all state changes flow through `Update()`, rendering is pure in `View()`
- Speed history is a circular buffer (50 samples) rendered as an ASCII graph
- Cross-platform file operations in `tui/open.go` dispatch to OS-specific commands (open/xdg-open/explorer)
- Real-Debrid torrent flow polls until ready (max 5 min) before unrestricting the download link

## Runtime Dependencies

- **aria2** must be installed and accessible on PATH
- **Real-Debrid API token** configured in `~/.venaqui/config.yaml`

## Release

GoReleaser builds for linux/amd64, darwin/amd64, darwin/arm64, windows/amd64. GitHub Actions triggers on `v*` tags. Homebrew tap: `mhrsntrk/homebrew-venaqui`.
