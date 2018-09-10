package pack

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/buildpack/lifecycle"
	"github.com/buildpack/packs"
	"github.com/buildpack/packs/img"
)

func export(group lifecycle.BuildpackGroup, launchDir, repoName, stackName string, useDaemon, useDaemonStack bool) (string, error) {
	if useDaemon {
		return exportDaemon(group, launchDir, repoName, stackName)
	} else {
		return exportRegistry(group, launchDir, repoName, stackName)
	}
}

func exportRegistry(group lifecycle.BuildpackGroup, launchDir, repoName, stackName string) (string, error) {
	origImage, err := readImage(repoName, false)
	if err != nil {
		return "", err
	}

	stackImage, err := readImage(stackName, false)
	if err != nil || stackImage == nil {
		return "", packs.FailErr(err, "get image for", stackName)
	}

	repoStore, err := img.NewRegistry(repoName)
	if err != nil {
		return "", packs.FailErr(err, "access", repoName)
	}

	tmpDir, err := ioutil.TempDir("", "lifecycle.exporter.layer")
	if err != nil {
		return "", packs.FailErr(err, "create temp directory")
	}
	defer os.RemoveAll(tmpDir)

	exporter := &lifecycle.Exporter{
		Buildpacks: group.Buildpacks,
		TmpDir:     tmpDir,
		Out:        os.Stdout,
		Err:        os.Stderr,
	}
	newImage, err := exporter.Export(
		launchDir,
		stackImage,
		origImage,
	)
	if err != nil {
		return "", packs.FailErrCode(err, packs.CodeFailedBuild)
	}

	if err := repoStore.Write(newImage); err != nil {
		return "", packs.FailErrCode(err, packs.CodeFailedUpdate, "write")
	}

	sha, err := newImage.Digest()
	if err != nil {
		return "", packs.FailErr(err, "calculating image digest")
	}

	return sha.String(), nil
}

func exportDaemon(group lifecycle.BuildpackGroup, launchDir, repoName, stackName string) (string, error) {
	var dockerFile string
	dockerFile += "FROM " + stackName + "\n"
	dockerFile += "ADD --chown=packs:packs app /launch/app\n"
	dockerFile += "ADD --chown=packs:packs config /launch/config\n"
	bpLayers := make(map[string][]string)
	numLayers := 0
	needPrevImage := false
	for _, buildpack := range group.Buildpacks {
		dirs, err := filepath.Glob(filepath.Join(launchDir, buildpack.ID, "*.toml"))
		if err != nil {
			return "", err
		}
		bpLayers[buildpack.ID] = dirs
		for _, dir := range dirs {
			if filepath.Base(dir) == "launch.toml" {
				continue
			}
			dir = dir[:len(dir)-5]
			exists := true
			if _, err := os.Stat(dir); err != nil {
				if os.IsNotExist(err) {
					exists = false
				} else {
					return "", err
				}
			}
			dir, err = filepath.Rel(launchDir, dir)
			if err != nil {
				return "", err
			}
			if exists {
				dockerFile += fmt.Sprintf("ADD --chown=packs:packs %s /launch/%s\n", dir, dir)
			} else {
				needPrevImage = true
				dockerFile += fmt.Sprintf("COPY --from=prev --chown=packs:packs /launch/%s /launch/%s\n", dir, dir)
			}
			numLayers++
		}
	}
	if needPrevImage {
		dockerFile = "FROM " + repoName + " AS prev\n\n" + dockerFile
	}
	if err := ioutil.WriteFile(filepath.Join(launchDir, "Dockerfile"), []byte(dockerFile), 0666); err != nil {
		return "", err
	}

	cmd := exec.Command(
		"docker", "build",
		"-t", repoName,
		".",
	)
	cmd.Dir = launchDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", err
	}

	// Layers
	// b, err := exec.Command("docker", "inspect", repoName, "-f", "{{json .RootFS.Layers}}").Output()
	return "TODO", nil
}
