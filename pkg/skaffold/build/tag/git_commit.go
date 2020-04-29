/*
Copyright 2019 The Skaffold Authors

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

package tag

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/constants"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/util"
)

const (
	tags = iota
	commitSha
	abbrevCommitSha
	treeSha
	abbrevTreeSha
)

// GitCommit tags an image by the git commit it was built at.
type GitCommit struct {
	prefix  string
	variant int
}

// NewGitCommit creates a new git commit tagger. It fails if the tagger variant is invalid.
func NewGitCommit(prefix, taggerVariant string) (*GitCommit, error) {
	var variant int
	switch strings.ToLower(taggerVariant) {
	case "", "tags":
		// default to "tags" when unset
		variant = tags
	case "commitsha":
		variant = commitSha
	case "abbrevcommitsha":
		variant = abbrevCommitSha
	case "treesha":
		variant = treeSha
	case "abbrevtreesha":
		variant = abbrevTreeSha
	default:
		return nil, fmt.Errorf("%s is not a valid git tagger variant", taggerVariant)
	}

	return &GitCommit{
		prefix:  prefix,
		variant: variant,
	}, nil
}

// Labels are labels specific to the git tagger.
func (c *GitCommit) Labels() map[string]string {
	return map[string]string{
		constants.Labels.TagPolicy: "git-commit",
	}
}

// GenerateFullyQualifiedImageName tags an image with the supplied image name and the git commit.
func (c *GitCommit) GenerateFullyQualifiedImageName(workingDir string, imageName string) (string, error) {
	ref, err := c.makeGitTag(workingDir)
	if err != nil {
		return "", fmt.Errorf("unable to find git commit: %w", err)
	}

	changes, err := runGit(workingDir, "status", ".", "--porcelain")
	if err != nil {
		return "", fmt.Errorf("getting git status: %w", err)
	}

	if len(changes) > 0 {
		return fmt.Sprintf("%s:%s%s-dirty", imageName, c.prefix, ref), nil
	}

	return fmt.Sprintf("%s:%s%s", imageName, c.prefix, sanitizeTag(ref)), nil
}

// sanitizeTag takes a git tag and converts it to a docker tag by removing
// all the characters that are not allowed by docker.
func sanitizeTag(tag string) string {
	// Replace unsupported characters with `_`
	sanitized := regexp.MustCompile(`[^a-zA-Z0-9-._]`).ReplaceAllString(tag, `_`)

	// Remove leading `-`s and `.`s
	prefixSuffix := regexp.MustCompile(`([-.]*)(.*)`).FindStringSubmatch(sanitized)
	sanitized = strings.Repeat("_", len(prefixSuffix[1])) + prefixSuffix[2]

	// Truncate to 128 characters
	if len(sanitized) > 128 {
		return sanitized[0:128]
	}

	if tag != sanitized {
		logrus.Warnf("Using %q instead of %q as an image tag", sanitized, tag)
	}

	return sanitized
}

func (c *GitCommit) makeGitTag(workingDir string) (string, error) {
	args := make([]string, 0, 4)
	switch c.variant {
	case tags:
		args = append(args, "describe", "--tags", "--always")
	case commitSha, abbrevCommitSha:
		args = append(args, "rev-list", "-1", "HEAD")
		if c.variant == abbrevCommitSha {
			args = append(args, "--abbrev-commit")
		}
	case treeSha, abbrevTreeSha:
		gitPath, err := getGitPathToWorkdir(workingDir)
		if err != nil {
			return "", err
		}
		args = append(args, "rev-parse")
		if c.variant == abbrevTreeSha {
			args = append(args, "--short")
		}
		// revision must come after the --short flag
		args = append(args, "HEAD:"+gitPath+"/")
	default:
		return "", errors.New("invalid git tag variant: defaulting to 'dirty'")
	}

	return runGit(workingDir, args...)
}

func getGitPathToWorkdir(workingDir string) (string, error) {
	absWorkingDir, err := filepath.Abs(workingDir)
	if err != nil {
		return "", err
	}

	// git reports the gitdir with resolved symlinks, so we need to do this too in order for filepath.Rel to work
	absWorkingDir, err = filepath.EvalSymlinks(absWorkingDir)
	if err != nil {
		return "", err
	}

	gitRoot, err := runGit(workingDir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}

	return filepath.Rel(gitRoot, absWorkingDir)
}

func runGit(workingDir string, arg ...string) (string, error) {
	cmd := exec.Command("git", arg...)
	cmd.Dir = workingDir

	out, err := util.RunCmdOut(cmd)
	if err != nil {
		return "", err
	}

	return string(bytes.TrimSpace(out)), nil
}
