package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
)

var borgBinary string

func main() {
	// http server
	// /recent
	// find repositories
	// get archives from repository
	// parse / sort data
	root, err := getRoot()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	borgBinary, err = findBorgBinary()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	fmt.Printf("%+v\n", borgBinary)

	repoNames, err := findArchives(root)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	fmt.Printf("%+v\n", repoNames)
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

// func findArchives(root string) ([]string, error) {
// }

func findArchives(root string) ([]string, error) {
	// Create a nil slice
	// var repoNames []string
	repoNames := make([]string, 0)

	// Setup command
	args := []string{"list", root}
	cmd := exec.Command(borgBinary, args...)

	// cmd.Stdout = writeFile
	// cmd.Stderr = stderr

	// Log stdout
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return repoNames, err
	}

	// Log stderr
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return repoNames, err
	}

	fmt.Println(cmd)

	// Run command
	err = cmd.Start()
	if err != nil {
		return repoNames, err
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
		return repoNames, errors.New(string(line))
	}

	// Open /dev/null as io.Writer
	devNull, _ := os.Open(os.DevNull)
	defer devNull.Close()

	// Output stdout to /dev/null
	fmt.Fprintln(devNull, stdout)

	return repoNames, nil
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
