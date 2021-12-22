/*
   Copyright 2020 Docker Inc.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/google/uuid"
	"github.com/mitchellh/go-homedir"
)

var (
	// SnykDesktopVersion is the version of the Snyk CLI Binary embedded with Docker Desktop
	SnykDesktopVersion = "unknown"
)

type snykProvider struct {
	Options
}

// NewSnykProvider returns a Snyk implementation of scan provider
func NewSnykProvider(defaultProvider Options, snykOps ...SnykProviderOps) (Provider, error) {
	provider := snykProvider{
		Options: defaultProvider,
	}
	for _, snykOp := range snykOps {
		if err := snykOp(&provider); err != nil {
			return nil, err
		}
	}
	return &provider, nil
}

// SnykProviderOps function taking a pointer to a Snyk Provider and returning an error if needed
type SnykProviderOps func(*snykProvider) error

func (s *snykProvider) Authenticate(token string) error {
	if token != "" {
		if _, err := uuid.Parse(token); err != nil {
			return &invalidTokenError{token}
		}
	}
	cmd := s.newCommand("auth", token)
	cmd.Env = append(cmd.Env,
		"SNYK_UTM_MEDIUM=Partner",
		"SNYK_UTM_SOURCE=Docker",
		"SNYK_UTM_CAMPAIGN=Docker-Desktop-2020")
	cmd.Stdout = s.out
	cmd.Stderr = s.err
	return checkCommandErr(cmd.Run())
}

func (s *snykProvider) Scan(image string) error {
	// check snyk token
	cmd := s.newCommand(append(s.flags, image)...)
	if authenticated, err := isAuthenticatedOnSnyk(); authenticated == "" || err != nil {
		var err error
		token, err := getToken(s.Options)
		if err != nil {
			return fmt.Errorf("failed to get DockerScanID: %s", err)
		}
		cmd.Env = append(cmd.Env, fmt.Sprintf("SNYK_DOCKER_TOKEN=%s", token))
	} else {
		cmd.Env = append(cmd.Env, fmt.Sprintf("SNYK_TOKEN=%s", authenticated))
	}

	cmd.Stdout = s.out
	cmd.Stderr = s.err
	return checkCommandErr(cmd.Run())
}

func (s *snykProvider) Version() (string, error) {
	cmd := s.newCommand("--version")
	buff := bytes.NewBuffer(nil)
	buffErr := bytes.NewBuffer(nil)
	cmd.Stdout = buff
	cmd.Stderr = buffErr
	if err := cmd.Run(); err != nil {
		errMsg := fmt.Sprintf("failed to get snyk version: %s", checkCommandErr(err))
		if buffErr.String() != "" {
			errMsg = fmt.Sprintf(errMsg+",%s", buffErr.String())
		}
		return "", fmt.Errorf(errMsg)
	}
	return fmt.Sprintf("Snyk (%s)", strings.TrimSpace(buff.String())), nil
}

func (s *snykProvider) newCommand(arg ...string) *exec.Cmd {
	cmd := exec.CommandContext(s.context, s.path, arg...)
	cmd.Env = append(os.Environ(),
		"NO_UPDATE_NOTIFIER=true",
		"SNYK_CFG_DISABLESUGGESTIONS=true",
		"SNYK_INTEGRATION_NAME=DOCKER_DESKTOP")
	return cmd
}

func checkCommandErr(err error) error {
	if err == nil {
		return nil
	}
	if err == exec.ErrNotFound {
		// Could not find Snyk in $PATH
		return fmt.Errorf("could not find Snyk binary")
	} else if _, ok := err.(*exec.Error); ok {
		return fmt.Errorf("could not find Snyk binary")
	} else if _, ok := err.(*os.PathError); ok {
		// The specified path for Snyk binary does not exist
		return fmt.Errorf("could not find Snyk binary")
	}
	return err
}

type snykConfig struct {
	API string `json:"api,omitempty"`
}

func isAuthenticatedOnSnyk() (string, error) {
	home, err := homedir.Dir()
	if err != nil {
		return "", err
	}
	snykConfFilePath := filepath.Join(home, ".config", "configstore", "snyk.json")
	buff, err := ioutil.ReadFile(snykConfFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	var config snykConfig
	if err := json.Unmarshal(buff, &config); err != nil {
		return "", err
	}

	return config.API, nil
}

func checkUserSnykBinaryVersion(path string) (bool, error) {
	cmd := exec.Command(path, "--version")
	buff := bytes.NewBuffer(nil)
	cmd.Stdout = buff
	cmd.Stderr = ioutil.Discard
	if err := cmd.Run(); err != nil {
		// an error occurred, so let's use the desktop binary
		return false, err
	}
	ver, err := semver.NewVersion(cleanVersion(buff.String()))
	if err != nil {
		return false, err
	}
	constraint, err := semver.NewConstraint(minimalSnykVersion())
	if err != nil {
		return false, err
	}
	matchConstraint := constraint.Check(ver)
	if !matchConstraint {
		return matchConstraint, fmt.Errorf("The Snyk version %s installed on your system is older as the one embedded by Docker Desktop (%s), using embedded Snyk version instead.\n",
			ver, minimalSnykVersion())
	}
	return matchConstraint, nil
}

func cleanVersion(version string) string {
	version = strings.TrimSpace(version)
	return strings.Split(version, " ")[0]
}

func minimalSnykVersion() string {
	return fmt.Sprintf(">=%s", SnykDesktopVersion)
}
