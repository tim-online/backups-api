package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"path"
	"path/filepath"
	"strings"
	"time"
)

var borgBinary string

type archive struct {
	name     string
	path     string
	datetime time.Time
}

func main() {
	// http server
	// /recent
	// find repositories
	// get archives from repository
	// parse / sort data

	archives, err := getMostRecentArchivesPerRepository()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	b, _ := json.Marshal(archives)
	fmt.Println(string(b))

	// Format into:
	// [
	// 	{
	// 		"repo": "ironhide.tim-online.nl",
	// 		"date": "2012-04-23T18:25:43.511Z"
	// 	},
	// 	{
	// 		"repo": "starscream.tim-online.nl",
	// 		"date": "2013-04-23T18:25:43.511Z"
	// 	},
	// 	{
	// 		"repo": "mirage.tim-online.nl",
	// 		"date": "2013-04-23T18:25:43.511Z"
	// 	}
	// ]

}

func getMostRecentArchivesPerRepository() ([]archive, error) {
	archives := make([]archive, 0)

	root, err := getRoot()
	if err != nil {
		return nil, err
	}

	borgBinary, err = findBorgBinary()
	if err != nil {
		return nil, err
	}

	repoNames, err := findRepositories(root)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	for _, repoName := range repoNames {
		newArchives, err := findArchives(root, repoName)

		if len(newArchives) > 0 {
			archives = append(archives, newArchives...)
		}

		if err != nil {
			return nil, err
		}
	}

	return archives, nil
}

func getRoot() (string, error) {
	flag.Parse()

	if len(os.Args) < 2 {
		return "", errors.New("You have to provide a root directory")
	}

	root := os.Args[1]

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
		if !isRepository(repoPath) {
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
		// Split line into columns by whitespace
		fields := strings.Fields(line)
		// fields := strings.Split(line, "  ")

		// Arbitrary number of fields to act as cutoff
		if len(fields) < 6 {
			continue
		}

		// Collect fields into meaningful columns
		name := fields[0]
		str := strings.Join(fields[1:6], " ")

		// Parse date/time column
		datetime, err := time.Parse("Mon Jan 2 15:04:05 2006", str)
		if err != nil {
			return archives, fmt.Errorf("Can't parse %s", str)
		}

		// Instantiate new archive
		archive := archive{
			name:     name,
			path:     repoPath,
			datetime: datetime,
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

func isRepository(repoPath string) bool {
	_, _, err := borgList(repoPath)
	if err != nil {
		return false
	}

	return true
}

func getRepositoryInfo(repoPath string) (string, error) {
	// stdout, stderr, err := borgList(repoPath)
	return "", nil
}
