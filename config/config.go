package config

import (
	"errors"
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
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome != "" {
		return filepath.Join(dataHome, "sia-nbdserver", path)
	}

	currentUser, err := user.Current()
	if err != nil {
		log.Fatal(err)
	}

	return filepath.Join(currentUser.HomeDir, ".local/share/sia-nbdserver", path)
}

func GetSocketPath() (string, error) {
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		return "", errors.New("$XDG_RUNTIME_DIR not set")
	}
	
	return runtimeDir, nil
}

func ReadPasswordFile(path string) (string, error) {
	passwordBytes, err := ioutil.ReadFile(path)
	if err != nil {
		return "", nil
	}

	return strings.TrimSpace(string(passwordBytes)), nil
}
