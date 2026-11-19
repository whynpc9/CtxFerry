package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/whynpc9/CtxFerry/internal/platform"
	"github.com/whynpc9/CtxFerry/internal/store"
	"gopkg.in/yaml.v3"
)

const (
	skillMaxBytes = 32 << 20
	skillMaxFiles = 2048
	skillMaxPath  = 1024
)

type SkillImport struct {
	Local, Git, Subdir, Ref, License string
	Platforms, Dependencies          []string
}

type SkillFile struct {
	Path       string `json:"path"`
	Digest     string `json:"digest"`
	Executable bool   `json:"executable"`
}

type SkillPackage struct {
	SchemaVersion int         `json:"schemaVersion"`
	Name          string      `json:"name"`
	SourceKind    string      `json:"sourceKind"`
	SourceURL     string      `json:"sourceUrl"`
	Subdir        string      `json:"subdir"`
	Commit        string      `json:"commit,omitempty"`
	Release       string      `json:"release,omitempty"`
	Digest        string      `json:"digest"`
	License       string      `json:"license"`
	Compatibility string      `json:"compatibility,omitempty"`
	Platforms     []string    `json:"platforms"`
	Dependencies  []string    `json:"dependencies"`
	Files         []SkillFile `json:"files"`
}

type skillPayload struct {
	files []SkillFile
	data  map[string][]byte
	size  int
}

func (p *skillPayload) add(path string, executable bool, data []byte) error {
	p.size += len(data)
	if p.size > skillMaxBytes || len(p.files) >= skillMaxFiles {
		return Fail("skill-too-large", "a Skill may contain at most 2048 files and 32 MiB of content")
	}
	if p.data == nil {
		p.data = make(map[string][]byte)
	}
	p.data[path] = data
	p.files = append(p.files, SkillFile{path, digest(data), executable})
	return nil
}

func validateSkillPath(path string) bool {
	if len(path) > skillMaxPath || !utf8.ValidString(path) || !fs.ValidPath(path) || path == "." {
		return false
	}
	for part := range strings.SplitSeq(path, "/") {
		if len(part) > 255 || strings.ContainsAny(part, "\\:*?\"<>|") || strings.TrimRight(part, " .") != part || strings.EqualFold(part, ".git") {
			return false
		}
		// Reject Windows device names on every platform.
		device, _, _ := strings.Cut(part, ".")
		device = strings.ToUpper(strings.TrimRight(device, " "))
		switch device {
		case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
			return false
		}
		if strings.HasPrefix(device, "COM") || strings.HasPrefix(device, "LPT") {
			if strings.Contains("123456789¹²³", device[3:]) && utf8.RuneCountInString(device[3:]) == 1 {
				return false
			}
		}
		for _, c := range part {
			if c < 32 || c == 127 {
				return false
			}
		}
	}
	return true
}

func validSkillName(name string) bool {
	return len(name) <= 64 && validSlug.MatchString(name) && validateSkillPath(name)
}

func (p *skillPayload) finish() error {
	slices.SortFunc(p.files, func(a, b SkillFile) int { return strings.Compare(a.Path, b.Path) })
	paths := make(map[string]string)
	for _, file := range p.files {
		if !validateSkillPath(file.Path) {
			return Fail("invalid-skill", fmt.Sprintf("unsupported Skill path %q", file.Path))
		}
		if strings.Contains(file.Path, "/") && strings.EqualFold(filepath.Base(file.Path), "SKILL.md") {
			return Fail("invalid-skill", "the selected directory contains a nested Skill; choose a single Skill directory")
		}
		for path := file.Path; path != "."; path = filepath.ToSlash(filepath.Dir(path)) {
			// EqualFold handles Unicode case pairs beyond strings.ToLower.
			// Canonicalize by uppercasing then lowercasing so final sigma and
			// other equivalent forms share a key.
			key := strings.ToLower(strings.ToUpper(path))
			if previous, exists := paths[key]; exists && previous != path {
				return Fail("invalid-skill", fmt.Sprintf("case-colliding paths %q and %q", previous, path))
			}
			paths[key] = path
		}
	}
	return nil
}

