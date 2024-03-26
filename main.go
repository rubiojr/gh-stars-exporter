package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"flag"

	"github.com/charmbracelet/log"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/mattn/go-sqlite3"
	"github.com/upper/db/v4"
	"github.com/upper/db/v4/adapter/sqlite"
)

//go:embed db/migrations/*.sql
var fs embed.FS

type ConnectionURL struct {
	Database string
}

type Repository struct {
	ID              int       `json:"id" db:"id"`
	Name            string    `json:"name" db:"name"`
	HTMLURL         string    `json:"html_url" db:"html_url"`
	Description     string    `json:"description" db:"description"`
	CreatedAt       time.Time `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time `json:"updated_at" db:"updated_at"`
	PushedAt        time.Time `json:"pushed_at" db:"pushed_at"`
	StargazersCount int       `json:"stargazers_count" db:"stargazers_count"`
	Language        string    `json:"language" db:"language"`
	FullName        string    `json:"full_name" db:"full_name"`
	TopicList       []string  `json:"topics"`
	IsTemplate      bool      `json:"is_template" db:"is_template"`
	Topics          string    `db:"topics"`
	Private         bool      `json:"private" db:"private"`
}

var logger = log.NewWithOptions(os.Stderr, log.Options{
	ReportTimestamp: false,
})

func main() {
	flag.Parse()

	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		logger.Fatal("GITHUB_TOKEN is required")
	}

	if debug {
		logger.SetLevel(log.DebugLevel)
	}

	if err := migrateDB(); err != nil {
		logger.Fatal(err)
	}

	settings := sqlite.ConnectionURL{
		Database: dbFile,
	}
	sess, err := sqlite.Open(settings)
	if err != nil {
		logger.Fatal(err)
	}
	defer sess.Close()

	stars := sess.Collection("starred_repos")
	err = fetchAllStarredRepos(os.Getenv("GITHUB_TOKEN"), func(repos []Repository) error {

		for _, repo := range repos {
			res := stars.Find(db.Cond{"id": repo.ID})
			ok, err := res.Exists()
			if ok || err != nil {
				continue
			}

			repo.Topics = strings.Join(repo.TopicList, ",")
			if repo.Private && !storePrivate {
				logger.Infof("skipping private repository %s", repo.FullName)
				continue
			}

			_, err = sess.Collection("starred_repos").Insert(repo)
			if err != nil {
				return nil
			}
		}

		return nil
	})

	if err != nil {
		logger.Fatal(err)
	}
}

func fetchAllStarredRepos(githubToken string, iterator func([]Repository) error) error {
	nextPageURL := "https://api.github.com/user/starred?per_page=100"

	client := &http.Client{}

	for nextPageURL != "" {
		logger.Debugf("Fetching stars %s", nextPageURL)
		req, err := http.NewRequest("GET", nextPageURL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+githubToken)

		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return err
		}

		var repos []Repository
		if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
			return err
		}
		err = iterator(repos)
		if err != nil {
			return err
		}

		nextPageURL = getNextPageURL(resp.Header.Get("Link"))
	}

	return nil
}

// getNextPageURL parses the Link header from GitHub API response and finds the URL for the next page.
func getNextPageURL(linkHeader string) string {
	if linkHeader == "" {
		return ""
	}

	links := strings.Split(linkHeader, ",")
	for _, link := range links {
		parts := strings.Split(link, ";")
		if len(parts) == 2 && strings.TrimSpace(parts[1]) == `rel="next"` {
			nextPageURL := strings.TrimSpace(parts[0])
			return strings.Trim(nextPageURL, "<>")
		}
	}

	return ""
}

func migrateDB() error {
	d, err := iofs.New(fs, "db/migrations")
	if err != nil {
		return err
	}
	m, err := migrate.NewWithSourceInstance("iofs", d, fmt.Sprintf("sqlite3://%s", dbFile))
	if err != nil {
		return err
	}
	defer m.Close()

	err = m.Up()
	if err != nil && err != migrate.ErrNoChange {
		return err
	}

	return nil
}

var dbFile string
var debug bool
var storePrivate bool

func init() {
	flag.StringVar(&dbFile, "db", "data.ghstars", "Database file")
	flag.BoolVar(&debug, "debug", false, "Enable debug mode")
	flag.BoolVar(&storePrivate, "store-private", false, "Store private starred repositories")
}
