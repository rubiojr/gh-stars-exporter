CREATE TABLE IF NOT EXISTS starred_repos (
	id INTEGER PRIMARY KEY,
	name TEXT NOT NULL,
	html_url TEXT NOT NULL,
	description TEXT,
	created_at DATETIME,
	updated_at DATETIME,
	pushed_at DATETIME,
	stargazers_count INTEGER,
	language TEXT,
	full_name TEXT,
	topics TEXT,
	is_template BOOLEAN,
	private BOOLEAN,
	starred_at DATETIME
);
