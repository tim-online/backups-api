package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/egonelbre/slice"
)

var borgBinary string

type archive struct {
	Name     string
	RepoName string
	Datetime time.Time
}

type file struct {
	Path  string
	Mtime time.Time
}

type jsonFormat struct {
	Repo string `json:"repo"`
	Date string `json:"date"`
	MysqlDate string `json:"mysql_date"`
}

func main() {
	// http server
	// /recent
	// find repositories
	// get archives from repository
	// parse / sort data

	var err error
	var port = flag.Int("port", 2674, "specify a port to listen on. Default is 2674")
	flag.Parse()
	addr := fmt.Sprintf(":%v", *port)

	borgBinary, err = findBorgBinary()
	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/recent", recentHandler)
	err = http.ListenAndServe(addr, nil)
	if err != nil {
		log.Fatal(err)
	}
}

// Format into:
// [
// 	{
// 		"repo": "ironhide.tim-online.nl",
// 		"date": "2012-04-23T18:25:43.511Z"
// 		"mysql_date": "2012-04-23T18:25:43.511Z"
// 	},
// 	{
// 		"repo": "starscream.tim-online.nl",
// 		"date": "2013-04-23T18:25:43.511Z"
// 		"mysql_date": ""
// 	},
// 	{
// 		"repo": "mirage.tim-online.nl",
// 		"date": "2013-04-23T18:25:43.511Z"
// 		"mysql_date": ""
// 	}
// ]
func recentHandler(w http.ResponseWriter, r *http.Request) {
	archives, err := getMostRecentArchivesPerRepository()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Sort archives by datetime
	slice.Sort(archives, func(i, j int) bool {
		return archives[i].Datetime.Before(archives[j].Datetime)
	})

	jsonItems := make([]jsonFormat, 0)
	for _, archive := range archives {
		var mysqlBackup *file
		mysqlBackup, err := getMostRecentMysqlBackupInArchive(archive)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// @TODO: this can be done with goroutines to speed it up
		var item jsonFormat
		if mysqlBackup == nil {
			item = jsonFormat{
				Repo: archive.RepoName,
				Date: archive.Datetime.Format(time.RFC3339),
				MysqlDate: "",
			}
		} else {
			item = jsonFormat{
				Repo: archive.RepoName,
				Date: archive.Datetime.Format(time.RFC3339),
				MysqlDate: mysqlBackup.Mtime.Format(time.RFC3339),
			}
		}

		jsonItems = append(jsonItems, item)
	}

	b, err := json.Marshal(jsonItems)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}

func getMostRecentArchivesPerRepository() ([]archive, error) {
	archives := make([]archive, 0)

	root, err := getRoot()
	if err != nil {
		return archives, err
	}

	repoNames, err := findRepositories(root)
	if err != nil {
		return archives, err
	}

	for _, repoName := range repoNames {
		newArchives, err := findArchives(root, repoName)
		if err != nil {
			return nil, err
		}

		if len(newArchives) == 0 {
			continue
		}

		// Newest is last
		n := len(newArchives)
		archives = append(archives, newArchives[n-1])
	}

	return archives, nil
}

func getRoot() (string, error) {
	if len(os.Args) < 2 {
		return "", errors.New("You have to provide a root directory")
	}

	// Use the last argument as root path
	position := len(os.Args) -1
	root := os.Args[position]

	// Not necessary, just trying stuff out
	root = expandTilde(root)

	// check if the source dir exist
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("%s doesn't exist", root)
	}

	// check if the source is indeed a directory or not
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", root)
	}

	return root, nil
}

func expandTilde(f string) string {
	// TODO expansion of other user's home directories.
	// Q what characters are valid in a user name?
	if strings.HasPrefix(f, "~"+string(filepath.Separator)) {
		user, _ := user.Current()
		return user.HomeDir + f[1:]
	}
	return f
}

func findRepositories(root string) ([]string, error) {
	repoNames := make([]string, 0)

	files, err := ioutil.ReadDir(root)
	if err != nil {
		return repoNames, nil
	}

	for _, f := range files {
		repoName := f.Name()

		if !f.IsDir() {
			continue
		}

		repoPath := path.Join(root, repoName)

		isRepo, err := isRepository(repoPath)
		if err != nil {
			return repoNames, err
		}

		if !isRepo {
			continue
		}

		repoNames = append(repoNames, f.Name())
	}

	return repoNames, nil
}

