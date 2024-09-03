package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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

func jsonExport(sess db.Session) error {
	stars := []*Repository{}
	sess.Collection("starred_repos").Find().All(&stars)

	b, err := json.MarshalIndent(stars, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))

	return nil
}

func dbInit() (db.Session, error) {
	if err := migrateDB(); err != nil {
		return nil, err
	}

	settings := sqlite.ConnectionURL{
		Database: dbFile,
	}
	sess, err := sqlite.Open(settings)

	return sess, err
}

func token() string {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		logger.Fatal("GITHUB_TOKEN is required")
	}
	return token
}

func main() {
	flag.Parse()

	if debug {
		logger.SetLevel(log.DebugLevel)
	}

	if skipUpdate && jsonFlag {
		if _, err := os.Stat(dbFile); os.IsNotExist(err) {
			logger.Fatal("Database file not found, use the exporter without --skip-update at least once.")
		}
	}

	sess, err := dbInit()
	if err != nil {
		logger.Fatal("opening database", err)
	}
	stars := sess.Collection("starred_repos")

	updatedStars := 0
	if !skipUpdate {
		logger.Info("Fetching stars from github.com...")
		err = fetchAllStarredRepos(token(), func(repos []Repository) error {
			for _, repo := range repos {
				repo.Topics = strings.Join(repo.TopicList, ",")
				if repo.Private && !storePrivate {
					logger.Warnf("Skipping private repository %s", repo.FullName)
					continue
				}

				res := stars.Find(db.Cond{"id": repo.ID})
				ok, err := res.Exists()
				if ok || err != nil {
					return nil
				}

				updatedStars++
				_, err = sess.Collection("starred_repos").Insert(repo)
				if err != nil {
					return err
				}
			}

			return nil
		})

		logger.Infof("Updated stars: %d", updatedStars)
	} else {
		logger.Info("Skipping update (offline mode)")
	}

	if jsonFlag {
		err = jsonExport(sess)
		if err != nil {
			logger.Fatal("exporting to JSON", err)
		}
	}
}

func fetchAllStarredRepos(githubToken string, iterator func([]Repository) error) error {
	nextPageURL := "https://api.github.com/user/starred?per_page=100"

	client := &http.Client{
		Timeout: time.Second * 10,
	}

	currentPage := 1
	for nextPageURL != "" {
		logger.Debugf("Page URL %s", nextPageURL)
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

		pagerLink := resp.Header.Get("Link")
		nextPageURL = getNextPageURL(pagerLink)
		pageCount := getPageCount(pagerLink)
		if pageCount == "" {
			pageCount = fmt.Sprintf("%d", currentPage)
		}
		logger.Debugf("Fetching stars... (page %d/%s)", currentPage, pageCount)
		currentPage++
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

// getNextPageURL parses the Link header from GitHub API response and finds the URL for the next page.
func getPageCount(linkHeader string) string {
	if linkHeader == "" {
		return ""
	}

	links := strings.Split(linkHeader, ",")
	for _, link := range links {
		parts := strings.Split(link, ";")
		if len(parts) == 2 && strings.TrimSpace(parts[1]) == `rel="last"` {
			nextPageURL := strings.TrimSpace(parts[0])
			u, err := url.Parse(strings.Trim(nextPageURL, "<>"))
			if err != nil {
				return ""
			}
			return u.Query().Get("page")
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
var jsonFlag bool
var storePrivate bool
var skipUpdate bool

func init() {
	flag.StringVar(&dbFile, "db", "data.ghstars", "Database file")
	flag.BoolVar(&debug, "debug", false, "Enable debug mode")
	flag.BoolVar(&skipUpdate, "skip-update", false, "Do not update the database (offline, use existing data)")
	flag.BoolVar(&jsonFlag, "json", false, "JSON Export to stdout")
	flag.BoolVar(&storePrivate, "store-private", false, "Store private starred repositories")
}