func readSkillDirectory(dir string, selecting bool) (skillPayload, []string, error) {
	var payload skillPayload
	var directories []string
	root, err := os.OpenRoot(dir)
	if err != nil {
		return payload, nil, err
	}
	defer root.Close()
	err = fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || path == "." {
			return err
		}
		if selecting && strings.EqualFold(entry.Name(), ".git") {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !validateSkillPath(path) {
			return Fail("invalid-skill", fmt.Sprintf("unsupported Skill path %q", path))
		}
		if entry.IsDir() {
			if len(directories) >= skillMaxFiles {
				return Fail("skill-too-large", "a Skill may contain at most 2048 directories")
			}
			directories = append(directories, path)
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return Fail("invalid-skill", fmt.Sprintf("Skill path %q is not a regular file", path))
		}
		f, err := root.Open(filepath.FromSlash(path))
		if err != nil {
			return err
		}
		defer f.Close()
		actual, err := f.Stat()
		if err != nil {
			return err
		}
		if !os.SameFile(info, actual) {
			return Fail("invalid-skill", fmt.Sprintf("Skill path %q changed during inspection", path))
		}
		data, err := io.ReadAll(io.LimitReader(f, int64(skillMaxBytes-payload.size+1)))
		if err != nil {
			return err
		}
		return payload.add(path, info.Mode().Perm()&0111 != 0, data)
	})
	return payload, directories, err
}

func skillFrontMatter(data []byte) (name, license, compatibility string, err error) {
	var metadata struct {
		Name, License, Compatibility string
	}
	first, rest, found := bytes.Cut(data, []byte("\n"))
	if !found || string(bytes.TrimSuffix(first, []byte("\r"))) != "---" {
		err = Fail("invalid-skill", "SKILL.md must start with YAML front matter containing a name")
		return
	}
	end := 0
	for line := range bytes.SplitSeq(rest, []byte("\n")) {
		if string(bytes.TrimSuffix(line, []byte("\r"))) == "---" {
			if end > 64<<10 || yaml.Unmarshal(rest[:end], &metadata) != nil || !validSkillName(metadata.Name) {
				err = Fail("invalid-skill", "SKILL.md must declare a valid lowercase Skill name in its YAML front matter")
				return
			}
			return metadata.Name, metadata.License, metadata.Compatibility, nil
		}
		end += len(line) + 1
	}
	err = Fail("invalid-skill", "SKILL.md front matter is missing its closing delimiter")
	return
}

func skillFileURL(path string) string {
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}

func (a App) skillDir() string {
	return filepath.Join(filepath.Dir(store.Path(a.DataDir)), "skills")
}

func (a App) skillDevice(ctx context.Context) (string, error) {
	db, err := store.Open(ctx, a.DataDir, false)
	if err != nil {
		return "", err
	}
	defer db.Close()
	var device string
	err = db.QueryRowContext(ctx, "SELECT id FROM device WHERE singleton=1").Scan(&device)
	if errors.Is(err, sql.ErrNoRows) {
		return "", store.ErrNotInitialized
	}
	return device, err
}

func (a App) lockSkills(ctx context.Context) (func(), error) {
	unlock, err := a.syncLock(ctx, "skill-owner.sqlite", "5000")
	if err != nil && Classify(err).Code == "sync-busy" {
		return nil, Fail("skill-busy", "another Skill operation is active; retry")
	}
	return unlock, err
}

func (a App) AddSkill(ctx context.Context, input SkillImport) (SkillPackage, error) {
	pkg := SkillPackage{SchemaVersion: 1, Subdir: ".", Platforms: []string{}, Dependencies: []string{}}
	if (input.Local == "") == (input.Git == "") || input.Local != "" && (input.Ref != "" || input.Subdir != "") {
		return pkg, Fail("usage", "choose either --local or --git with --ref and an optional --subdir")
	}
	for _, value := range input.Platforms {
		osName, arch, hasArch := strings.Cut(value, "/")
		if osName == "" || !validSlug.MatchString(osName) || hasArch && !validSlug.MatchString(arch) {
			return pkg, Fail("usage", "platforms must be OS or OS/architecture names")
		}
		pkg.Platforms = append(pkg.Platforms, value)
	}
	for _, dependency := range input.Dependencies {
		if !validateSkillPath(dependency) || strings.Contains(dependency, "/") {
			return pkg, Fail("usage", "dependencies must be executable names on PATH")
		}
		pkg.Dependencies = append(pkg.Dependencies, dependency)
	}
	slices.Sort(pkg.Platforms)
	slices.Sort(pkg.Dependencies)
	pkg.Platforms = slices.Compact(pkg.Platforms)
	pkg.Dependencies = slices.Compact(pkg.Dependencies)
	if _, err := a.skillDevice(ctx); err != nil {
		return pkg, err
	}

	var payload skillPayload
	var err error
	if input.Local != "" {
		var dir string
		dir, err = canonical(input.Local)
		if err != nil {
			return pkg, err
		}
		root, err := canonical(a.DataDir)
		if err != nil {
			return pkg, err
		}
		if contains(dir, root) || contains(root, dir) {
			return pkg, Fail("invalid-skill", "select a Skill directory outside the application data directory")
		}
		pkg.SourceKind, pkg.SourceURL = "local", skillFileURL(dir)
		payload, _, err = readSkillDirectory(dir, true)
	} else {
		payload, err = readGitSkill(ctx, &pkg, input)
	}
	if err != nil {
		return pkg, err
	}
	if err := payload.finish(); err != nil {
		return pkg, err
	}
	pkg.Name, pkg.License, pkg.Compatibility, err = skillFrontMatter(payload.data["SKILL.md"])
	if err != nil {
		return pkg, err
	}
	if input.License != "" {
		pkg.License = input.License
	}
	if strings.TrimSpace(pkg.License) == "" {
		pkg.License = "NOASSERTION"
	}
	pkg.Files = payload.files
	wire, err := json.Marshal(pkg.Files)
	if err != nil {
		return pkg, err
	}
	pkg.Digest = digest(wire)

	unlock, err := a.lockSkills(ctx)
	if err != nil {
		return pkg, err
	}
	defer unlock()
	return pkg, a.saveSkillPackage(pkg, payload.data)
}

