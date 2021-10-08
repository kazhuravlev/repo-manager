package repomgr

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	cryptoSsh "golang.org/x/crypto/ssh"

	"github.com/Masterminds/semver/v3"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/go-git/go-git/v5/storage/memory"
)

func module2gitAddr(module string) (string, error) {
	parts := strings.SplitN(module, "/", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("bad module name")
	}

	return fmt.Sprintf("git@%s:%s.git", parts[0], parts[1]), nil
}

func fetchLastTag(module, privateKey string) (string, error) {
	repoUrl, err := module2gitAddr(module)
	if err != nil {
		return "", fmt.Errorf("cannot extract git addr: %w", err)
	}

	pk, err := ssh.NewPublicKeysFromFile("git", os.ExpandEnv(privateKey), "")
	if err != nil {
		return "", fmt.Errorf("cannot parse private key: %w", err)
	}

	pk.HostKeyCallback = func(hostname string, remote net.Addr, key cryptoSsh.PublicKey) error { return nil }

	r, err := git.Clone(memory.NewStorage(), nil, &git.CloneOptions{
		URL:          repoUrl,
		Auth:         pk,
		Depth:        1,
		SingleBranch: true,
		Tags:         git.AllTags,
	})
	if err != nil {
		return "", fmt.Errorf("cannot clone repo: %w", err)
	}

	var semverTags []*semver.Version
	tags, err := r.Tags()
	if err != nil {
		return "", fmt.Errorf("cannot get tags: %w", err)
	}

	defer tags.Close()

	tagHandler := func(ref *plumbing.Reference) error {
		v, err := semver.NewVersion(ref.Name().Short())
		if err != nil {
			return err
		}

		semverTags = append(semverTags, v)
		return nil
	}

	if err := tags.ForEach(tagHandler); err != nil {
		return "", fmt.Errorf("cannot handle git tags: %w", err)
	}

	if len(semverTags) == 0 {
		return "", errors.New("at least one tag should be exists")
	}

	lastTag := semverTags[0]
	for _, tag := range semverTags[1:] {
		if tag.GreaterThan(lastTag) {
			lastTag = tag
		}
	}

	return lastTag.Original(), nil
}
