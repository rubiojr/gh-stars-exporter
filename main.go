package main

import (
	"database/sql"
	"database/sql/driver"
	"embed"
	"encoding/json"
	"fmt"
	"io"
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

var readmeFiles = []string{
	"README.md",
	"README.rst",
	"docs/README.md",
	".github/README.md",
	"README.adoc",
	"README.markdown",
	"README.rdoc",
	"README.txt",
	"README",
	"readme.md",
	"Readme.md",
	"README.MD",
	"readme",
	"Readme",
	"readme.rst",
	"Readme.rst",
	"README.org",
	"Readme.org",
	"readme.org",
	"docs/Readme.md",
	"docs/readme.md",
}

var logger = log.NewWithOptions(os.Stderr, log.Options{
	ReportTimestamp: false,
})

type ConnectionURL struct {
	Database string
}

type StarredRepo struct {
	Repo      Repository `json:"repo"`
	StarredAt time.Time  `json:"starred_at"`
}

type Repository struct {
	ID              int            `json:"id" db:"id"`
	Name            string         `json:"name" db:"name"`
	HTMLURL         string         `json:"html_url" db:"html_url"`
	Description     string         `json:"description" db:"description"`
	CreatedAt       time.Time      `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at" db:"updated_at"`
	PushedAt        time.Time      `json:"pushed_at" db:"pushed_at"`
	StargazersCount int            `json:"stargazers_count" db:"stargazers_count"`
	Language        string         `json:"language" db:"language"`
	FullName        string         `json:"full_name" db:"full_name"`
	Topics          StringList     `json:"topics" db:"topics"`
	IsTemplate      bool           `json:"is_template" db:"is_template"`
	Private         bool           `json:"private" db:"private"`
	StarredAt       time.Time      `json:"starred_at" db:"starred_at"`
	Readme          sql.NullString `json:"readme" db:"readme"`
}

type StringList []string

func (sl StringList) Value() (driver.Value, error) {
	return strings.Join(sl, ","), nil
}

func (sl *StringList) Scan(value interface{}) error {
	if value == nil {
		*sl = nil
		return nil
	}

	if bv, err := driver.String.ConvertValue(value); err == nil {
		if v, ok := bv.(string); ok {
			*sl = strings.Split(v, ",")
			return nil
		}
	}

	return fmt.Errorf("failed to scan StringList")
}

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

var newStars int
var updatedStars int

func main() {
	flag.Parse()

	if debug {
		logger.SetLevel(log.DebugLevel)
	}

	if getReadme {
		logger.Info("Fetching READMEs enabled")
	}

	if jsonFlag {
		logger.Info("JSON export enabled")
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

	newStars := 0
	if !skipUpdate {
		logger.Info("Fetching stars from github.com...")
		err = fetchAllStarredRepos(token(), func(repos []StarredRepo) error {
			for _, sr := range repos {
				repo := sr.Repo
				repo.StarredAt = sr.StarredAt
				if repo.Private && !storePrivate {
					logger.Warnf("Skipping private repository %s", repo.FullName)
					continue
				}

				res := stars.Find(db.Cond{"id": repo.ID})
				var r Repository
				err := res.One(&r)
				if err == nil {
					if getReadme {
						updateRepoReadme(r, res)
					}
					continue
				}

				return addNewRepo(repo, sess)
			}

			return nil
		})
		logger.Infof("New stars: %d", newStars)
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

func addNewRepo(repo Repository, sess db.Session) error {
	if getReadme {
		readme, err := getReadmeContent(repo)
		if err != nil {
			logger.Warnf("Failed to fetch README for %s: %s", repo.FullName, err)
		} else {
			repo.Readme = sql.NullString{String: readme, Valid: true}
		}
	}

	_, err := sess.Collection("starred_repos").Insert(repo)
	if err == nil {
		newStars++
	}
	return err
}

func updateRepoReadme(r Repository, res db.Result) error {
	logger.Debugf("Repository %s already exists in the database", r.FullName)

	if r.Readme.Valid {
		logger.Debug("README already exists")
		return nil
	}

	logger.Debugf("Updating README for %s", r.FullName)
	readme, err := getReadmeContent(r)
	if err != nil {
		logger.Warnf("Failed to fetch README for %s, ignoring: %s", r.FullName, err)
		return nil
	}

	r.Readme = sql.NullString{String: readme, Valid: true}
	err = res.Update(r)
	if err == nil {
		logger.Debugf("Updated README for %s", r.FullName)
		updatedStars++
	}
	return err
}

func fetchAllStarredRepos(githubToken string, iterator func([]StarredRepo) error) error {
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
		req.Header.Set("Accept", "application/vnd.github.star+json")
		//req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return err
		}

		var repos []StarredRepo
		if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
			return err
		}

		pagerLink := resp.Header.Get("Link")
		nextPageURL = getNextPageURL(pagerLink)
		pageCount := getPageCount(pagerLink)
		if pageCount == "" {
			pageCount = fmt.Sprintf("%d", currentPage)
		}
		logger.Infof("Fetching stars... (page %d/%s)", currentPage, pageCount)

		err = iterator(repos)
		if err != nil {
			return err
		}

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

func getReadmeContent(repo Repository) (string, error) {
	baseURL := fmt.Sprintf("https://api.github.com/repos/%s/contents/", repo.FullName)
	client := &http.Client{Timeout: time.Second * 10}

	for _, file := range readmeFiles {
		req, err := http.NewRequest("GET", baseURL+file, nil)
		if err != nil {
			return "", err
		}

		req.Header.Set("Authorization", "Bearer "+token())
		req.Header.Set("Accept", "application/vnd.github.v3.raw")

		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			content, err := io.ReadAll(resp.Body)
			if err != nil {
				return "", err
			}
			return string(content), nil
		}
	}

	return "", fmt.Errorf("no readme found for %s", repo.FullName)
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
	logger.Debug("Migrating database...")

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
var getReadme bool

func init() {
	flag.StringVar(&dbFile, "db", "data.ghstars", "Database file")
	flag.BoolVar(&debug, "debug", false, "Enable debug mode")
	flag.BoolVar(&skipUpdate, "skip-update", false, "Do not update the database (offline, use existing data)")
	flag.BoolVar(&jsonFlag, "json", false, "JSON Export to stdout")
	flag.BoolVar(&getReadme, "get-readme", false, "JSON Export to stdout")
	flag.BoolVar(&storePrivate, "store-private", false, "Store private starred repositories")
}
