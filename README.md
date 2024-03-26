# gh-stars-exporter

Export GitHub stars to a SQLite database.

## Install

```bash
go install github.com/kevinpollet/gh-stars-exporter@latest
```

## Usage

```bash
export GITHUB_TOKEN=your_github_token
gh-stars-exporter --db stars.db
```
