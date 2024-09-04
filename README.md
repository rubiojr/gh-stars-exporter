# gh-stars-exporter

Export GitHub stars to a SQLite database and JSON.

## Install

```bash
go install github.com/rubiojr/gh-stars-exporter@latest
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

Export sample format:

```json
[
  {
    "id": 841044067,
    "name": "timelinize",
    "html_url": "https://github.com/timelinize/timelinize",
    "description": "Store your data from all your accounts and devices in a single cohesive timeline on your own computer",
    "created_at": "2024-08-11T13:27:39Z",
    "updated_at": "2024-09-03T07:17:29Z",
    "pushed_at": "2024-09-02T15:31:59Z",
    "stargazers_count": 504,
    "language": "Go",
    "full_name": "timelinize/timelinize",
    "is_template": false,
    "topics": [
        "archival",
        "data-archiving",
        "data-import",
        "timeline"
    ],
    "private": false,
    "starred_at": "2024-08-12T17:55:48Z"
  },
  {
    "id": 841141075,
    "name": "pocket-id",
    "html_url": "https://github.com/stonith404/pocket-id",
    "description": "A simple OIDC provider that allows users to authenticate with their passkeys to your services.",
    "created_at": "2024-08-11T18:57:32Z",
    "updated_at": "2024-09-03T20:42:27Z",
    "pushed_at": "2024-09-03T20:42:25Z",
    "stargazers_count": 201,
    "language": "Svelte",
    "full_name": "stonith404/pocket-id",
    "is_template": false,
    "topics": [],
    "private": false,
    "starred_at": "2024-08-16T21:11:44Z"
  }
]
```