func (a App) saveSkillPackage(pkg SkillPackage, data map[string][]byte) error {
	base := filepath.Join(a.skillDir(), "packages")
	path := filepath.Join(base, pkg.Name)
	if info, err := os.Lstat(path); err == nil {
		if !info.IsDir() {
			return Fail("skill-name-collision", "the local package name is already in use")
		}
		var existing SkillPackage
		if err := readSkillJSON(filepath.Join(path, "manifest.json"), &existing); err != nil {
			return err
		}
		before, err := json.Marshal(existing)
		if err != nil {
			return err
		}
		after, err := json.Marshal(pkg)
		if err != nil {
			return err
		}
		if !bytes.Equal(before, after) {
			return Fail("skill-name-collision", "a different Skill pin already uses this name")
		}
		changes, err := compareSkillTree(filepath.Join(path, "files"), pkg.Files)
		if err != nil {
			return err
		}
		if len(changes) != 0 {
			return Fail("skill-package-changed", "the local Skill package differs from its manifest")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := mkdirSkillDirs(base); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(base, ".package-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage) // This invocation owns the private staging directory.
	if err := writeSkillTree(filepath.Join(stage, "files"), pkg.Files, data); err != nil {
		return err
	}
	if err := writeSkillJSON(filepath.Join(stage, "manifest.json"), pkg); err != nil {
		return err
	}
	if err := moveSkillExclusive(stage, path); err != nil {
		return err
	}
	return platform.SyncSkillDirectory(base)
}

func writeSkillTree(root string, files []SkillFile, data map[string][]byte) error {
	for _, file := range files {
		path := filepath.Join(root, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		mode := os.FileMode(0600)
		if file.Executable {
			mode = 0700
		}
		if err := writeSkillFile(path, mode, data[file.Path]); err != nil {
			return err
		}
	}
	var dirs []string
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && entry.IsDir() {
			dirs = append(dirs, path)
		}
		return err
	}); err != nil {
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := platform.SyncSkillDirectory(dirs[i]); err != nil {
			return err
		}
	}
	return nil
}

func writeSkillFile(path string, mode os.FileMode, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}

func readSkillJSON(path string, value any) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return Fail("invalid-skill-state", "a Skill state record must be a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	actual, err := f.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(info, actual) {
		return Fail("invalid-skill-state", "a Skill state record changed during inspection")
	}
	decoder := json.NewDecoder(io.LimitReader(f, 8<<20))
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Fail("invalid-skill-state", "unexpected data after the Skill record")
	}
	return nil
}

func writeSkillJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".record-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if err = errors.Join(err, f.Close()); err != nil {
		return err
	}
	if err = platform.ReplaceSkillRecord(f.Name(), path); err != nil {
		return err
	}
	return platform.SyncSkillDirectory(filepath.Dir(path))
}

func moveSkillExclusive(source, dest string) error {
	from, err := os.Open(filepath.Dir(source))
	if err != nil {
		return err
	}
	defer from.Close()
	to, err := os.Open(filepath.Dir(dest))
	if err != nil {
		return err
	}
	defer to.Close()
	err = platform.RenameSkillExclusive(from, filepath.Base(source), to, filepath.Base(dest))
	if err != nil {
		return &os.LinkError{Op: "publish skill", Old: source, New: dest, Err: err}
	}
	return nil
}

func sameSkillFile(a, b SkillFile) bool {
	return a.Digest == b.Digest && (runtime.GOOS == "windows" || a.Executable == b.Executable)
}
