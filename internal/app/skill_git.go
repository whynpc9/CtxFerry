package app

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func readGitSkill(ctx context.Context, pkg *SkillPackage, input SkillImport) (skillPayload, error) {
	var payload skillPayload
	pkg.SourceKind = "git"
	source := input.Git
	if filepath.IsAbs(source) {
		pkg.SourceURL = skillFileURL(filepath.Clean(source))
	} else {
		u, err := url.Parse(source)
		if err != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Path == "" {
			return payload, Fail("usage", "provide a Git URL without credentials, query or fragment, or an absolute Git repository path")
		}
		switch u.Scheme {
		case "https", "ssh":
			password := false
			if u.User != nil {
				_, password = u.User.Password()
			}
			if u.Hostname() == "" || password || u.Scheme == "https" && u.User != nil ||
				u.Scheme == "ssh" && (strings.HasPrefix(u.Hostname(), "-") || u.User != nil && strings.HasPrefix(u.User.Username(), "-")) {
				return payload, Fail("usage", "invalid Git URL or embedded credentials")
			}
		case "file":
			if u.Host != "" || u.User != nil || !strings.HasPrefix(u.Path, "/") {
				return payload, Fail("usage", "file Git URLs must point to an absolute local path")
			}
		default:
			return payload, Fail("usage", "Git sources must use HTTPS, SSH, file URLs or absolute paths")
		}
		pkg.SourceURL = u.String()
	}
	if input.Subdir != "" && input.Subdir != "." {
		if !validateSkillPath(input.Subdir) {
			return payload, Fail("usage", "subdir must be a portable repository-relative path")
		}
		pkg.Subdir = input.Subdir
	}
	ref := input.Ref
	hash := skillObjectID(ref)
	if !hash {
		tag, ok := strings.CutPrefix(ref, "refs/tags/")
		if !ok || tag == "" || strings.Contains(tag, ":") {
			return payload, Fail("usage", "ref must be a full commit hash or refs/tags/<release>")
		}
		pkg.Release = tag
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	dir, err := os.MkdirTemp("", "ctxferry-skill-git-")
	if err != nil {
		return payload, err
	}
	defer os.RemoveAll(dir) // Remove the private Git object scratch directory.
	init := []string{"init", "--bare", "--template="}
	if hash && len(ref) == 64 {
		init = append(init, "--object-format=sha256")
	}
	if _, err := syncGit(ctx, dir, nil, init...); err != nil {
		return payload, err
	}
	if !hash {
		if _, err := syncGit(ctx, dir, nil, "check-ref-format", ref); err != nil {
			return payload, Fail("usage", "ref must name a valid Git tag")
		}
	}
	if _, err := syncGit(ctx, dir, nil, "fetch", "--depth=1", "--no-tags", "--no-recurse-submodules", "--", source, ref); err != nil {
		return payload, err
	}
	pkg.Commit, err = syncGit(ctx, dir, nil, "rev-parse", "--verify", "FETCH_HEAD^{commit}")
	if err != nil {
		return payload, err
	}
	if !skillObjectID(pkg.Commit) || hash && !strings.EqualFold(ref, pkg.Commit) {
		return payload, Fail("invalid-skill", "fetched Git object does not match the pinned commit")
	}

	tree := pkg.Commit + "^{tree}"
	if pkg.Subdir != "." {
		tree = pkg.Commit + ":" + pkg.Subdir
	}
	listing, err := syncGitRaw(ctx, dir, nil, "ls-tree", "-rlz", tree)
	if err != nil {
		return payload, err
	}
	for row := range strings.SplitSeq(strings.TrimSuffix(listing, "\x00"), "\x00") {
		if row == "" {
			continue
		}
		header, path, ok := strings.Cut(row, "\t")
		fields := strings.Fields(header)
		if !ok || len(fields) != 4 || fields[1] != "blob" || !skillObjectID(fields[2]) ||
			(fields[0] != "100644" && fields[0] != "100755") || !validateSkillPath(path) {
			return payload, Fail("invalid-skill", fmt.Sprintf("unsupported Git tree entry %q", path))
		}
		size, err := strconv.Atoi(fields[3])
		if err != nil || size < 0 || size > skillMaxBytes-payload.size || len(payload.files) >= skillMaxFiles {
			return payload, Fail("skill-too-large", "a Skill may contain at most 2048 files and 32 MiB of content")
		}
		raw, err := syncGitRaw(ctx, dir, nil, "cat-file", "blob", fields[2])
		if err != nil {
			return payload, err
		}
		if len(raw) != size {
			return payload, Fail("invalid-skill", "Git blob does not match its tree entry")
		}
		if err := payload.add(path, fields[0] == "100755", []byte(raw)); err != nil {
			return payload, err
		}
	}
	return payload, nil
}

func skillObjectID(id string) bool {
	if len(id) != 40 && len(id) != 64 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}