func findArchives(root string, repoName string) ([]archive, error) {
	archives := make([]archive, 0)

	// Create path to repo
	repoPath := path.Join(root, repoName)

	// list archives in repo
	stdout, _, err := borgList(repoPath)
	if err != nil {
		return archives, err
	}

	// Loop each line in stdout
	for _, line := range strings.Split(string(stdout), "\n") {
		// Split line into columns by whitespace:
		fields := strings.Fields(line)

		// 0.27.0: wbb.tim-online.nl-2015-10-31  Mon Jan 2 15:04:05 2006
		// 0.30.0: wbb.tim-online.nl-2016-01-27  Wed, 2016-01-27 03:01:19

		// Arbitrary number of fields to act as cutoff
		if len(fields) < 4 {
			continue
		}

		// Collect fields into meaningful columns
		name := fields[0]
		str := strings.Join(fields[1:4], " ")

		// Parse date/time column
		// https://golang.org/src/time/format.go#L64
		datetime, err := time.Parse("Mon, 2006-01-02 15:04:05", str)
		if err != nil {
			return archives, fmt.Errorf("Can't parse %s", str)
		}

		// Instantiate new archive
		archive := archive{
			Name:     name,
			RepoName: repoName,
			Datetime: datetime,
		}

		// Add archive to list
		archives = append(archives, archive)
	}

	return archives, nil
}

func borgList(repoOrArchive string) ([]byte, []byte, error) {
	// Setup command
	args := []string{"list", repoOrArchive}
	cmd := exec.Command(borgBinary, args...)

	// Log stdout
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}

	// Log stderr
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, err
	}

	// Run command
	err = cmd.Start()
	if err != nil {
		return nil, nil, err
	}

	// Read stdout & stderr to []byte
	stdout, _ := ioutil.ReadAll(stdoutPipe)
	stderr, _ := ioutil.ReadAll(stderrPipe)

	// get first line of stderr as error
	line, _ := bytes.NewBuffer(stderr).ReadString('\n')

	// Wait for command to finish
	err = cmd.Wait()
	if err != nil {
		// This gets triggered when exitstatus != 0
		return nil, nil, errors.New(string(line))
	}

	return stdout, stderr, nil
}

func findBorgBinary() (string, error) {
	return lookPath("borg")
}

func lookPath(file string) (string, error) {
	path, err := exec.LookPath("./" + file)
	if err == nil {
		return path, nil
	}

	return exec.LookPath(file)
}

func isRepository(repoPath string) (bool, error) {
	_, _, err := borgList(repoPath)
	if err != nil {
		return false, err
	}

	return true, nil
}

func getRepositoryInfo(repoPath string) (string, error) {
	// stdout, stderr, err := borgList(repoPath)
	return "", nil
}

func findMysqlBackups(archive archive) ([]file, error) {
	files := make([]file, 0)

	root, err := getRoot()
	if err != nil {
		return files, err
	}

	repoPath := path.Join(root, archive.RepoName)
	repoOrArchive := fmt.Sprintf("%v::%v", repoPath, archive.Name)

	stdout, _, err := borgList(repoOrArchive)
	if err != nil {
		return files, err
	}

	globs := []string{
		// sql dumps
		"var/backups/mysql/daily/*.sql.gz",
		// binary backups
		"var/backups/mysql/daily/*/ibdata1",
	}

	// Loop each line in stdout
	for _, line := range strings.Split(string(stdout), "\n") {
		// Split line into columns by whitespace
		fields := strings.Fields(line)
		// fields := strings.Split(line, "  ")

		// Arbitrary number of fields to act as cutoff
		if len(fields) < 8 {
			continue
		}

		// Collect fields into meaningful columns
		// permissions := fields[0]
		// user := fields[1]
		// group := fields[2]
		size := fields[3]
		datetimeStr := strings.Join(fields[4:7], " ")
		p := fields[7]

		// If no size: don't count it as a match
		if size == "0" {
			continue
		}

		// check if globs match
		matched := false
		for _, glob := range globs {
			matched, err = path.Match(glob, p)
			if err != nil {
				return files, err
			}

			if matched {
				break
			}
		}

		// No globs matched: skip file
		if matched == false {
			continue
		}

		// Parse different date/time columns
		// okt  9 18:09
		// apr 11  2014
		// https://golang.org/src/time/format.go#L64
		datetime, err := time.Parse("Mon, 2006-01-02 15:04:05", datetimeStr)
		if err != nil {
			return files, fmt.Errorf("Can't parse %s", datetimeStr)
		}

		// Instantiate new file
		f := file{
			Path:  p,
			Mtime: datetime,
		}

		// Add archive to list
		files = append(files, f)
	}

	return files, nil
}

func getMostRecentMysqlBackupInArchive(archive archive) (*file, error) {
	mysqlBackups, err := findMysqlBackups(archive)
	if err != nil {
		return nil, nil
	}

	// Sort mysql backups by datetime (newest first)
	slice.Sort(mysqlBackups, func(i, j int) bool {
		return mysqlBackups[i].Mtime.After(mysqlBackups[j].Mtime)
	})

	if len(mysqlBackups) == 0 {
		return nil, err
	}

	return &mysqlBackups[0], err
}
