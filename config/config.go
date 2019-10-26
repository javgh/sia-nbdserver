package config

import (
	"io/ioutil"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

func PrependHomeDirectory(path string) string {
	currentUser, err := user.Current()
	if err != nil {
		log.Fatal(err)
	}
	return filepath.Join(currentUser.HomeDir, path)
}

func PrependDataDirectory(path string) string {
	if os.Getenv("XDG_DATA_HOME") != "" {
		return filepath.Join(os.Getenv("XDG_DATA_HOME"), "sia-nbdserver", path)
	}

	currentUser, err := user.Current()
	if err != nil {
		log.Fatal(err)
	}

	return filepath.Join(currentUser.HomeDir, ".local/share/sia-nbdserver", path)
}

func ReadPasswordFile(path string) (string, error) {
	passwordBytes, err := ioutil.ReadFile(path)
	if err != nil {
		return "", nil
	}

	return strings.TrimSpace(string(passwordBytes)), nil
}
