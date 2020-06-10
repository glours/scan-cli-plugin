package e2e

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/docker/cli/cli/config/configfile"
	"github.com/docker/cli/cli/config/types"
	"github.com/docker/docker-scan/config"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/env"
	"gotest.tools/v3/fs"
	"gotest.tools/v3/icmd"
)

const (
	ImageWithVulnerabilities    = "alpine:3.10.0"
	ImageWithoutVulnerabilities = "dockerscanci/scratch:1.0"
)

func TestScanFailsNoAuthentication(t *testing.T) {
	// create Snyk config file with empty token
	_, cleanFunction := createSnykConfFile(t, "")
	defer cleanFunction()

	cmd, configDir, cleanup := dockerCli.createTestCmd()
	defer cleanup()

	// write dockerCli config with authentication to a registry which isn't Hub
	patchConfig(t, configDir, "com.example.registry")

	cmd.Command = dockerCli.Command("scan", "example:image")
	icmd.RunCmd(cmd).Assert(t, icmd.Expected{
		ExitCode: 1,
		Err: `You need to be logged in to Docker Hub to use scan feature.
please login to Docker Hub using the Docker Login command`,
	})
}

func TestScanFailsWithCleanMessage(t *testing.T) {
	// create Snyk config file with empty token
	_, cleanFunction := createSnykConfFile(t, "")
	defer cleanFunction()

	cmd, _, cleanup := dockerCli.createTestCmd()
	defer cleanup()

	cmd.Command = dockerCli.Command("scan", "example:image")
	icmd.RunCmd(cmd).Assert(t, icmd.Expected{
		ExitCode: 1,
		Err: `You need to be logged in to Docker Hub to use scan feature.
please login to Docker Hub using the Docker Login command`,
	})
}

func TestScanSucceedWithDockerHub(t *testing.T) {
	t.Skip("TODO: waiting for Hub ID generation")
	cmd, configDir, cleanup := dockerCli.createTestCmd()
	defer cleanup()

	createScanConfigFile(t, configDir)

	// write dockerCli config with authentication to the Hub
	patchConfig(t, configDir, "https://index.docker.io/v1/")

	cmd.Command = dockerCli.Command("scan", ImageWithVulnerabilities)
	output := icmd.RunCmd(cmd).Assert(t, icmd.Expected{ExitCode: 1}).Combined()
	assert.Assert(t, strings.Contains(output, "vulnerability found"))
}

func TestScanWithSnyk(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		t.Skip("Can't run on this ci platform (windows containers or no engine installed)")
	}
	_, cleanFunction := createSnykConfFile(t, os.Getenv("E2E_TEST_AUTH_TOKEN"))
	defer cleanFunction()

	cmd, configDir, cleanup := dockerCli.createTestCmd()
	defer cleanup()

	createScanConfigFile(t, configDir)

	testCases := []struct {
		name     string
		image    string
		exitCode int
		contains string
	}{
		{
			name:     "image-without-vulnerabilities",
			image:    ImageWithoutVulnerabilities,
			exitCode: 0,
			contains: "no vulnerable paths found",
		},
		{
			name:     "image-with-vulnerabilities",
			image:    ImageWithVulnerabilities,
			exitCode: 1,
			contains: "vulnerability found",
		},
		{
			name:     "invalid-image-name",
			image:    "scratch",
			exitCode: 2,
			contains: "image was not found locally and pulling failed",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			cmd.Command = dockerCli.Command("scan", testCase.image)
			output := icmd.RunCmd(cmd).Assert(t, icmd.Expected{ExitCode: testCase.exitCode}).Combined()
			assert.Assert(t, strings.Contains(output, testCase.contains))
		})
	}
}

func createSnykConfFile(t *testing.T, token string) (*fs.Dir, func()) {
	content := fmt.Sprintf(`{"api" : "%s"}`, token)
	homeDir := fs.NewDir(t, t.Name(),
		fs.WithDir(".config",
			fs.WithDir("configstore",
				fs.WithFile("snyk.json", content))))
	homeFunc := env.Patch(t, "HOME", homeDir.Path())
	userProfileFunc := env.Patch(t, "USERPROFILE", homeDir.Path())
	cleanup := func() {
		userProfileFunc()
		homeFunc()
		homeDir.Remove()
	}

	return homeDir, cleanup
}

func patchConfig(t *testing.T, configDir string, url string) {
	buff, err := ioutil.ReadFile(filepath.Join(configDir, "config.json"))
	assert.NilError(t, err)
	var conf configfile.ConfigFile
	assert.NilError(t, json.Unmarshal(buff, &conf))

	conf.AuthConfigs = map[string]types.AuthConfig{url: {}}
	buff, err = json.Marshal(&conf)
	assert.NilError(t, err)

	assert.NilError(t, ioutil.WriteFile(filepath.Join(configDir, "config.json"), buff, 0644))
}

func createScanConfigFile(t *testing.T, configDir string) {
	conf := config.Config{Path: fmt.Sprintf("%s/scan/snyk", configDir)}
	buf, err := json.MarshalIndent(conf, "", "  ")
	assert.NilError(t, err)
	err = ioutil.WriteFile(fmt.Sprintf("%s/scan/config.json", configDir), buf, 0644)
	assert.NilError(t, err)
}
