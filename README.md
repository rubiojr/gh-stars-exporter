# gh-stars-exporter

Export GitHub stars to a SQLite database and JSON.

## Install

```bash
go install github.com/kevinpollet/gh-stars-exporter@latest
```

## Usage

```bash
export GITHUB_TOKEN=your_github_token
gh-stars-exporter --db stars.db
```

### JSON exports

```bash
export GITHUB_TOKEN=your_github_token
gh-stars-exporter --db stars.db --json > ghstars.json
```

JSON exports also supports offline mode, using the `--skip-update` flag. It'll export the stars from the existing database.
